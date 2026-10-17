package httpx

import (
	"fmt"
	"net/http"

	"github.com/pinchtab/pinchtab/internal/remedy"
)

// DisabledEndpointMessage returns a consistent message for locked endpoint
// families. capability names the gated capability, which is not always the
// endpoint the caller asked for — /storage is gated by stateExport — so the
// sentence names the requirement instead of calling the label an endpoint.
func DisabledEndpointMessage(capability, setting string) string {
	return fmt.Sprintf("this endpoint requires the %s capability; enable %s in config to use it", capability, setting)
}

// Writing the config is not enough: the security block is snapshotted at boot and
// is not rebuilt on a config edit, so enabling a capability only takes effect once
// PinchTab restarts. The restart is named in the hint, not the executable remedy,
// because the restart command is mode-dependent while this gate is not: the same
// gate answers on a bridge and on a server, and `pinchtab server restart` stops a
// bridge and silently replaces it with a server. The handler cannot know how the
// caller launched this instance, so the remedy carries only the mode-neutral
// config write and the hint tells the caller to restart PinchTab to apply it.
var enableCapability = remedy.Declare("pinchtab config set <setting> true")

// DisabledEndpointDetails builds the error details every capability refusal
// carries, so the guidance is written once rather than restated at each gate.
func DisabledEndpointDetails(setting string) map[string]any {
	details := remedy.Details(
		fmt.Sprintf("Enable %s to use this feature, then restart PinchTab to apply the change.", setting),
		enableCapability.Fill(setting))
	details["setting"] = setting
	return details
}

// DisabledEndpointHandler returns a handler that reports a capability gate lock.
func DisabledEndpointHandler(capability, setting, code string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		ErrorCode(w, http.StatusForbidden, code,
			DisabledEndpointMessage(capability, setting), false,
			DisabledEndpointDetails(setting))
	}
}
