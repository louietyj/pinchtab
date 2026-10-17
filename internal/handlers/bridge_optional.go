package handlers

import "github.com/pinchtab/pinchtab/internal/bridge"

type bridgeUnwrapper interface {
	Unwrap() bridge.BridgeAPI
}

func bridgeAs[T any](b bridge.BridgeAPI) (T, bool) {
	for b != nil {
		if capability, ok := any(b).(T); ok {
			return capability, true
		}
		wrapper, ok := b.(bridgeUnwrapper)
		if !ok {
			break
		}
		b = wrapper.Unwrap()
	}
	var zero T
	return zero, false
}
