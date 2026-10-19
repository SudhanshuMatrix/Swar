package catalog

import (
	"errors"
	"fmt"
)

// Kind classifies an error so the HTTP layer can map it to a status code
// and a safe message without leaking upstream details.
type Kind string

const (
	KindInvalid     Kind = "invalid_request"
	KindNotFound    Kind = "not_found"
	KindUnavailable Kind = "unavailable"    // feature disabled or source restricted
	KindRateLimited Kind = "rate_limited"   // upstream throttled us
	KindUpstream    Kind = "upstream_error" // upstream broken / malformed
	KindTimeout     Kind = "upstream_timeout"
)

type Error struct {
	Kind    Kind
	Message string // safe to show to clients
	Err     error  // internal cause, logged but never returned to clients
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Kind, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func (e *Error) Unwrap() error { return e.Err }

func NewError(kind Kind, msg string, cause error) *Error {
	return &Error{Kind: kind, Message: msg, Err: cause}
}

func NotFound(msg string) *Error    { return &Error{Kind: KindNotFound, Message: msg} }
func Invalid(msg string) *Error     { return &Error{Kind: KindInvalid, Message: msg} }
func Unavailable(msg string) *Error { return &Error{Kind: KindUnavailable, Message: msg} }

// KindOf returns the Kind of err, or "" if it is not a catalog error.
func KindOf(err error) Kind {
	var ce *Error
	if errors.As(err, &ce) {
		return ce.Kind
	}
	return ""
}
