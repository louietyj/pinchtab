package bridge

import (
	"errors"
	"fmt"
)

var ErrUnknownAction = errors.New("unknown action")

// TabLimitError is returned when a new tab cannot be created because
// the configured limit has been reached and the eviction policy is "reject".
// HTTP handlers should map this to 429 Too Many Requests.
type TabLimitError struct {
	Current int
	Max     int
}

func (e *TabLimitError) Error() string {
	return fmt.Sprintf("tab limit reached (%d/%d)", e.Current, e.Max)
}

type ErrDialogBlocking struct {
	DialogType    string
	DialogMessage string
}

func (e *ErrDialogBlocking) Error() string {
	return fmt.Sprintf("click blocked by JavaScript dialog (%s: %q)", e.DialogType, e.DialogMessage)
}
