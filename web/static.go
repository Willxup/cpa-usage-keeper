package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

var Static fs.FS = mustSub(dist, "dist")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}
