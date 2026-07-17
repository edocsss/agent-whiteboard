# agent-whiteboard Core Domains and Filesystem Storage Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement deterministic common primitives, whiteboard and image domain services, externally implementable store interfaces, and the concurrent expiring filesystem store.

**Architecture:** Business domains own their models, operations, and required store interfaces. The filesystem package depends on those interfaces and implements them in one `fs.go`, using per-resource standard-library locks, immutable content generations, atomic metadata replacement, and store-owned cleanup.

**Tech Stack:** Go 1.25+, standard library, `golang.org/x/net/html` v0.57.0, `golang.org/x/image/webp` v0.44.0, Mockery v3.7.1, Testify v1.11.1

## Global Constraints

- Work on a new Git branch; do not use a Git worktree.
- Module path is `github.com/edocsss/agent-whiteboard`.
- All service and store methods accept `context.Context` first.
- Use constructor dependency injection; no mutable package globals.
- IDs are 24 random bytes encoded as 32-character unpadded URL-safe Base64.
- Default expiration is 86,400 seconds; `0` is permanent; negative values are invalid.
- Supported images are PNG, JPEG, GIF, and WebP; SVG is rejected.
- One process owns one filesystem root.
- Use only standard-library synchronization primitives.
- Generated mocks are checked in; full filesystem tests use real `t.TempDir()` storage.

---

### Task 1: Bootstrap the Go module and stable error contract

**Files:**
- Create: `go.mod`
- Create: `internal/common/errors.go`
- Create: `internal/common/errors_test.go`
- Create: `.mockery.yaml`

**Interfaces:**
- Produces: `common.ErrorCode`, `common.Error`, `common.NewError`, `common.HasCode`, and the internal `common.ErrIDCollision` sentinel for every later task.

- [ ] **Step 1: Write the failing error-contract test**

```go
func TestErrorWrapAndCode(t *testing.T) {
    cause := errors.New("disk failed")
    err := common.NewError(common.CodeStorageUnavailable, "storage unavailable", cause)
    require.ErrorIs(t, err, cause)
    require.True(t, common.HasCode(err, common.CodeStorageUnavailable))
    require.Equal(t, "storage unavailable", err.Error())
}
```

- [ ] **Step 2: Create the module and pinned test dependencies**

```go
module github.com/edocsss/agent-whiteboard

go 1.25.0

require (
    github.com/stretchr/testify v1.11.1
)
```

Run: `go mod tidy`

Expected: `go.sum` is created and dependency resolution succeeds.

- [ ] **Step 3: Run the test to verify it fails**

Run: `go test ./internal/common -run TestErrorWrapAndCode -v`

Expected: FAIL because `internal/common` does not yet define the error contract.

- [ ] **Step 4: Implement the stable error contract**

```go
type ErrorCode string

const (
    CodeInvalidRequest       ErrorCode = "invalid_request"
    CodeNotFound             ErrorCode = "not_found"
    CodeContentTooLarge      ErrorCode = "content_too_large"
    CodeUnsupportedMediaType ErrorCode = "unsupported_media_type"
    CodeStorageUnavailable   ErrorCode = "storage_unavailable"
    CodeInternal             ErrorCode = "internal_error"
)

type Error struct {
    Code    ErrorCode
    Message string
    Err     error
}

var ErrIDCollision = errors.New("resource id collision")

func (e *Error) Error() string { return e.Message }
func (e *Error) Unwrap() error { return e.Err }

func NewError(code ErrorCode, message string, cause error) *Error {
    return &Error{Code: code, Message: message, Err: cause}
}

func HasCode(err error, code ErrorCode) bool {
    var target *Error
    return errors.As(err, &target) && target.Code == code
}
```

- [ ] **Step 5: Add Mockery configuration**

```yaml
template: testify
formatter: goimports
force-file-write: true
packages: {}
```

Packages are added only after their interfaces exist so the pinned generator is runnable at every checkpoint.

- [ ] **Step 6: Verify and commit**

