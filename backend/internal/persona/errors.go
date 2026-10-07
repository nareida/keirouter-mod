package persona

import "errors"

// errEmptyKey is returned when an operation is given a blank API key id.
var errEmptyKey = errors.New("persona: api key id is required")
