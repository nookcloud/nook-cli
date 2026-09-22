// Package helpers embeds the two data clients a nook's own server code uses. nook-init writes
// them beside the nook's files at boot, so they always match the server they talk to and an
// author never copies a stale one around.
package helpers

import _ "embed"

//go:embed nook_data.py
var Python string

//go:embed nook-data.js
var Node string

// Names are what nook-init calls them in the nook's directory. Both are reserved: a file the
// author deploys under either name is replaced.
const (
	PythonName = "nook_data.py"
	NodeName   = "nook-data.js"
)
