package autosolver

import "errors"

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