Run: `go test ./internal/common -v`

Expected: PASS.

```bash
git add go.mod go.sum .mockery.yaml internal/common
git commit -m "feat: establish core error contract"
```

### Task 2: Implement clock, IDs, and expiration semantics

**Files:**
- Create: `internal/common/clock.go`
- Create: `internal/common/id.go`
- Create: `internal/common/expiration.go`
- Create: `internal/common/id_test.go`
- Create: `internal/common/expiration_test.go`
- Generate: `internal/common/mocks/mock_clock.go`
- Generate: `internal/common/mocks/mock_id_generator.go`
- Modify: `.mockery.yaml`

**Interfaces:**
- Produces: `Clock.Now() time.Time`, `IDGenerator.NewID() (string, error)`, `ValidateID(string) error`, `ResolveCreateExpiration`, and `ResolveUpdateExpiration`.

- [ ] **Step 1: Write failing table-driven tests**

```go
func TestCryptoIDGenerator(t *testing.T) {
    id, err := (common.CryptoIDGenerator{}).NewID()
    require.NoError(t, err)
    require.Len(t, id, 32)
    require.NoError(t, common.ValidateID(id))
    require.NotContains(t, id, "=")
}

func TestResolveExpiration(t *testing.T) {
    now := time.Unix(1_700_000_000, 0).UTC()
    zero := int64(0)
    oneHour := int64(3600)
    permanent, err := common.ResolveCreateExpiration(now, 86400, &zero)
    require.NoError(t, err)
    require.Nil(t, permanent)
    exp, err := common.ResolveCreateExpiration(now, 86400, &oneHour)
    require.NoError(t, err)
    require.Equal(t, now.Add(time.Hour), *exp)
    preserved, err := common.ResolveUpdateExpiration(now, exp, nil)
    require.NoError(t, err)
    require.Equal(t, exp, preserved)
}
```

- [ ] **Step 2: Run tests to verify failure**

Run: `go test ./internal/common -run 'TestCryptoIDGenerator|TestResolveExpiration' -v`

Expected: FAIL with undefined symbols.

- [ ] **Step 3: Implement injectable time and IDs**

```go
type Clock interface { Now() time.Time }
type SystemClock struct{}
func (SystemClock) Now() time.Time { return time.Now().UTC() }

type IDGenerator interface { NewID() (string, error) }
type CryptoIDGenerator struct{}

func (CryptoIDGenerator) NewID() (string, error) {
    raw := make([]byte, 24)
    if _, err := io.ReadFull(rand.Reader, raw); err != nil { return "", err }
    return base64.RawURLEncoding.EncodeToString(raw), nil
}

func ValidateID(id string) error {
    if len(id) != 32 { return NewError(CodeInvalidRequest, "invalid resource id", nil) }
    raw, err := base64.RawURLEncoding.DecodeString(id)
    if err != nil || len(raw) != 24 { return NewError(CodeInvalidRequest, "invalid resource id", err) }
    return nil
}
```

- [ ] **Step 4: Implement expiration without `time.Duration` overflow**

```go
func expirationAt(now time.Time, seconds int64) (*time.Time, error) {
    if seconds < 0 { return nil, NewError(CodeInvalidRequest, "expiration must not be negative", nil) }
    if seconds == 0 { return nil, nil }
    unix := now.Unix()
    if unix > 0 && seconds > math.MaxInt64-unix {
        return nil, NewError(CodeInvalidRequest, "expiration overflows unix time", nil)
    }
    value := time.Unix(unix+seconds, 0).UTC()
    return &value, nil
}

func ResolveCreateExpiration(now time.Time, defaultSeconds int64, supplied *int64) (*time.Time, error) {
    if supplied == nil { return expirationAt(now, defaultSeconds) }
    return expirationAt(now, *supplied)
}

func ResolveUpdateExpiration(now time.Time, current *time.Time, supplied *int64) (*time.Time, error) {
    if supplied == nil { return current, nil }
    return expirationAt(now, *supplied)
}

func IsExpired(now time.Time, expiresAt *time.Time) bool {
    return expiresAt != nil && !now.Before(*expiresAt)
}
```

