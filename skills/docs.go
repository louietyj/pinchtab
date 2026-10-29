// Package skills embeds the pinchtab agent skill, so `pinchtab docs` prints the
// docs that match the build an agent is running.
package skills

import "embed"

//go:embed pinchtab/SKILL.md pinchtab/references/*.md
var Pinchtab embed.FS
