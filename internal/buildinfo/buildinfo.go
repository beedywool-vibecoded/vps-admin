// Package buildinfo holds values injected at link time by the release workflow.
package buildinfo

// Version is set with -ldflags "-X github.com/beedywool/vps-admin/internal/buildinfo.Version=v1.2.3".
var Version = "dev"

// Repo is the default update repository; the panel can point an install at a fork.
var Repo = "beedywool-vibecoded/vps-admin"
