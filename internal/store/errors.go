package store

import "errors"

// ErrDisabled means this write needs database dynamic config but the database
// is off. The HTTP layer maps it to 503.
var ErrDisabled = errors.New("Database is not enabled, so the change cannot be saved")
