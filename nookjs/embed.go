// Package nookjs embeds the browser client that every nook serves at /_nook/nook.js.
package nookjs

import _ "embed"

//go:embed nook.js
var JS string
