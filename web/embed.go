// Package web carries the built dashboard inside the binary.
//
// The dashboard's source lives next to this file and is built into dist by
// `npm run build`. dist is not committed: the container build and CI produce
// it. A binary built without it still runs, and says what is missing.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets returns the built dashboard, or nil when it was not built.
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
