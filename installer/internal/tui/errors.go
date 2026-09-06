package tui

import "errors"

// errPasswordMismatch is surfaced on the account screen rather than deferred to
// validation, so the operator sees it while the fields are still in front of
// them.
var errPasswordMismatch = errors.New("passwords do not match")
