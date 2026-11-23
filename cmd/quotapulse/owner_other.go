//go:build !linux

package main

// becomeDataOwner is for a container started as root (owner_linux.go);
// elsewhere a process runs as whoever started it.
func becomeDataOwner(string) error { return nil }
