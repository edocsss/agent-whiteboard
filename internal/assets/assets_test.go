package assets

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

type assetManifest struct {
	Versions map[string]string `json:"versions"`
	SHA256   map[string]string `json:"sha256"`
}

func TestEmbeddedAssetsMatchManifest(t *testing.T) {
	t.Parallel()

	manifestBytes := Manifest()
	var manifest assetManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}

	wantVersions := map[string]string{
		"markdown-it":  "14.2.0",
		"dompurify":    "3.4.12",
		"mermaid":      "11.15.0",
		"highlight.js": "11.11.1",
		"esbuild":      "0.28.1",
	}
	for dependency, want := range wantVersions {
		if got := manifest.Versions[dependency]; got != want {
			t.Errorf("manifest version %q = %q, want %q", dependency, got, want)
		}
	}

	assets := map[string][]byte{
		"dist/viewer.min.js":  ViewerJS(),
		"dist/viewer.min.css": ViewerCSS(),
	}
	for name, content := range assets {
		if len(content) == 0 {
			t.Errorf("%s is empty", name)
			continue
		}
		digest := sha256.Sum256(content)
		if got, want := hex.EncodeToString(digest[:]), manifest.SHA256[name]; got != want {
			t.Errorf("%s SHA-256 = %q, want %q", name, got, want)
		}
	}
}

func TestEmbeddedAssetsHaveNoExternalRuntimeReferences(t *testing.T) {
	t.Parallel()

	for name, content := range map[string][]byte{
		"viewer JavaScript": ViewerJS(),
		"viewer CSS":        ViewerCSS(),
	} {
		lower := bytes.ToLower(content)
		for _, forbidden := range []string{"http://", "https://", "<script src"} {
			if bytes.Contains(lower, []byte(forbidden)) {
				t.Errorf("%s contains forbidden external reference marker %q", name, forbidden)
			}
		}
		if regexp.MustCompile(`<link[^>]+(?:rel=["']?stylesheet|href=)`).Match(lower) {
			t.Errorf("%s contains a stylesheet link reference", name)
		}
	}
}

func TestEmbeddedAssetsCannotTerminateViewerInlineElements(t *testing.T) {
	t.Parallel()

	if bytes.Contains(bytes.ToLower(ViewerJS()), []byte("</script")) {
		t.Fatal("viewer JavaScript contains a closing script marker")
	}
	if bytes.Contains(bytes.ToLower(ViewerCSS()), []byte("</style")) {
		t.Fatal("viewer CSS contains a closing style marker")
	}
}

func TestEmbeddedAssetAccessorsReturnFreshCopies(t *testing.T) {
	t.Parallel()

	tests := map[string]func() []byte{
		"JavaScript": ViewerJS,
		"CSS":        ViewerCSS,
		"manifest":   Manifest,
	}
	for name, accessor := range tests {
		t.Run(name, func(t *testing.T) {
			first := accessor()
			second := accessor()
			if len(first) == 0 || len(second) == 0 {
				t.Fatal("embedded asset is empty")
			}
			original := second[0]
			first[0] ^= 0xff
			if second[0] != original {
				t.Fatal("mutating one result changed another result")
			}
			if third := accessor(); third[0] != original {
				t.Fatal("mutating a result changed embedded state")
			}
		})
	}
}

func TestManifestContainsOnlyLocalAssetNames(t *testing.T) {
	t.Parallel()

	manifest := strings.ToLower(string(Manifest()))
	if strings.Contains(manifest, "http://") || strings.Contains(manifest, "https://") {
		t.Fatal("manifest contains an external URL")
	}
}
