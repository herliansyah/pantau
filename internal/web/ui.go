package web

import (
	"embed"
	"io/fs"
)

//go:embed static
var embeddedFS embed.FS

//go:embed static/index.html
var embeddedHTML []byte

// vendorFS returns a sub-filesystem rooted at static/vendor
func vendorFS() fs.FS {
	sub, err := fs.Sub(embeddedFS, "static/vendor")
	if err != nil {
		panic(err)
	}
	return sub
}
