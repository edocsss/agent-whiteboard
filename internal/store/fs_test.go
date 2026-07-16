package store

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/edocsss/agent-whiteboard/internal/common"
	imageDomain "github.com/edocsss/agent-whiteboard/internal/image"
	whiteboardDomain "github.com/edocsss/agent-whiteboard/internal/whiteboard"
	"github.com/stretchr/testify/require"
)

const (
	testID  = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	testID2 = "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
)

func TestFSCreateHonorsCanceledLifecycleBeforeInitialization(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	fs, err := NewFS(Config{Root: root, CleanupInterval: time.Hour, Clock: common.SystemClock{}, Context: ctx})
	require.Nil(t, fs)
	require.ErrorIs(t, err, context.Canceled)
	_, statErr := os.Stat(filepath.Join(root, "whiteboards"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestFSCreateRejectsConfiguredRootSymlink(t *testing.T) {
	target := t.TempDir()
	configuredRoot := filepath.Join(t.TempDir(), "storage-link")
	require.NoError(t, os.Symlink(target, configuredRoot))

	fs, err := NewFS(Config{Root: configuredRoot, CleanupInterval: time.Hour, Clock: common.SystemClock{}, Context: context.Background()})
	require.Nil(t, fs)
	assertCodeWithoutRoot(t, err, common.CodeStorageUnavailable, target)
	_, statErr := os.Stat(filepath.Join(target, "whiteboards"))
	require.ErrorIs(t, statErr, os.ErrNotExist)
}

func TestFSCreateRejectsPreexistingCategorySymlink(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "real-whiteboards"), 0o700))
	require.NoError(t, os.Symlink("real-whiteboards", filepath.Join(root, "whiteboards")))

	fs, err := NewFS(Config{Root: root, CleanupInterval: time.Hour, Clock: common.SystemClock{}, Context: context.Background()})
	require.Nil(t, fs)
	assertCodeWithoutRoot(t, err, common.CodeStorageUnavailable, root)
}

func TestPublishGenerationDoesNotClobberAndCanRetry(t *testing.T) {
	root, err := os.OpenRoot(t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.Close()) })
	require.NoError(t, root.WriteFile("temp", []byte("new"), 0o600))
	require.NoError(t, root.WriteFile("content-00000000000000000000000000000000", []byte("committed"), 0o600))

	err = publishGeneration(root, "temp", "content-00000000000000000000000000000000")
	require.ErrorIs(t, err, os.ErrExist)
	committed, readErr := root.ReadFile("content-00000000000000000000000000000000")
	require.NoError(t, readErr)
	require.Equal(t, []byte("committed"), committed)
	require.Equal(t, []byte("new"), mustReadRootFile(t, root, "temp"))

	require.NoError(t, publishGeneration(root, "temp", "content-11111111111111111111111111111111"))
	require.Equal(t, []byte("new"), mustReadRootFile(t, root, "content-11111111111111111111111111111111"))
	_, err = root.Lstat("temp")
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestFSCreateInitializesLayoutAndRoundTripsWhiteboard(t *testing.T) {
	root := t.TempDir()
	fs := newTestFS(t, root)
	created := time.Unix(1_700_000_000, 123_456_789).UTC()
	updated := time.Unix(1_700_000_001, 987_654_321).UTC()
	expires := time.Unix(1_800_000_000, 222_333_444).UTC()
	record := whiteboardDomain.Whiteboard{
		ID: testID, Kind: whiteboardDomain.KindMarkdown, Source: []byte("# exact\x00bytes\n"),
		CreatedAt: created, UpdatedAt: updated, ExpiresAt: &expires,
	}

	require.NoError(t, fs.Whiteboards().Create(context.Background(), record))
	got, err := fs.Whiteboards().Get(context.Background(), testID)
	require.NoError(t, err)
	require.Equal(t, record, got)

	for _, dir := range []string{root, filepath.Join(root, "whiteboards"), filepath.Join(root, "images"), filepath.Join(root, ".readiness"), filepath.Join(root, "whiteboards", testID)} {
		assertPermissions(t, dir, 0o700)
	}
	resourceDir := filepath.Join(root, "whiteboards", testID)
	entries, err := os.ReadDir(resourceDir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	var generation string
	for _, entry := range entries {
		assertPermissions(t, filepath.Join(resourceDir, entry.Name()), 0o600)
		if entry.Name() != "metadata.json" {
			generation = entry.Name()
		}
	}
	require.Regexp(t, regexp.MustCompile(`^source-[a-f0-9]{32}\.md$`), generation)
	require.Equal(t, record.Source, readFile(t, filepath.Join(resourceDir, generation)))

	var metadata map[string]any
	require.NoError(t, json.Unmarshal(readFile(t, filepath.Join(resourceDir, "metadata.json")), &metadata))
	require.Equal(t, float64(1), metadata["schema_version"])
	require.Equal(t, "markdown", metadata["kind"])
	require.Equal(t, generation, metadata["content_filename"])
	assertJSONTime(t, metadata["created_at"], created)
	assertJSONTime(t, metadata["updated_at"], updated)
	assertJSONTime(t, metadata["expires_at"], expires)
}

func TestFSCreateImageUsesExtensionlessInternalGeneration(t *testing.T) {
	root := t.TempDir()
	fs := newTestFS(t, root)
	record := imageDomain.Image{
		ID: testID, Extension: ".png", MediaType: "image/png", Content: []byte("not interpreted by storage"),
		CreatedAt: time.Unix(10, 11).UTC(), UpdatedAt: time.Unix(12, 13).UTC(),
	}

	require.NoError(t, fs.Images().Create(context.Background(), record))
	got, err := fs.Images().Get(context.Background(), testID)
	require.NoError(t, err)
	require.Equal(t, record, got)

	resourceDir := filepath.Join(root, "images", testID)
	entries, err := os.ReadDir(resourceDir)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	for _, entry := range entries {
		if entry.Name() != "metadata.json" {
			require.Regexp(t, regexp.MustCompile(`^content-[a-f0-9]{32}$`), entry.Name())
			require.NotContains(t, entry.Name(), record.Extension)
		}
	}
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(readFile(t, filepath.Join(resourceDir, "metadata.json")), &metadata))
	require.Equal(t, "image", metadata["kind"])
	require.Equal(t, ".png", metadata["extension"])
	require.Equal(t, "image/png", metadata["media_type"])
	require.Nil(t, metadata["expires_at"])
}

