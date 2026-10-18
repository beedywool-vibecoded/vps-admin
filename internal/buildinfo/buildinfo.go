// Package buildinfo holds values injected at link time by the release workflow.
package buildinfo

// Version is set with -ldflags "-X github.com/beedywool/vps-admin/internal/buildinfo.Version=v1.2.3".
var Version = "dev"

// Repo is the GitHub repository the agent updates itself from.
var Repo = "beedywool/vps-admin"
