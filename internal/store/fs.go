package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/edocsss/agent-whiteboard/internal/common"
	imageDomain "github.com/edocsss/agent-whiteboard/internal/image"
	whiteboardDomain "github.com/edocsss/agent-whiteboard/internal/whiteboard"
)

const (
	metadataSchemaVersion = 1
	metadataFilename      = "metadata.json"
	directoryPermissions  = 0o700
	filePermissions       = 0o600
)

var (
	whiteboardGenerationPattern = regexp.MustCompile(`^source-[a-f0-9]{32}\.(md|html)$`)
	imageGenerationPattern      = regexp.MustCompile(`^content-[a-f0-9]{32}$`)
)

type Config struct {
	Root            string
	CleanupInterval time.Duration
	Clock           common.Clock
	Context         context.Context
}

type FS struct {
	root   string
	clock  common.Clock
	ctx    context.Context
	cancel context.CancelFunc

	closeOnce sync.Once
	mu        sync.RWMutex
	closed    bool
	closeErr  error
}

type whiteboardView struct{ fs *FS }
type imageView struct{ fs *FS }

var _ whiteboardDomain.Store = (*whiteboardView)(nil)
var _ imageDomain.Store = (*imageView)(nil)

type storedTime struct {
	Seconds     int64 `json:"seconds"`
	Nanoseconds int32 `json:"nanoseconds"`
}

type metadata struct {
	SchemaVersion   int         `json:"schema_version"`
	Kind            string      `json:"kind"`
	CreatedAt       storedTime  `json:"created_at"`
	UpdatedAt       storedTime  `json:"updated_at"`
	ExpiresAt       *storedTime `json:"expires_at"`
	ContentFilename string      `json:"content_filename"`
	Extension       string      `json:"extension"`
	MediaType       string      `json:"media_type"`
}

func NewFS(config Config) (*FS, error) {
	if config.Root == "" {
		return nil, invalidRequest("storage root is required")
	}
	if config.Context == nil {
		return nil, invalidRequest("storage context is required")
	}
	if err := config.Context.Err(); err != nil {
		return nil, err
	}
	if isNil(config.Clock) {
		return nil, invalidRequest("storage clock is required")
	}

	absoluteRoot, err := filepath.Abs(config.Root)
	if err != nil {
		return nil, storageUnavailable(err)
	}
	if err := os.MkdirAll(absoluteRoot, directoryPermissions); err != nil {
		return nil, storageUnavailable(err)
	}
	resolvedRoot, err := filepath.EvalSymlinks(absoluteRoot)
	if err != nil {
		return nil, storageUnavailable(err)
	}
	resolvedRoot, err = filepath.Abs(resolvedRoot)
	if err != nil {
		return nil, storageUnavailable(err)
	}
	info, err := os.Lstat(resolvedRoot)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, storageUnavailable(err)
	}
	if err := os.Chmod(resolvedRoot, directoryPermissions); err != nil {
		return nil, storageUnavailable(err)
	}

	ctx, cancel := context.WithCancel(config.Context)
	fs := &FS{root: resolvedRoot, clock: config.Clock, ctx: ctx, cancel: cancel}
	for _, name := range []string{"whiteboards", "images", ".readiness"} {
		path, pathErr := fs.containedPath(name)
		if pathErr != nil {
			cancel()
			return nil, storageUnavailable(pathErr)
		}
		if err := createManagedDirectory(path); err != nil {
			cancel()
			return nil, storageUnavailable(err)
		}
	}
	return fs, nil
}

func (fs *FS) Whiteboards() whiteboardDomain.Store { return &whiteboardView{fs: fs} }
func (fs *FS) Images() imageDomain.Store           { return &imageView{fs: fs} }

func (view *whiteboardView) Create(ctx context.Context, record whiteboardDomain.Whiteboard) error {
	if err := validateWhiteboardRecord(record); err != nil {
		return err
	}
	return view.fs.create(ctx, "whiteboards", record.ID, record.Source, whiteboardMetadata(record))
}

