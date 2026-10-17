package assets

import (
	_ "embed"
)

//go:embed stealth.js
var StealthScript string

//go:embed popup_guard.js
var PopupGuardScript string

//go:embed readability.js
var ReadabilityJS string

//go:embed screencast_repaint_start.js
var ScreencastRepaintStartJS string

//go:embed screencast_repaint_stop.js
var ScreencastRepaintStopJS string

// AxeJS is the vendored axe-core accessibility engine (MPL-2.0), run in the
// isolated world by the /a11y/audit axe engine. See THIRD_PARTY_LICENSES.md.
//
//go:embed axe.min.js
var AxeJS string

// AxeVersion is the vendored axe-core version, echoed in the audit response so a
// caller can pin rule ids and help URLs to a known release. Keep it in step with
// the axe.min.js banner (`axe vX.Y.Z`) on every asset refresh.
const AxeVersion = "4.13.0"