- [ ] **Step 5: Configure and generate common dependency mocks**

Add the common package to `.mockery.yaml` with `dir: "{{.InterfaceDir}}/mocks"` and `pkgname: mocks`. Add `Clock` with `filename: mock_clock.go`, `structname: MockClock`; add `IDGenerator` with `filename: mock_id_generator.go`, `structname: MockIDGenerator`.

Run: `go run github.com/vektra/mockery/v3@v3.7.1`

Expected: the two Testify-backed generated files compile.

- [ ] **Step 6: Verify edge cases and commit**

Run: `go test ./internal/common -v`

Expected: PASS, including negative and overflow cases.

```bash
git add internal/common .mockery.yaml
git commit -m "feat: add identity and expiration primitives"
```

### Task 3: Implement the whiteboard domain service

**Files:**
- Create: `internal/whiteboard/model.go`
- Create: `internal/whiteboard/store.go`
- Create: `internal/whiteboard/markdown.go`
- Create: `internal/whiteboard/html.go`
- Create: `internal/whiteboard/service.go`
- Create: `internal/whiteboard/service_test.go`
- Generate: `internal/whiteboard/mocks/mock_store.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: `common.Clock`, `common.IDGenerator`, expiration functions, and `whiteboard.Store`.
- Produces: `Service.CreateMarkdown`, `CreateHTML`, `Get`, `Update`, and `Delete` for plans 2 and 3.

- [ ] **Step 1: Define model and store contracts**

```go
type Kind string
const ( KindMarkdown Kind = "markdown"; KindHTML Kind = "html" )

type Whiteboard struct {
    ID string; Kind Kind; Source []byte
    CreatedAt time.Time; UpdatedAt time.Time; ExpiresAt *time.Time
}

type Store interface {
    Create(context.Context, Whiteboard) error
    Get(context.Context, string) (Whiteboard, error)
    Replace(context.Context, Whiteboard) error
    Delete(context.Context, string) error
    Ready(context.Context) error
    Close() error
}

type CreateInput struct { Source []byte; ExpiresInSeconds *int64 }
type UpdateInput struct { ID string; Kind Kind; Source []byte; ExpiresInSeconds *int64 }
type Result struct { ID string; Kind Kind; CreatedAt, UpdatedAt time.Time; ExpiresAt *time.Time }
```

- [ ] **Step 2: Configure/generate the store mock and write failing service tests**

Add the whiteboard package to `.mockery.yaml` with `dir: "{{.InterfaceDir}}/mocks"` and `pkgname: mocks`; configure `Store` with `filename: mock_store.go` and `structname: MockStore`.

Run: `go run github.com/vektra/mockery/v3@v3.7.1`

Use generated `common/mocks.MockClock`, `common/mocks.MockIDGenerator`, and `whiteboard/mocks.MockStore`. Add tests for exact context propagation, Markdown creation, permanent creation, omitted-update expiration preservation, wrong-kind update, last-write-wins replacement, expired get/update as not found, and deletion.

```go
store.EXPECT().Create(mock.Anything, mock.MatchedBy(func(w whiteboard.Whiteboard) bool {
    return w.Kind == whiteboard.KindMarkdown && string(w.Source) == "# Hello"
})).Return(nil).Once()
result, err := service.CreateMarkdown(ctx, whiteboard.CreateInput{Source: []byte("# Hello")})
require.NoError(t, err)
require.Equal(t, whiteboard.KindMarkdown, result.Kind)
```

- [ ] **Step 3: Run tests to verify failure**

Run: `go test ./internal/whiteboard -v`

Expected: FAIL because the service is not implemented.

- [ ] **Step 4: Implement document validation**

Run: `go get golang.org/x/net@v0.57.0 && go mod tidy`

`markdown.go` rejects non-UTF-8 input and otherwise preserves source bytes.

`html.go` tokenizes with `golang.org/x/net/html` and requires doctype, html, head, and body tokens. Reject `script[src]` and `link[rel=stylesheet]` using case-insensitive attribute matching. Return `common.CodeInvalidRequest` for every validation failure.

```go
func validateMarkdown(source []byte) error {
    if !utf8.Valid(source) { return common.NewError(common.CodeInvalidRequest, "markdown must be UTF-8", nil) }
    return nil
}
```

- [ ] **Step 5: Implement the service**

```go
type Service struct { store Store; clock common.Clock; ids common.IDGenerator; defaultExpiration int64 }