func (view *whiteboardView) Get(ctx context.Context, id string) (whiteboardDomain.Whiteboard, error) {
	if err := common.ValidateID(id); err != nil {
		return whiteboardDomain.Whiteboard{}, err
	}
	content, stored, err := view.fs.get(ctx, "whiteboards", id, "whiteboard")
	if err != nil {
		return whiteboardDomain.Whiteboard{}, err
	}
	return whiteboardDomain.Whiteboard{
		ID: id, Kind: whiteboardDomain.Kind(stored.Kind), Source: content,
		CreatedAt: stored.CreatedAt.time(), UpdatedAt: stored.UpdatedAt.time(), ExpiresAt: stored.ExpiresAt.timePtr(),
	}, nil
}

func (view *whiteboardView) Replace(ctx context.Context, record whiteboardDomain.Whiteboard) error {
	if err := validateWhiteboardRecord(record); err != nil {
		return err
	}
	return view.fs.replace(ctx, "whiteboards", record.ID, record.Source, whiteboardMetadata(record), "whiteboard")
}

func (view *whiteboardView) Delete(ctx context.Context, id string) error {
	if err := common.ValidateID(id); err != nil {
		return err
	}
	return view.fs.delete(ctx, "whiteboards", id, "whiteboard")
}

func (view *whiteboardView) Ready(ctx context.Context) error { return view.fs.Ready(ctx) }
func (view *whiteboardView) Close() error                    { return view.fs.Close() }

func (view *imageView) Create(ctx context.Context, record imageDomain.Image) error {
	if err := validateImageRecord(record); err != nil {
		return err
	}
	return view.fs.create(ctx, "images", record.ID, record.Content, imageMetadata(record))
}

func (view *imageView) Get(ctx context.Context, id string) (imageDomain.Image, error) {
	if err := common.ValidateID(id); err != nil {
		return imageDomain.Image{}, err
	}
	content, stored, err := view.fs.get(ctx, "images", id, "image")
	if err != nil {
		return imageDomain.Image{}, err
	}
	return imageDomain.Image{
		ID: id, Extension: stored.Extension, MediaType: stored.MediaType, Content: content,
		CreatedAt: stored.CreatedAt.time(), UpdatedAt: stored.UpdatedAt.time(), ExpiresAt: stored.ExpiresAt.timePtr(),
	}, nil
}

func (view *imageView) Replace(ctx context.Context, record imageDomain.Image) error {
	if err := validateImageRecord(record); err != nil {
		return err
	}
	return view.fs.replace(ctx, "images", record.ID, record.Content, imageMetadata(record), "image")
}

func (view *imageView) Delete(ctx context.Context, id string) error {
	if err := common.ValidateID(id); err != nil {
		return err
	}
	return view.fs.delete(ctx, "images", id, "image")
}

func (view *imageView) Ready(ctx context.Context) error { return view.fs.Ready(ctx) }
func (view *imageView) Close() error                    { return view.fs.Close() }

func (fs *FS) create(ctx context.Context, namespace, id string, content []byte, stored metadata) error {
	if err := fs.begin(ctx); err != nil {
		return err
	}
	parent, err := fs.namespacePath(namespace)
	if err != nil {
		return storageUnavailable(err)
	}
	resourceDir, err := fs.resourcePath(namespace, id)
	if err != nil {
		return err
	}
	if err := fs.verifyPath(parent, false); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	if err := os.Mkdir(resourceDir, directoryPermissions); err != nil {
		info, statErr := os.Lstat(resourceDir)
		if errors.Is(err, os.ErrExist) && statErr == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			return common.ErrIDCollision
		}
		return storageUnavailable(err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = os.RemoveAll(resourceDir)
		}
	}()
	if err := os.Chmod(resourceDir, directoryPermissions); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	if err := fs.commit(ctx, resourceDir, content, &stored, ""); err != nil {
		return err
	}
	committed = true
	return nil
}

