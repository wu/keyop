package links

import "errors"

// ErrUnknownAction is returned when an unknown action is requested.
var ErrUnknownAction = errors.New("unknown action")

// ErrMissingURL is returned when a URL is required but not provided.
var ErrMissingURL = errors.New("url is required")

// ErrMissingID is returned when an ID is required but not provided.
var ErrMissingID = errors.New("id is required")
