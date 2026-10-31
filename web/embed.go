// Package web embeds the static frontend (Alpine.js, no build step).
package web

import "embed"

//go:embed all:static
var Static embed.FS