func (fs *FS) get(ctx context.Context, namespace, id, expectedKind string) ([]byte, metadata, error) {
	if err := fs.begin(ctx); err != nil {
		return nil, metadata{}, err
	}
	resourceDir, err := fs.resourcePath(namespace, id)
	if err != nil {
		return nil, metadata{}, err
	}
	stored, err := fs.loadMetadata(ctx, resourceDir, expectedKind)
	if err != nil {
		return nil, metadata{}, err
	}
	contentPath, err := fs.containedPath(namespace, id, stored.ContentFilename)
	if err != nil {
		return nil, metadata{}, storageUnavailable(err)
	}
	if err := fs.verifyRegularFile(contentPath); err != nil {
		return nil, metadata{}, storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return nil, metadata{}, err
	}
	content, err := os.ReadFile(contentPath)
	if err != nil {
		return nil, metadata{}, storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return nil, metadata{}, err
	}
	return content, stored, nil
}

func (fs *FS) replace(ctx context.Context, namespace, id string, content []byte, stored metadata, expectedKind string) error {
	if err := fs.begin(ctx); err != nil {
		return err
	}
	resourceDir, err := fs.resourcePath(namespace, id)
	if err != nil {
		return err
	}
	old, err := fs.loadMetadata(ctx, resourceDir, expectedKind)
	if err != nil {
		return err
	}
	oldContent, err := fs.containedPath(namespace, id, old.ContentFilename)
	if err != nil {
		return storageUnavailable(err)
	}
	if err := fs.verifyRegularFile(oldContent); err != nil {
		return storageUnavailable(err)
	}
	return fs.commit(ctx, resourceDir, content, &stored, old.ContentFilename)
}

func (fs *FS) delete(ctx context.Context, namespace, id, expectedKind string) error {
	if err := fs.begin(ctx); err != nil {
		return err
	}
	resourceDir, err := fs.resourcePath(namespace, id)
	if err != nil {
		return err
	}
	if _, err := fs.loadMetadata(ctx, resourceDir, expectedKind); err != nil {
		return err
	}
	if err := fs.verifyTree(resourceDir); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	if err := os.RemoveAll(resourceDir); err != nil {
		return storageUnavailable(err)
	}
	return nil
}

func (fs *FS) Ready(ctx context.Context) error {
	if err := fs.begin(ctx); err != nil {
		return err
	}
	dir, err := fs.containedPath(".readiness")
	if err != nil {
		return storageUnavailable(err)
	}
	if err := fs.verifyPath(dir, false); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	probe, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return storageUnavailable(err)
	}
	probePath := probe.Name()
	defer os.Remove(probePath)
	if err := probe.Chmod(filePermissions); err != nil {
		_ = probe.Close()
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		_ = probe.Close()
		return err
	}
	if _, err := probe.Write([]byte("ready")); err != nil {
		_ = probe.Close()
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		_ = probe.Close()
		return err
	}
	if err := probe.Sync(); err != nil {
		_ = probe.Close()
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		_ = probe.Close()
		return err
	}
	if err := probe.Close(); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	if err := os.Remove(probePath); err != nil {
		return storageUnavailable(err)
	}
	return nil
}

func (fs *FS) Close() error {
	fs.closeOnce.Do(func() {
		fs.mu.Lock()
		fs.closed = true
		fs.mu.Unlock()
		fs.cancel()
	})
	return fs.closeErr
}

