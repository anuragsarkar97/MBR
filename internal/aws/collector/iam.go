package collector

// iam.go collects IAM roles and customer-managed policies.
// IAM is a global service; the client always uses us-east-1.
//
// Metadata keys written by this file:
//
//	Role:   RoleId, CreateDate, Description, LastUsedDate, LastUsedRegion,
//	        DaysSinceUsed (-1 = never), UsageStatus (active|stale|unused|never),
//	        TrustPrincipal, IsServiceLinked, Path
//	Policy: PolicyId, CreateDate, UpdateDate, AttachmentCount, DefaultVersionId

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// IAM resource types — not registered in DefaultRegistry (they are global,
// not per-region) and are collected separately via CollectIAM.
const (
	TypeIAMRole   ResourceType = "iam:role"
	TypeIAMPolicy ResourceType = "iam:policy"
)

// IAMUsageStatus classifies how recently an IAM role was assumed.
type IAMUsageStatus string

const (
	IAMStatusActive  IAMUsageStatus = "active"  // last used ≤ 30 days
	IAMStatusStale   IAMUsageStatus = "stale"   // last used 31–90 days
	IAMStatusUnused  IAMUsageStatus = "unused"  // last used > 90 days
	IAMStatusNever   IAMUsageStatus = "never"   // never assumed
)

// IAMResult holds the output of a full IAM scan.
type IAMResult struct {
	Roles    []Resource
	Policies []Resource
	Err      error
}

// CollectIAM fetches all IAM roles and customer-managed policies in one pass.
// It is intentionally not a per-region Collector — call it once per account.
func CollectIAM(ctx context.Context, cfg aws.Config) IAMResult {
	// IAM is global; the SDK only needs a valid region for endpoint resolution.
	iamCfg := cfg.Copy()
	iamCfg.Region = "us-east-1"
	client := iam.NewFromConfig(iamCfg)

	roles, err := collectRoles(ctx, client)
	if err != nil {
		return IAMResult{Err: fmt.Errorf("iam roles: %w", err)}
	}

	policies, err := collectPolicies(ctx, client)
	if err != nil {
		return IAMResult{Roles: roles, Err: fmt.Errorf("iam policies: %w", err)}
	}

	return IAMResult{Roles: roles, Policies: policies}
}

// ── Roles ─────────────────────────────────────────────────────────────────────

func collectRoles(ctx context.Context, client *iam.Client) ([]Resource, error) {
	paginator := iam.NewListRolesPaginator(client, &iam.ListRolesInput{})
	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, role := range page.Roles {
			resources = append(resources, normaliseRole(role))
		}
	}
	return resources, nil
}

func normaliseRole(role iamtypes.Role) Resource {
	name := aws.ToString(role.RoleName)
	arn := aws.ToString(role.Arn)

	// Usage status derived from RoleLastUsed.
	status := string(IAMStatusNever)
	lastUsedDate := ""
	lastUsedRegion := ""
	daysSinceUsed := "-1"

	if role.RoleLastUsed != nil && role.RoleLastUsed.LastUsedDate != nil {
		t := *role.RoleLastUsed.LastUsedDate
		days := int(math.Round(time.Since(t).Hours() / 24))
		lastUsedDate = t.Format("2006-01-02")
		lastUsedRegion = aws.ToString(role.RoleLastUsed.Region)
		daysSinceUsed = fmt.Sprintf("%d", days)
		switch {
		case days <= 30:
			status = string(IAMStatusActive)
		case days <= 90:
			status = string(IAMStatusStale)
		default:
			status = string(IAMStatusUnused)
		}
	}

	path := aws.ToString(role.Path)
	isServiceLinked := strings.HasPrefix(path, "/aws-service-role/") ||
		strings.HasPrefix(name, "AWSServiceRoleFor")

	createDate := ""
	if role.CreateDate != nil {
		createDate = role.CreateDate.Format("2006-01-02")
	}

	meta := map[string]string{
		"RoleId":          aws.ToString(role.RoleId),
		"CreateDate":      createDate,
		"Description":     aws.ToString(role.Description),
		"LastUsedDate":    lastUsedDate,
		"LastUsedRegion":  lastUsedRegion,
		"DaysSinceUsed":   daysSinceUsed,
		"UsageStatus":     status,
		"TrustPrincipal":  extractTrustPrincipal(role.AssumeRolePolicyDocument),
		"IsServiceLinked": fmt.Sprintf("%v", isServiceLinked),
		"Path":            path,
	}

	return Resource{
		ID:       arn,
		RawID:    name,
		Type:     TypeIAMRole,
		Name:     name,
		Region:   "global",
		Tags:     iamTagsToMap(role.Tags),
		Metadata: meta,
	}
}