func NewService(store Store, clock common.Clock, ids common.IDGenerator, defaultExpiration int64) (*Service, error)
```

Creation validates source, resolves expiration, generates an ID, and retries only `errors.Is(err, common.ErrIDCollision)` at most three times; any other create error returns immediately, and three collisions become a generic internal error. Get validates the ID and defensively maps an expired store result to not found. Update validates ID and source, gets the current record, rejects expired/wrong-kind records as not found, preserves `CreatedAt`, recalculates optional expiration, and calls `Replace`. Delete gets first so wrong-kind and expired resources map to not found. Every store call receives the service's incoming context unchanged.

- [ ] **Step 6: Verify and commit**

Run: `go test ./internal/whiteboard -v`

Expected: PASS.

```bash
git add internal/whiteboard .mockery.yaml go.mod go.sum
git commit -m "feat: implement whiteboard domain service"
```

### Task 4: Implement image detection and domain service

**Files:**
- Create: `internal/image/model.go`
- Create: `internal/image/store.go`
- Create: `internal/image/format.go`
- Create: `internal/image/service.go`
- Create: `internal/image/format_test.go`
- Create: `internal/image/service_test.go`
- Generate: `internal/image/mocks/mock_store.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces: image CRUD operations and content-signature detection for plans 2 and 3.

- [ ] **Step 1: Define image contracts and generate the store mock**

```go
type Image struct {
    ID, Extension, MediaType string
    Content []byte
    CreatedAt, UpdatedAt time.Time
    ExpiresAt *time.Time
}
type Store interface {
    Create(context.Context, Image) error
    Get(context.Context, string) (Image, error)
    Replace(context.Context, Image) error
    Delete(context.Context, string) error
    Ready(context.Context) error
    Close() error
}
type Upload struct { Content []byte; ExpiresInSeconds *int64 }
type CreateInput struct { Images []Upload }
type UpdateInput struct { ID string; Content []byte; ExpiresInSeconds *int64 }
type Result struct { ID, Filename, Extension, MediaType string; CreatedAt, UpdatedAt time.Time; ExpiresAt *time.Time }
```

Add the image package to `.mockery.yaml` with `dir: "{{.InterfaceDir}}/mocks"` and `pkgname: mocks`; configure `Store` with `filename: mock_store.go` and `structname: MockStore`.

Run: `go run github.com/vektra/mockery/v3@v3.7.1`

Expected: `internal/image/mocks/mock_store.go` compiles and exposes Testify expectations.

- [ ] **Step 2: Write failing signature tests**

Use valid 1x1 fixtures for PNG, JPEG, GIF, and WebP. Assert normalized extensions/media types. Assert SVG, random bytes, and truncated signatures return `unsupported_media_type`.

- [ ] **Step 3: Run signature tests to verify failure**

Run: `go test ./internal/image -run 'TestDetectFormat' -v`

Expected: FAIL because signature detection is not implemented.

- [ ] **Step 4: Implement and verify detection**

Run: `go get golang.org/x/image@v0.44.0 && go mod tidy`

Use `http.DetectContentType` plus format-specific validation: `png.DecodeConfig`, `jpeg.DecodeConfig`, `gif.DecodeConfig`, and `golang.org/x/image/webp.DecodeConfig`. Normalize JPEG to `.jpg`.