func (fs *FS) commit(ctx context.Context, resourceDir string, content []byte, stored *metadata, oldGeneration string) (returnErr error) {
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	generation, err := generationFilename(stored.Kind)
	if err != nil {
		return storageUnavailable(err)
	}
	generationPath, err := fs.containedResourceFile(resourceDir, generation)
	if err != nil {
		return storageUnavailable(err)
	}
	contentTemp, err := os.CreateTemp(resourceDir, ".content-temp-*")
	if err != nil {
		return storageUnavailable(err)
	}
	contentTempPath := contentTemp.Name()
	generationCommitted := false
	metadataCommitted := false
	defer func() {
		_ = contentTemp.Close()
		_ = os.Remove(contentTempPath)
		if generationCommitted && !metadataCommitted {
			_ = os.Remove(generationPath)
		}
	}()
	if err := contentTemp.Chmod(filePermissions); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	if err := writeAll(contentTemp, content); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	if err := contentTemp.Sync(); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	if err := contentTemp.Close(); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	if err := os.Rename(contentTempPath, generationPath); err != nil {
		return storageUnavailable(err)
	}
	generationCommitted = true
	stored.ContentFilename = generation
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	encoded, err := json.Marshal(stored)
	if err != nil {
		return storageUnavailable(err)
	}
	metadataTemp, err := os.CreateTemp(resourceDir, ".metadata-temp-*")
	if err != nil {
		return storageUnavailable(err)
	}
	metadataTempPath := metadataTemp.Name()
	defer func() {
		_ = metadataTemp.Close()
		_ = os.Remove(metadataTempPath)
	}()
	if err := metadataTemp.Chmod(filePermissions); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	if err := writeAll(metadataTemp, encoded); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	if err := metadataTemp.Sync(); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	if err := metadataTemp.Close(); err != nil {
		return storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return err
	}
	metadataPath, err := fs.containedResourceFile(resourceDir, metadataFilename)
	if err != nil {
		return storageUnavailable(err)
	}
	if err := os.Rename(metadataTempPath, metadataPath); err != nil {
		return storageUnavailable(err)
	}
	metadataCommitted = true
	if oldGeneration != "" && oldGeneration != generation {
		oldPath, pathErr := fs.containedResourceFile(resourceDir, oldGeneration)
		if pathErr == nil {
			_ = os.Remove(oldPath)
		}
	}
	return nil
}

