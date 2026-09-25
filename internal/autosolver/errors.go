package autosolver

import (
	"errors"
	"time"
)

// ErrSpent marks a failure after a paid solve was bought. Retrying buys another,
// so the run stops instead.
var ErrSpent = errors.New("paid solve already spent")

// ErrPermanent marks a refusal no retry changes: an unsupported task, a bad key,
// an empty balance. The run stops asking that solver but still tries the next.
var ErrPermanent = errors.New("permanent solver refusal")

// classifiedError tags err with a class while keeping its message unchanged.
type classifiedError struct {
	err, class error
}

func (e classifiedError) Error() string   { return e.err.Error() }
func (e classifiedError) Unwrap() []error { return []error{e.err, e.class} }

// Spent tags err as ErrSpent.
func Spent(err error) error {
	if err == nil {
		return nil
	}
	return classifiedError{err: err, class: ErrSpent}
}

// Permanent tags err as ErrPermanent.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return classifiedError{err: err, class: ErrPermanent}
}

// retryLaterError is a refusal the same solver may pass after a pause, though
// not at once: a slider that refused a drag has never passed a drag seconds
// later, and has a minute later. It is ErrPermanent for the run; the caller
// can come back after the pause.
type retryLaterError struct {
	err   error
	after time.Duration
}

func (e retryLaterError) Error() string   { return e.err.Error() }
func (e retryLaterError) Unwrap() []error { return []error{e.err, ErrPermanent} }

// RetryLater tags err as a refusal worth retrying after the pause.
func RetryLater(err error, after time.Duration) error {
	if err == nil {
		return nil
	}
	return retryLaterError{err: err, after: after}
}

// RetryAfter is the pause err asks for, or 0 when it asks for none.
func RetryAfter(err error) time.Duration {
	var r retryLaterError
	if errors.As(err, &r) {
		return r.after
	}
	return 0
}