Run: `go test ./internal/image -run 'TestDetectFormat' -v`

Expected: PASS.

- [ ] **Step 5: Write failing service tests**

Use generated `common/mocks.MockClock`, `common/mocks.MockIDGenerator`, and `image/mocks.MockStore`. Cover exact primary-operation context propagation, ordered multi-create, validate-all-before-create, compensating rollback after persistence failure, rollback after parent-context cancellation, permanent records, expired get/update as not found, format-changing update, expiration preservation, and delete. The canceled-parent rollback test asserts retained context values, a fresh five-second deadline, and a live cleanup context.

- [ ] **Step 6: Run service tests to verify failure**

Run: `go test ./internal/image -run 'TestService' -v`

Expected: FAIL because the image service is not implemented.

- [ ] **Step 7: Implement image service and rollback**

Validate all uploads into an in-memory slice before calling the store. Persist sequentially to preserve order. On failure, derive `cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)`, delete previously created IDs in reverse order, cancel the cleanup context, and return the original persistence error; log rollback failures through an injected `*slog.Logger` without full IDs. Get defensively maps expired store results to not found. Every primary store call receives the incoming context unchanged; only compensating deletion uses the bounded cleanup context.

Use the exact constructor `NewService(store Store, clock common.Clock, ids common.IDGenerator, defaultExpiration int64, logger *slog.Logger) (*Service, error)`. Image creation applies the same three-attempt `common.ErrIDCollision` retry policy per image as whiteboards; a collision does not enter the compensating rollback path until retry attempts are exhausted.

- [ ] **Step 8: Verify and commit**

Run: `go test ./internal/image -v`

Expected: PASS.

```bash
git add internal/image go.mod go.sum .mockery.yaml
git commit -m "feat: implement image domain service"
```

### Task 5: Implement filesystem creation, reads, and atomic generations

**Files:**
- Create: `internal/store/fs.go`
- Create: `internal/store/fs_test.go`

**Interfaces:**
- Consumes: `whiteboard.Store`, `image.Store`, and `common.Clock`.
- Produces: one `store.FS` lifecycle owner plus `Whiteboards()` and `Images()` views implementing the two domain stores.

- [ ] **Step 1: Define filesystem configuration and compile-time assertions**

```go
type Config struct { Root string; CleanupInterval time.Duration; Clock common.Clock; Context context.Context }
type FS struct { /* root, clock, lifecycle, keyed locks, cleanup cancellation */ }
type whiteboardView struct { fs *FS }
type imageView struct { fs *FS }
func (fs *FS) Whiteboards() whiteboard.Store
func (fs *FS) Images() image.Store
var _ whiteboard.Store = (*whiteboardView)(nil)
var _ image.Store = (*imageView)(nil)
```

Both unexported views and all of their delegated methods remain in `fs.go`. `Ready` and `Close` delegate to the same `FS`, so cleanup, active-operation tracking, and idempotent closure are shared rather than duplicated. `Context` is the required application-lifetime parent for cleanup; `NewFS` derives its owned cancelable child and never substitutes a request context.

- [ ] **Step 2: Write failing real-filesystem tests**

Use `root := t.TempDir()`. Cover directory initialization, create/get for both resource types, metadata schema 1, client filename isolation, invalid ID rejection, and symlink traversal rejection.

- [ ] **Step 3: Implement metadata and generation commits**

Use one private metadata struct containing schema, kind, timestamps as Unix seconds/nanoseconds, nullable expiration, content filename, extension, and media type. Write content to a same-directory temporary file, sync, close, rename to a random generation name, then atomically replace `metadata.json` the same way.

On create, reserve the resource directory with `os.Mkdir` rather than `MkdirAll`; an existing valid ID directory returns `common.ErrIDCollision`. Other filesystem failures map to `storage_unavailable`. If create fails before metadata commit, remove its temporary files and newly reserved directory. On replacement, check context between generation write, sync, rename, metadata write, sync, and final rename; cancellation removes the uncommitted generation and leaves the old metadata/content valid.