func (fs *FS) loadMetadata(ctx context.Context, resourceDir, expectedKind string) (metadata, error) {
	if err := fs.verifyDirectory(resourceDir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return metadata{}, notFound()
		}
		return metadata{}, storageUnavailable(err)
	}
	metadataPath, err := fs.containedResourceFile(resourceDir, metadataFilename)
	if err != nil {
		return metadata{}, storageUnavailable(err)
	}
	if err := fs.verifyRegularFile(metadataPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return metadata{}, notFound()
		}
		return metadata{}, storageUnavailable(err)
	}
	if err := ctxErr(ctx, fs.ctx); err != nil {
		return metadata{}, err
	}
	encoded, err := os.ReadFile(metadataPath)
	if err != nil {
		return metadata{}, storageUnavailable(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var stored metadata
	if err := decoder.Decode(&stored); err != nil {
		return metadata{}, storageUnavailable(err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return metadata{}, storageUnavailable(err)
	}
	if err := validateMetadata(stored, expectedKind); err != nil {
		return metadata{}, storageUnavailable(err)
	}
	return stored, nil
}

func (fs *FS) begin(ctx context.Context) error {
	if ctx == nil {
		return invalidRequest("context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	fs.mu.RLock()
	closed := fs.closed
	fs.mu.RUnlock()
	if closed {
		return storageUnavailable(nil)
	}
	if err := fs.ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (fs *FS) namespacePath(namespace string) (string, error) {
	if namespace != "whiteboards" && namespace != "images" {
		return "", errors.New("invalid namespace")
	}
	return fs.containedPath(namespace)
}

func (fs *FS) resourcePath(namespace, id string) (string, error) {
	if err := common.ValidateID(id); err != nil {
		return "", err
	}
	if _, err := fs.namespacePath(namespace); err != nil {
		return "", storageUnavailable(err)
	}
	path, err := fs.containedPath(namespace, id)
	if err != nil {
		return "", storageUnavailable(err)
	}
	return path, nil
}

func (fs *FS) containedResourceFile(resourceDir, filename string) (string, error) {
	if filename == "" || filepath.Base(filename) != filename || filename == "." || filename == ".." {
		return "", errors.New("invalid internal filename")
	}
	relative, err := filepath.Rel(fs.root, resourceDir)
	if err != nil || escapesRoot(relative) {
		return "", errors.New("resource directory escapes root")
	}
	return fs.containedPath(relative, filename)
}

func (fs *FS) containedPath(elements ...string) (string, error) {
	target := filepath.Join(append([]string{fs.root}, elements...)...)
	relative, err := filepath.Rel(fs.root, target)
	if err != nil || escapesRoot(relative) {
		return "", errors.New("path escapes storage root")
	}
	return target, nil
}

func (fs *FS) verifyPath(path string, allowMissing bool) error {
	relative, err := filepath.Rel(fs.root, path)
	if err != nil || escapesRoot(relative) {
		return errors.New("path escapes storage root")
	}
	current := fs.root
	for _, component := range splitPath(relative) {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			if allowMissing && errors.Is(statErr, os.ErrNotExist) {
				return nil
			}
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("symlink in managed path")
		}
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	resolvedRelative, err := filepath.Rel(fs.root, resolved)
	if err != nil || escapesRoot(resolvedRelative) {
		return errors.New("resolved path escapes storage root")
	}
	return nil
}

func (fs *FS) verifyDirectory(path string) error {
	if err := fs.verifyPath(path, false); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("managed directory is not a directory")
	}
	return nil
}

func (fs *FS) verifyRegularFile(path string) error {
	if err := fs.verifyPath(path, false); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("managed file is not regular")
	}
	return nil
}

func (fs *FS) verifyTree(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symlink in managed tree")
		}
		contained, err := filepath.Rel(fs.root, path)
		if err != nil || escapesRoot(contained) {
			return errors.New("managed tree escapes storage root")
		}
		return nil
	})
}

func createManagedDirectory(path string) error {
	if err := os.Mkdir(path, directoryPermissions); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		info, statErr := os.Lstat(path)
		if statErr != nil {
			return statErr
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("managed path is not a real directory")
		}
	}
	return os.Chmod(path, directoryPermissions)
}

func validateWhiteboardRecord(record whiteboardDomain.Whiteboard) error {
	if err := common.ValidateID(record.ID); err != nil {
		return err
	}
	if record.Kind != whiteboardDomain.KindMarkdown && record.Kind != whiteboardDomain.KindHTML {
		return invalidRequest("invalid whiteboard kind")
	}
	return nil
}

func validateImageRecord(record imageDomain.Image) error {
	if err := common.ValidateID(record.ID); err != nil {
		return err
	}
	if !validImageFormat(record.Extension, record.MediaType) {
		return invalidRequest("invalid image format")
	}
	return nil
}

func validateMetadata(stored metadata, expectedKind string) error {
	if stored.SchemaVersion != metadataSchemaVersion {
		return errors.New("invalid metadata schema or kind")
	}
	if !stored.CreatedAt.valid() || !stored.UpdatedAt.valid() || (stored.ExpiresAt != nil && !stored.ExpiresAt.valid()) {
		return errors.New("invalid metadata timestamp")
	}
	switch expectedKind {
	case "whiteboard":
		if stored.Kind != string(whiteboardDomain.KindMarkdown) && stored.Kind != string(whiteboardDomain.KindHTML) {
			return errors.New("invalid whiteboard kind")
		}
		if !whiteboardGenerationPattern.MatchString(stored.ContentFilename) || stored.Extension != "" || stored.MediaType != "" {
			return errors.New("invalid whiteboard metadata")
		}
		if strings.HasSuffix(stored.ContentFilename, ".md") != (stored.Kind == string(whiteboardDomain.KindMarkdown)) {
			return errors.New("invalid whiteboard generation")
		}
	case "image":
		if stored.Kind != "image" || !imageGenerationPattern.MatchString(stored.ContentFilename) || !validImageFormat(stored.Extension, stored.MediaType) {
			return errors.New("invalid image metadata")
		}
	default:
		return errors.New("invalid metadata kind")
	}
	return nil
}

