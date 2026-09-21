// Package skill embeds the agent skill file so nookd can serve it.
package skill

import _ "embed"

//go:embed SKILL.md
var Markdown string
