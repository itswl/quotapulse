// Package buildinfo carries what the build knows about itself.
package buildinfo

// Version is set at build time with -ldflags "-X quotapulse/internal/buildinfo.Version=1.2.3".
var Version = "dev"

// UserAgent identifies QuotaPulse to the platforms it reads.
func UserAgent() string { return "QuotaPulse/" + Version }