- [ ] **Step 4: Implement path containment and readiness**

Resolve the absolute root once. Every resource path uses validated IDs and a containment check based on `filepath.Rel`. `Ready(ctx)` creates and removes a small probe file under a reserved readiness directory while respecting context checks.

- [ ] **Step 5: Verify and commit**

Run: `go test ./internal/store -run 'TestFSCreate|TestFSGet|TestFSPathSafety' -v`

Expected: PASS.

```bash
git add internal/store
git commit -m "feat: add filesystem resource persistence"
```

### Task 6: Add replacement, deletion, granular locking, and cleanup

**Files:**
- Modify: `internal/store/fs.go`
- Modify: `internal/store/fs_test.go`

**Interfaces:**
- Completes: last-write-wins, lazy expiration, periodic cleanup, concurrency, and idempotent closure.

- [ ] **Step 1: Write failing replacement and concurrency tests**

Test that replacement preserves `CreatedAt`, swaps metadata atomically, deletes the old generation, and lets unrelated IDs update concurrently. Start 50 readers and 20 writers, assert every read is a complete old or new record, and run the suite with the race detector.

- [ ] **Step 2: Implement keyed standard-library locks**

```go
type lockEntry struct { mu sync.RWMutex; refs int }
type lockSet struct { mu sync.Mutex; entries map[string]*lockEntry }
```

Acquire increments `refs` under the map mutex before taking the entry lock. Release unlocks, decrements `refs`, and deletes the entry at zero. Read uses `RLock`; every mutation uses `Lock`. Check context before and immediately after acquisition.

An expired `Get` must not mutate while holding `RLock`: release the read lock, acquire the same resource's write lock, reload metadata, and delete only if the record is still expired.

- [ ] **Step 3: Write failing expiration and lifecycle tests**

Cover lazy deletion on get/update/delete, periodic sweep with an injected short interval, cleanup-versus-read safety, orphan-generation removal, `Close` waiting for active operations, and repeated `Close` calls.

- [ ] **Step 4: Implement cleanup and closure**

Start cleanup from `NewFS`. Each sweep enumerates IDs without holding a global resource lock, locks one candidate, reloads metadata, and deletes only after re-checking expiration. `Close` marks the store closed, cancels cleanup, waits for cleanup and active operations, then returns the stored close result on repeated calls.

- [ ] **Step 5: Run full core verification**

Run: `go test ./internal/common ./internal/whiteboard ./internal/image ./internal/store -v`

Expected: PASS.

Run: `go test -race ./internal/...`

Expected: PASS with no race reports.

- [ ] **Step 6: Commit**

```bash
git add internal/store
git commit -m "feat: complete concurrent expiring filesystem store"
```

### Task 7: Core phase acceptance checkpoint

**Files:**
- Modify only if verification reveals a core-phase defect.

**Interfaces:**
- Produces the stable domain/store boundary consumed by plans 2 and 3.

- [ ] **Step 1: Regenerate mocks and verify a clean diff**

Run: `go run github.com/vektra/mockery/v3@v3.7.1 && git diff --exit-code -- internal/common/mocks internal/whiteboard/mocks internal/image/mocks`

Expected: exit 0.

- [ ] **Step 2: Run formatting, vetting, unit tests, and race tests**

```bash
gofmt -w internal
go vet ./internal/...
go test ./internal/...
go test -race ./internal/...
```

Expected: every command exits 0.

- [ ] **Step 3: Confirm the public boundary requirements**

Run: `go list -deps ./internal/... >/dev/null`

Expected: exit 0 with no import cycle. Confirm whiteboard does not import image or `internal/store`, image does not import whiteboard or `internal/store`, and the two views in `internal/store/fs.go` implement their respective domain interfaces over one shared `FS`.

- [ ] **Step 4: Commit any verification-only corrections**

```bash
git add -A
git commit -m "test: verify core domain and storage contracts"
```

Skip this commit only when the worktree is already clean.
