// Package version holds the application version, injected at build time.
package version

// Version is the app version. It is set at build time via
//
//	-ldflags "-X govault/internal/version.Version=<git tag>"
//
// and defaults to "dev" for local/un-tagged builds. Do NOT hardcode the
// version anywhere else — read it from here (backend) or via the GetVersion()
// bound method (frontend).
var Version = "dev"
