// Package buildinfo holds values injected at link time by the release workflow.
package buildinfo

// Version is set with -ldflags "-X github.com/beedywool/vps-admin/internal/buildinfo.Version=v1.2.3".
var Version = "dev"

// Repo is the GitHub repository the agent updates itself from. The panel can point an
// install at a fork; this is the default and the fallback.
var Repo = "beedywool-vibecoded/vps-admin"
