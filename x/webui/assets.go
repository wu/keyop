package webui

import (
	"embed"
	"io/fs"
)

//go:embed resources
var embeddedAssets embed.FS

// resourcesFS returns an fs.FS rooted at the embedded resources directory.
func resourcesFS() fs.FS {
	sub, _ := fs.Sub(embeddedAssets, "resources")
	return sub
}
