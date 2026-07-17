// Package assets provides the browser renderer bundled into the Go binary.
package assets

import (
	"bytes"
	"embed"
)

//go:embed dist/viewer.min.js dist/viewer.min.css manifest.json licenses/THIRD_PARTY_NOTICES.txt
var files embed.FS

var (
	viewerJS          = mustRead("dist/viewer.min.js")
	viewerCSS         = mustRead("dist/viewer.min.css")
	manifest          = mustRead("manifest.json")
	thirdPartyNotices = mustRead("licenses/THIRD_PARTY_NOTICES.txt")
)

// ViewerJS returns a fresh copy of the bundled browser renderer.
func ViewerJS() []byte {
	return bytes.Clone(viewerJS)
}

// ViewerCSS returns a fresh copy of the bundled viewer stylesheet.
func ViewerCSS() []byte {
	return bytes.Clone(viewerCSS)
}

// Manifest returns a fresh copy of the generated asset manifest.
func Manifest() []byte {
	return bytes.Clone(manifest)
}

// ThirdPartyNotices returns a fresh copy of the browser dependencies' notices.
func ThirdPartyNotices() []byte {
	return bytes.Clone(thirdPartyNotices)
}

func mustRead(name string) []byte {
	content, err := files.ReadFile(name)
	if err != nil {
		panic("read embedded browser asset " + name + ": " + err.Error())
	}
	return content
}