func TestFSCreateCollisionClassification(t *testing.T) {
	root := t.TempDir()
	fs := newTestFS(t, root)
	record := whiteboardDomain.Whiteboard{ID: testID, Kind: whiteboardDomain.KindHTML, Source: []byte("<!doctype html>"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, fs.Whiteboards().Create(context.Background(), record))

	err := fs.Whiteboards().Create(context.Background(), record)
	require.ErrorIs(t, err, common.ErrIDCollision)
	require.False(t, common.HasCode(err, common.CodeStorageUnavailable))

	linkIDDir := filepath.Join(root, "whiteboards", testID2)
	require.NoError(t, os.Symlink(filepath.Join(root, "images"), linkIDDir))
	err = fs.Whiteboards().Create(context.Background(), whiteboardDomain.Whiteboard{ID: testID2, Kind: whiteboardDomain.KindHTML})
	assertCodeWithoutRoot(t, err, common.CodeStorageUnavailable, root)
	require.NotErrorIs(t, err, common.ErrIDCollision)
}

func TestFSGetMissingAndInvalidIDs(t *testing.T) {
	fs := newTestFS(t, t.TempDir())
	_, err := fs.Whiteboards().Get(context.Background(), testID)
	assertCodeWithoutRoot(t, err, common.CodeNotFound, "")
	_, err = fs.Images().Get(context.Background(), "../outside")
	require.True(t, common.HasCode(err, common.CodeInvalidRequest))
	require.Equal(t, "invalid resource id", err.Error())
}

func TestFSPathSafetyRejectsManagedSymlinks(t *testing.T) {
	t.Run("resource directory", func(t *testing.T) {
		root := t.TempDir()
		fs := newTestFS(t, root)
		outside := t.TempDir()
		require.NoError(t, os.Symlink(outside, filepath.Join(root, "images", testID)))
		_, err := fs.Images().Get(context.Background(), testID)
		assertCodeWithoutRoot(t, err, common.CodeStorageUnavailable, root)
	})

	t.Run("metadata", func(t *testing.T) {
		root := t.TempDir()
		fs := newTestFS(t, root)
		dir := filepath.Join(root, "whiteboards", testID)
		require.NoError(t, os.Mkdir(dir, 0o700))
		outside := filepath.Join(t.TempDir(), "metadata.json")
		require.NoError(t, os.WriteFile(outside, []byte(`{}`), 0o600))
		require.NoError(t, os.Symlink(outside, filepath.Join(dir, "metadata.json")))
		_, err := fs.Whiteboards().Get(context.Background(), testID)
		assertCodeWithoutRoot(t, err, common.CodeStorageUnavailable, root)
	})

	t.Run("generation", func(t *testing.T) {
		root := t.TempDir()
		fs := newTestFS(t, root)
		record := whiteboardDomain.Whiteboard{ID: testID, Kind: whiteboardDomain.KindMarkdown, Source: []byte("safe"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
		require.NoError(t, fs.Whiteboards().Create(context.Background(), record))
		dir := filepath.Join(root, "whiteboards", testID)
		metadata := decodeMetadata(t, filepath.Join(dir, "metadata.json"))
		generation := metadata["content_filename"].(string)
		require.NoError(t, os.Remove(filepath.Join(dir, generation)))
		outside := filepath.Join(t.TempDir(), "outside")
		require.NoError(t, os.WriteFile(outside, []byte("escaped"), 0o600))
		require.NoError(t, os.Symlink(outside, filepath.Join(dir, generation)))
		_, err := fs.Whiteboards().Get(context.Background(), testID)
		assertCodeWithoutRoot(t, err, common.CodeStorageUnavailable, root)
		linkInfo, statErr := os.Lstat(filepath.Join(dir, generation))
		require.NoError(t, statErr)
		require.NotZero(t, linkInfo.Mode()&os.ModeSymlink)
		require.Equal(t, []byte("escaped"), readFile(t, outside))
	})
}

func TestFSGetValidatesMetadataAndCommittedFilename(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "schema", mutate: func(m map[string]any) { m["schema_version"] = 2 }},
		{name: "kind", mutate: func(m map[string]any) { m["kind"] = "image" }},
		{name: "timestamp nanoseconds", mutate: func(m map[string]any) { m["created_at"] = map[string]any{"seconds": 1, "nanoseconds": 1_000_000_000} }},
		{name: "filename traversal", mutate: func(m map[string]any) { m["content_filename"] = "../../outside.md" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			fs := newTestFS(t, root)
			record := whiteboardDomain.Whiteboard{ID: testID, Kind: whiteboardDomain.KindMarkdown, Source: []byte("safe"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
			require.NoError(t, fs.Whiteboards().Create(context.Background(), record))
			path := filepath.Join(root, "whiteboards", testID, "metadata.json")
			metadata := decodeMetadata(t, path)
			tt.mutate(metadata)
			encoded, err := json.Marshal(metadata)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, encoded, 0o600))
			_, err = fs.Whiteboards().Get(context.Background(), testID)
			assertCodeWithoutRoot(t, err, common.CodeStorageUnavailable, root)
		})
	}
}

func TestFSReplaceCommitsNewGenerationAndDeleteRemovesResource(t *testing.T) {
	root := t.TempDir()
	fs := newTestFS(t, root)
	old := imageDomain.Image{ID: testID, Extension: ".png", MediaType: "image/png", Content: []byte("old"), CreatedAt: time.Unix(1, 2), UpdatedAt: time.Unix(3, 4)}
	require.NoError(t, fs.Images().Create(context.Background(), old))
	dir := filepath.Join(root, "images", testID)
	oldGeneration := decodeMetadata(t, filepath.Join(dir, "metadata.json"))["content_filename"].(string)
	replacement := imageDomain.Image{ID: testID, Extension: ".jpg", MediaType: "image/jpeg", Content: []byte("new"), CreatedAt: old.CreatedAt, UpdatedAt: time.Unix(5, 6)}

	require.NoError(t, fs.Images().Replace(context.Background(), replacement))
	got, err := fs.Images().Get(context.Background(), testID)
	require.NoError(t, err)
	require.Equal(t, replacement.ID, got.ID)
	require.Equal(t, replacement.Extension, got.Extension)
	require.Equal(t, replacement.MediaType, got.MediaType)
	require.Equal(t, replacement.Content, got.Content)
	require.True(t, replacement.CreatedAt.Equal(got.CreatedAt))
	require.True(t, replacement.UpdatedAt.Equal(got.UpdatedAt))
	require.Nil(t, got.ExpiresAt)
	newGeneration := decodeMetadata(t, filepath.Join(dir, "metadata.json"))["content_filename"].(string)
	require.NotEqual(t, oldGeneration, newGeneration)
	_, err = os.Stat(filepath.Join(dir, oldGeneration))
	require.ErrorIs(t, err, os.ErrNotExist)

	require.NoError(t, fs.Images().Delete(context.Background(), testID))
	_, err = os.Stat(dir)
	require.ErrorIs(t, err, os.ErrNotExist)
	err = fs.Images().Delete(context.Background(), testID)
	assertCodeWithoutRoot(t, err, common.CodeNotFound, root)
}

func TestFSContextCancellationPreservesCommittedGeneration(t *testing.T) {
	root := t.TempDir()
	fs := newTestFS(t, root)
	record := whiteboardDomain.Whiteboard{ID: testID, Kind: whiteboardDomain.KindHTML, Source: []byte("old"), CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, fs.Whiteboards().Create(context.Background(), record))
	dir := filepath.Join(root, "whiteboards", testID)
	before := decodeMetadata(t, filepath.Join(dir, "metadata.json"))["content_filename"]
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	replacement := record
	replacement.Source = []byte("new")

	require.ErrorIs(t, fs.Whiteboards().Replace(canceled, replacement), context.Canceled)
	after := decodeMetadata(t, filepath.Join(dir, "metadata.json"))["content_filename"]
	require.Equal(t, before, after)
	got, err := fs.Whiteboards().Get(context.Background(), testID)
	require.NoError(t, err)
	require.Equal(t, record.ID, got.ID)
	require.Equal(t, record.Kind, got.Kind)
	require.Equal(t, record.Source, got.Source)
	require.True(t, record.CreatedAt.Equal(got.CreatedAt))
	require.True(t, record.UpdatedAt.Equal(got.UpdatedAt))
	require.Nil(t, got.ExpiresAt)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	require.ErrorIs(t, fs.Whiteboards().Delete(canceled, testID), context.Canceled)
	require.ErrorIs(t, fs.Whiteboards().Ready(canceled), context.Canceled)
	_, err = fs.Whiteboards().Get(canceled, testID)
	require.ErrorIs(t, err, context.Canceled)
}

func TestFSReadinessAndSharedIdempotentClose(t *testing.T) {
	root := t.TempDir()
	fs := newTestFS(t, root)
	require.NoError(t, fs.Whiteboards().Ready(context.Background()))
	require.Empty(t, readDirNames(t, filepath.Join(root, ".readiness")))
	require.NoError(t, fs.Images().Close())
	require.NoError(t, fs.Whiteboards().Close())
	err := fs.Images().Ready(context.Background())
	assertCodeWithoutRoot(t, err, common.CodeStorageUnavailable, root)
}

func newTestFS(t *testing.T, root string) *FS {
	t.Helper()
	fs, err := NewFS(Config{Root: root, CleanupInterval: time.Hour, Clock: common.SystemClock{}, Context: context.Background()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, fs.Close()) })
	return fs
}

