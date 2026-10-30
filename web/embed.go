// Package web embeds the built Vue frontend (web/dist).
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
