package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProductionCodeDoesNotGuardNilContexts(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	root := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	forbidden := regexp.MustCompile(`\b(?:ctx|config\.Context)\s*(?:==|!=)\s*nil\b`)

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		require.NoError(t, walkErr)
		if entry.IsDir() && (entry.Name() == ".git" || entry.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Falsef(t, forbidden.Match(content), "explicit nil-context guard in %s", path)
		return nil
	})
	require.NoError(t, err)
}
