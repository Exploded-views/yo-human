// Package version holds build metadata, set at link time with -ldflags -X.
package version

var (
	// Version is the release version, or "dev" for an unversioned build.
	Version = "dev"
	// Commit is the short git SHA the binary was built from.
	Commit = "none"
)