func mustReadRootFile(t *testing.T, root *os.Root, name string) []byte {
	t.Helper()
	content, err := root.ReadFile(name)
	require.NoError(t, err)
	return content
}

func assertPermissions(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Lstat(path)
	require.NoError(t, err)
	require.Equal(t, want, info.Mode().Perm(), path)
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	content, err := os.ReadFile(path)
	require.NoError(t, err)
	return content
}

func decodeMetadata(t *testing.T, path string) map[string]any {
	t.Helper()
	var metadata map[string]any
	require.NoError(t, json.Unmarshal(readFile(t, path), &metadata))
	return metadata
}

func assertJSONTime(t *testing.T, value any, want time.Time) {
	t.Helper()
	object, ok := value.(map[string]any)
	require.True(t, ok)
	require.Equal(t, float64(want.Unix()), object["seconds"])
	require.Equal(t, float64(want.Nanosecond()), object["nanoseconds"])
}

func assertCodeWithoutRoot(t *testing.T, err error, code common.ErrorCode, root string) {
	t.Helper()
	require.Error(t, err)
	require.True(t, common.HasCode(err, code), "error: %v", err)
	if root != "" {
		require.NotContains(t, err.Error(), root)
	}
}

func readDirNames(t *testing.T, path string) []string {
	t.Helper()
	entries, err := os.ReadDir(path)
	require.NoError(t, err)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
