// Package web embeds the admin frontend static assets.
package web

import (
	"embed"
	"io/fs"
)

//go:embed index.html app.js style.css
var files embed.FS

// FS returns the embedded static assets rooted so that index.html is at "/".
func FS() fs.FS { return files }
