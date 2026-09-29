package store

import "errors"

// Implementation note.
// Implementation note.
var ErrDisabled = errors.New("Database is not enabled, so the change cannot be saved")
