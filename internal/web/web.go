package web

import "embed"

//go:embed index.html
var content embed.FS

// Content returns the embedded filesystem containing the Web UI.
func Content() embed.FS {
	return content
}