func whiteboardMetadata(record whiteboardDomain.Whiteboard) metadata {
	return metadata{
		SchemaVersion: metadataSchemaVersion, Kind: string(record.Kind),
		CreatedAt: fromTime(record.CreatedAt), UpdatedAt: fromTime(record.UpdatedAt), ExpiresAt: fromTimePtr(record.ExpiresAt),
	}
}

func imageMetadata(record imageDomain.Image) metadata {
	return metadata{
		SchemaVersion: metadataSchemaVersion, Kind: "image",
		CreatedAt: fromTime(record.CreatedAt), UpdatedAt: fromTime(record.UpdatedAt), ExpiresAt: fromTimePtr(record.ExpiresAt),
		Extension: record.Extension, MediaType: record.MediaType,
	}
}

func generationFilename(kind string) (string, error) {
	random := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return "", err
	}
	generation := hex.EncodeToString(random)
	switch kind {
	case string(whiteboardDomain.KindMarkdown):
		return "source-" + generation + ".md", nil
	case string(whiteboardDomain.KindHTML):
		return "source-" + generation + ".html", nil
	case "image":
		return "content-" + generation, nil
	default:
		return "", errors.New("invalid resource kind")
	}
}

func validImageFormat(extension, mediaType string) bool {
	formats := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".gif": "image/gif", ".webp": "image/webp"}
	return formats[extension] == mediaType && mediaType != ""
}

func fromTime(value time.Time) storedTime {
	return storedTime{Seconds: value.Unix(), Nanoseconds: int32(value.Nanosecond())}
}

func fromTimePtr(value *time.Time) *storedTime {
	if value == nil {
		return nil
	}
	result := fromTime(*value)
	return &result
}

func (value storedTime) valid() bool {
	return value.Nanoseconds >= 0 && value.Nanoseconds < int32(time.Second)
}

func (value storedTime) time() time.Time {
	return time.Unix(value.Seconds, int64(value.Nanoseconds)).UTC()
}

func (value *storedTime) timePtr() *time.Time {
	if value == nil {
		return nil
	}
	result := value.time()
	return &result
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple metadata values")
		}
		return err
	}
	return nil
}

func ctxErr(request, lifecycle context.Context) error {
	if err := request.Err(); err != nil {
		return err
	}
	return lifecycle.Err()
}

func writeAll(file *os.File, content []byte) error {
	for len(content) > 0 {
		written, err := file.Write(content)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		content = content[written:]
	}
	return nil
}

func splitPath(path string) []string {
	if path == "." || path == "" {
		return nil
	}
	return strings.Split(filepath.Clean(path), string(filepath.Separator))
}

func escapesRoot(relative string) bool {
	return relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func isNil(value any) bool {
	if value == nil {
		return true
	}
	kind := reflect.ValueOf(value).Kind()
	return (kind == reflect.Chan || kind == reflect.Func || kind == reflect.Interface || kind == reflect.Map || kind == reflect.Pointer || kind == reflect.Slice) && reflect.ValueOf(value).IsNil()
}

func invalidRequest(message string) error {
	return common.NewError(common.CodeInvalidRequest, message, nil)
}

func notFound() error {
	return common.NewError(common.CodeNotFound, "resource not found", nil)
}

func storageUnavailable(cause error) error {
	return common.NewError(common.CodeStorageUnavailable, "storage unavailable", cause)
}
