//go:build linux

package main

import (
	"fmt"
	"os"
	"syscall"
)

// imageUser is the user the published image runs as.
const imageUser = 65532

// becomeDataOwner gives up root, when the process was started as root: it
// becomes whoever owns the data directory. A new, empty directory that
// belongs to root — Docker makes the one it is told to mount, as root — is
// given to the image's own user first, which is what spares a new install a
// chown. Data that already belongs to root stays root's, and so does the
// process: that is a choice someone made, and with rootless Docker root is
// only the person running it.
func becomeDataOwner(dir string) error {
	if os.Getuid() != 0 {
		return nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("the data directory %s cannot be created: %w", dir, err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	uid, gid := int(owner.Uid), int(owner.Gid)
	if uid == 0 {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(entries) > 0 {
			return nil
		}
		if err := os.Chown(dir, imageUser, imageUser); err != nil {
			return fmt.Errorf("give the data directory %s to user %d: %w", dir, imageUser, err)
		}
		uid, gid = imageUser, imageUser
	}
	// Groups, then group, then user: once the user is given up, the rest
	// can no longer be.
	if err := syscall.Setgroups([]int{}); err != nil {
		return fmt.Errorf("give up root's groups: %w", err)
	}
	if err := syscall.Setgid(gid); err != nil {
		return fmt.Errorf("become group %d: %w", gid, err)
	}
	if err := syscall.Setuid(uid); err != nil {
		return fmt.Errorf("become user %d: %w", uid, err)
	}
	return nil
}
