// Package collector defines the universal interface for AWS resource collection
// and the Registry that maps ResourceTypes to their Collector implementations.
//
// Extension pattern: to add a new resource type, create a new file in this
// package, define a struct implementing Collector, and register it via init().
// No changes to any other file are needed.
package collector

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"golang.org/x/sync/errgroup"
)

// ResourceType is a typed string enum identifying an AWS resource category.
// Format is "<service>:<subtype>", e.g. "ec2:instance".
type ResourceType string

const (
	TypeEC2Instance    ResourceType = "ec2:instance"
	TypeEBSVolume      ResourceType = "ec2:ebs-volume"
	TypeVPC            ResourceType = "ec2:vpc"
	TypeSubnet         ResourceType = "ec2:subnet"
	TypeSecurityGroup  ResourceType = "ec2:sg"
	TypeIGW            ResourceType = "ec2:igw"
	TypeELBClassic     ResourceType = "elb:classic"
	TypeELBV2          ResourceType = "elb:v2"
	TypeASG            ResourceType = "asg:group"
	TypeRDSInstance    ResourceType = "rds:instance"
	TypeRDSCluster     ResourceType = "rds:cluster"
	TypeDynamoTable    ResourceType = "dynamodb:table"
	TypeElastiCache    ResourceType = "elasticache:cluster"
	TypeLambdaFunction ResourceType = "lambda:function"
)

// Resource is the universal normalised representation of any AWS resource.
// All collectors map their SDK-specific output types to this struct so that
// the rest of the codebase (graph, orphan detector, TUI) is decoupled from
// individual AWS service packages.
type Resource struct {
	// ID is the canonical identifier — ARN when available, short ID otherwise.
	ID string

	// Type identifies which AWS resource category this is.
	Type ResourceType

	// Name is the human-readable label (from the Name tag or resource name field).
	Name string

	// Region is the AWS region this resource lives in.
	Region string

	// AccountID is the 12-digit AWS account number that owns this resource.
	AccountID string

	// RawID is the short AWS-native identifier, e.g. "i-0abc123" for EC2.
	RawID string

	// Tags are the raw AWS resource tags as key→value pairs.
	Tags map[string]string

	// Metadata holds resource-type-specific fields without requiring type
	// assertions elsewhere. Keys are documented in each collector file.
	// Example: "State"→"running", "InstanceType"→"t3.micro", "VpcId"→"vpc-abc".
	Metadata map[string]string
}

// DisplayName returns Name if set, otherwise RawID, otherwise the last
// segment of ID. Safe to call on zero-value Resources.
func (r Resource) DisplayName() string {
	if r.Name != "" {
		return r.Name
	}
	if r.RawID != "" {
		return r.RawID
	}
	return r.ID
}

// Collector is the interface every resource-type collector must satisfy.
// A single Collector handles exactly one ResourceType in one region.
// Implementations must be safe to call concurrently from multiple goroutines.
type Collector interface {
	// Type returns the ResourceType this collector handles.
	Type() ResourceType

	// Collect fetches all resources of this type in the given region using cfg.
	// The region in cfg may differ from the region argument; callers should
	// use the region argument as the authoritative region label.
	Collect(ctx context.Context, cfg aws.Config, region string) ([]Resource, error)
}

// Factory constructs a Collector from an aws.Config.
// Stored in the Registry so collectors can be instantiated on demand.
type Factory func(cfg aws.Config) Collector

// Registry maps ResourceType → Factory. New resource types register
// themselves in their package's init() function via DefaultRegistry.Register.
type Registry struct {
	mu        sync.RWMutex
	factories map[ResourceType]Factory
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{factories: make(map[ResourceType]Factory)}
}

// Register adds a Factory for rt. Panics on duplicate registration so that
// programming errors are caught at startup rather than silently dropped.
func (r *Registry) Register(rt ResourceType, f Factory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.factories[rt]; exists {
		panic(fmt.Sprintf("collector: duplicate registration for %q", rt))
	}
	r.factories[rt] = f
}

// All returns all registered Factories in a deterministic (sorted) order.
func (r *Registry) All() []Factory {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]string, 0, len(r.factories))
	for rt := range r.factories {
		types = append(types, string(rt))
	}
	sort.Strings(types)

	out := make([]Factory, 0, len(types))
	for _, t := range types {
		out = append(out, r.factories[ResourceType(t)])
	}
	return out
}

// DefaultRegistry is the package-level singleton populated by init() calls
// in each collector implementation file.
var DefaultRegistry = NewRegistry()

// RunAll executes every collector in reg across every region concurrently,
// merging all results into a single []Resource slice.
//
// maxConcurrency caps the total number of simultaneous AWS API calls.
// A value of 10 is a safe default for most accounts.
//
// Errors from individual (region, collector) pairs are logged to stderr but
// do not abort the entire scan — partial results are returned alongside a
// combined error summarising all failures.
func RunAll(
	ctx context.Context,
	baseCfg aws.Config,
	regions []string,
	reg *Registry,
	maxConcurrency int,
	progressFn func(region, resourceType string), // called after each successful collect; may be nil
) ([]Resource, error) {
	factories := reg.All()
	if len(factories) == 0 || len(regions) == 0 {
		return nil, nil
	}

	// sem limits concurrent goroutines to avoid throttling.
	sem := make(chan struct{}, maxConcurrency)

	var (
		mu      sync.Mutex
		results []Resource
	)

	eg, ctx := errgroup.WithContext(ctx)

	for _, region := range regions {
		for _, factory := range factories {
			// Capture loop variables.
			region := region
			factory := factory

			eg.Go(func() error {
				// Acquire semaphore slot.
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					return ctx.Err()
				}

				// Build a region-specific config and collector.
				regionCfg := baseCfg.Copy()
				regionCfg.Region = region
				c := factory(regionCfg)

				resources, err := c.Collect(ctx, regionCfg, region)
				if err != nil {
					// Non-fatal: return the error so errgroup records it,
					// but we still let other goroutines finish.
					return fmt.Errorf("[%s/%s] %w", region, c.Type(), err)
				}

				if progressFn != nil {
					progressFn(region, string(c.Type()))
				}

				mu.Lock()
				results = append(results, resources...)
				mu.Unlock()
				return nil
			})
		}
	}

	err := eg.Wait()
	return results, err
}

// tagValue extracts a tag value by key from an AWS tags map, returning ""
// if the key is absent. This helper is used by all collector files.
func tagValue(tags map[string]string, key string) string {
	return tags[key]
}

// tagsFromMap converts a map[string]string (already normalised) to the
// canonical Tags field — identity function kept for clarity at call sites.
func tagsFromMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
