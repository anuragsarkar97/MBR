// Package version holds build-time metadata injected via -ldflags.
package version

// These variables are set at build time via:
//
//	go build -ldflags "-X github.com/anuragsarkar97/mbr/internal/version.Version=..."
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)