// extractTrustPrincipal parses the URL-encoded trust policy and returns a
// short human-readable string for the first principal found.
func extractTrustPrincipal(encoded *string) string {
	if encoded == nil {
		return ""
	}
	raw, err := url.QueryUnescape(*encoded)
	if err != nil {
		raw = *encoded // try to parse as-is
	}

	var doc struct {
		Statement []struct {
			Principal json.RawMessage `json:"Principal"`
		} `json:"Statement"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil || len(doc.Statement) == 0 {
		return ""
	}

	p := doc.Statement[0].Principal

	// "Principal": "*"
	var star string
	if json.Unmarshal(p, &star) == nil {
		return star
	}

	// "Principal": { "Service": "...", "AWS": "..." }
	var obj map[string]json.RawMessage
	if json.Unmarshal(p, &obj) != nil {
		return ""
	}

	for _, key := range []string{"Service", "AWS", "Federated"} {
		raw, ok := obj[key]
		if !ok {
			continue
		}
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return shorten(s)
		}
		var arr []string
		if json.Unmarshal(raw, &arr) == nil && len(arr) > 0 {
			if len(arr) == 1 {
				return shorten(arr[0])
			}
			return shorten(arr[0]) + fmt.Sprintf(" +%d", len(arr)-1)
		}
	}
	return ""
}

// shorten trims verbose ARNs / service names to the most readable part.
func shorten(s string) string {
	// "arn:aws:iam::123456789012:root" → "123456789012"
	if strings.HasPrefix(s, "arn:aws:iam::") {
		parts := strings.Split(s, ":")
		if len(parts) >= 5 {
			acct := parts[4]
			rest := strings.Join(parts[5:], ":")
			if rest != "" && rest != "root" {
				return acct + "/" + rest
			}
			return acct
		}
	}
	// "lambda.amazonaws.com" → "lambda"
	if idx := strings.Index(s, ".amazonaws.com"); idx != -1 {
		return s[:idx]
	}
	return s
}

// ── Policies ──────────────────────────────────────────────────────────────────

func collectPolicies(ctx context.Context, client *iam.Client) ([]Resource, error) {
	// Scope=Local returns only customer-managed policies (not AWS-managed).
	paginator := iam.NewListPoliciesPaginator(client, &iam.ListPoliciesInput{
		Scope: iamtypes.PolicyScopeTypeLocal,
	})
	var resources []Resource
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, pol := range page.Policies {
			resources = append(resources, normalisePolicy(pol))
		}
	}
	return resources, nil
}

func normalisePolicy(pol iamtypes.Policy) Resource {
	name := aws.ToString(pol.PolicyName)
	arn := aws.ToString(pol.Arn)

	createDate := ""
	if pol.CreateDate != nil {
		createDate = pol.CreateDate.Format("2006-01-02")
	}
	updateDate := ""
	if pol.UpdateDate != nil {
		updateDate = pol.UpdateDate.Format("2006-01-02")
	}

	attachCount := int(aws.ToInt32(pol.AttachmentCount))

	meta := map[string]string{
		"PolicyId":        aws.ToString(pol.PolicyId),
		"CreateDate":      createDate,
		"UpdateDate":      updateDate,
		"AttachmentCount": fmt.Sprintf("%d", attachCount),
		"DefaultVersionId": aws.ToString(pol.DefaultVersionId),
		"Description":     aws.ToString(pol.Description),
	}

	return Resource{
		ID:       arn,
		RawID:    name,
		Type:     TypeIAMPolicy,
		Name:     name,
		Region:   "global",
		Metadata: meta,
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func iamTagsToMap(tags []iamtypes.Tag) map[string]string {
	m := make(map[string]string, len(tags))
	for _, t := range tags {
		if t.Key != nil && t.Value != nil {
			m[*t.Key] = *t.Value
		}
	}
	return m
}
