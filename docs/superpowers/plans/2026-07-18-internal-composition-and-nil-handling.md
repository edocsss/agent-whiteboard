# Internal Composition and Nil Handling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `internal/app` the shared composition root, remove `internal/cli`'s dependency on `pkg/agentwb`, centralize typed-nil detection, and treat nil contexts consistently as programmer errors.

**Architecture:** `internal/app` gains the service configuration, options, production defaults, composition, and service facade currently implemented in `pkg/agentwb`. `pkg/agentwb` retains its supported API through aliases and thin constructor/option forwarders, while `internal/cli` uses `internal/app` directly. `internal/common.IsNil` becomes the only reflection-based typed-nil helper.

**Tech Stack:** Go 1.25, standard library (`context`, `go/ast`, `go/parser`, `reflect`), Cobra v1.10.2, Testify v1.11.1, pnpm/Playwright asset and browser checks.

## Global Constraints

- Preserve the existing public `pkg/agentwb` source API and behavior.
- Preserve CLI flags, environment variables, output, exit codes, HTTP routes, payloads, storage formats, and lifecycle semantics.
- Preserve every valid context unchanged and preserve cancellation/deadline propagation.
- Nil contexts are programmer errors; do not add a replacement stable error or explicit panic contract.
- Keep domain behavior in its owning package and concrete dependency wiring in `internal/app`.
- Add or update tests at every level that can meaningfully detect a regression.
- Keep documentation synchronized with the new ownership boundary.
- Use test-first red/green/refactor cycles and commit each independently reviewable task.

---

### Task 1: Add the shared typed-nil primitive and remove duplicate helpers

**Files:**
- Create: `internal/common/nil.go`
- Create: `internal/common/nil_test.go`
- Modify: `internal/app/app.go`
- Modify: `internal/app/server.go`
- Modify: `internal/cli/cli.go`
- Modify: `internal/cli/output.go`
- Modify: `internal/cli/serve.go`
- Modify: `internal/http/client.go`
- Modify: `internal/http/http.go`
- Modify: `internal/image/handler.go`
- Modify: `internal/image/service.go`
- Modify: `internal/store/fs.go`
- Modify: `internal/whiteboard/handler.go`
- Modify: `internal/whiteboard/service.go`
- Modify: `pkg/agentwb/config.go`

**Interfaces:**
- Consumes: Go reflection only.
- Produces: `func common.IsNil(value any) bool`, the repository's sole typed-nil detector.

- [ ] **Step 1: Write the failing table test**

Create `internal/common/nil_test.go`:

```go
package common

import (
    "io"
    "testing"

    "github.com/stretchr/testify/require"
)

func TestIsNilRecognizesNilableValuesWithoutPanicking(t *testing.T) {
    var pointer *int
    var channel chan int
    var function func()
    var interfaceValue io.Reader = (*nilReader)(nil)
    var mapping map[string]int
    var slice []int

    tests := []struct {
        name  string
        value any
        want  bool
    }{
        {name: "nil interface", value: nil, want: true},
        {name: "typed nil pointer", value: pointer, want: true},
        {name: "typed nil channel", value: channel, want: true},
        {name: "typed nil function", value: function, want: true},
        {name: "typed nil interface implementation", value: interfaceValue, want: true},
        {name: "typed nil map", value: mapping, want: true},
        {name: "typed nil slice", value: slice, want: true},
        {name: "non-nil pointer", value: new(int)},
        {name: "non-nil channel", value: make(chan int)},
        {name: "non-nil function", value: func() {}},
        {name: "non-nil map", value: map[string]int{}},
        {name: "non-nil slice", value: []int{}},
        {name: "non-nilable integer", value: 0},
        {name: "non-nilable struct", value: struct{}{}},
    }
    for _, test := range tests {
        t.Run(test.name, func(t *testing.T) {
            require.Equal(t, test.want, IsNil(test.value))
        })
    }
}

type nilReader struct{}

func (*nilReader) Read([]byte) (int, error) { return 0, io.EOF }
```

- [ ] **Step 2: Run the focused test and verify red**

Run: `go test ./internal/common -run TestIsNilRecognizesNilableValuesWithoutPanicking -v`

Expected: FAIL to compile with `undefined: IsNil`.

- [ ] **Step 3: Implement `common.IsNil`**

Create `internal/common/nil.go`:

```go
package common

import "reflect"

// IsNil reports whether value is nil, including a typed nil held by an interface.
func IsNil(value any) bool {
    if value == nil {
        return true
    }
    reflected := reflect.ValueOf(value)
    switch reflected.Kind() {
    case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
        return reflected.IsNil()
    default:
        return false
    }
}
```

- [ ] **Step 4: Replace every package-local detector**

Replace calls as follows, retaining existing surrounding validation and error text:

```text
internal/app:        isNilReadiness, isNilInterface -> common.IsNil
internal/cli:        isNilLike                    -> common.IsNil
internal/http:       isNilReader, isNilReadiness  -> common.IsNil
internal/image:      isNilDependency              -> common.IsNil
internal/store:      isNil                        -> common.IsNil
internal/whiteboard: isNilDependency              -> common.IsNil
pkg/agentwb:         isNilValue                   -> common.IsNil
```

Delete each local helper and remove its now-unused `reflect` import. Add the `internal/common` import wherever it is not already present. Do not change checks such as `config.WhiteboardStore != nil`, because they distinguish an omitted optional store from a typed-nil store.

- [ ] **Step 5: Verify the helper and all typed-nil call sites**

Run:

```bash
gofmt -w internal/common/nil.go internal/common/nil_test.go internal/app internal/cli internal/http internal/image internal/store internal/whiteboard pkg/agentwb
go test ./internal/common ./internal/app ./internal/cli ./internal/http ./internal/image ./internal/store ./internal/whiteboard ./pkg/agentwb
rg -n 'func (isNil|isNilLike|isNilInterface|isNilReader|isNilReadiness|isNilDependency|isNilValue)' internal pkg
rg -n 'reflect\.ValueOf' internal pkg
```

Expected: all tests PASS; the first `rg` returns no matches; the second returns only `internal/common/nil.go`.

- [ ] **Step 6: Commit**

```bash
git add internal/common internal/app internal/cli internal/http internal/image internal/store internal/whiteboard pkg/agentwb
git commit -m "refactor: centralize typed nil detection"
```

---

### Task 2: Remove explicit nil-context handling

**Files:**
- Create: `tests/integration/architecture_test.go`
- Modify: `internal/app/server.go`
- Modify: `internal/http/client.go`
- Modify: `internal/store/fs.go`
- Modify: affected tests only if they currently assert a stable nil-context error

**Interfaces:**
- Consumes: existing APIs that accept `context.Context`.
- Produces: a repository-wide contract that callers supply non-nil contexts; valid-context behavior is unchanged.

- [ ] **Step 1: Add a failing architecture regression test**

Create `tests/integration/architecture_test.go` with a source scan limited to production Go files:

```go
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
```

- [ ] **Step 2: Run the architecture test and verify red**

Run: `go test ./tests/integration -run TestProductionCodeDoesNotGuardNilContexts -v`

Expected: FAIL and identify `internal/app/server.go`, `internal/http/client.go`, or `internal/store/fs.go`.

- [ ] **Step 3: Remove all explicit nil-context branches**

Make these exact behavioral simplifications:

```go
// internal/app/server.go
// Delete the opening common.IsNil(ctx) error branches from ListenAndServe,
// Serve, and Shutdown. Keep all reserve/listener/shutdown logic unchanged.

// internal/store/fs.go NewFS
if err := config.Context.Err(); err != nil {
    return nil, err
}

// internal/store/fs.go beginOperation
func (fs *FS) beginOperation(ctx context.Context) error {
    if err := ctx.Err(); err != nil {
        return err
    }
    // retain the existing lifecycle locking and subsequent ctx.Err checks
}

// internal/http/client.go
func contextError(ctx context.Context, err error) error {
    if err == nil {
        return nil
    }
    if contextErr := ctx.Err(); contextErr != nil {
        return contextErr
    }
    // retain errors.Is normalization below
}
```

Remove imports that become unused. Do not add `context.Background()` fallbacks.

- [ ] **Step 4: Verify valid context behavior and the architecture rule**

Run:

```bash
gofmt -w internal/app/server.go internal/http/client.go internal/store/fs.go tests/integration/architecture_test.go
go test ./internal/app ./internal/http ./internal/store
go test ./tests/integration -run TestProductionCodeDoesNotGuardNilContexts -v
```

Expected: all tests PASS. Existing cancellation, deadline, exact-context, and shutdown tests remain green.

- [ ] **Step 5: Commit**

```bash
git add internal/app/server.go internal/http/client.go internal/store/fs.go tests/integration/architecture_test.go
git commit -m "refactor: require non-nil contexts"
```

---

### Task 3: Move configuration and service composition into `internal/app`

**Files:**
- Create: `internal/app/service_config.go`
- Create: `internal/app/service_config_test.go`
- Create: `internal/app/service.go`
- Modify: `pkg/agentwb/agentwb_test.go`
- Reference while moving: `pkg/agentwb/config.go`
- Reference while moving: `pkg/agentwb/agentwb.go`
- Reference while moving: `pkg/agentwb/service.go`

**Interfaces:**
- Consumes: `internal/app.New`, `internal/app.NewServer`, domain services/handlers/stores, embedded viewer assets, and `common.IsNil`.
- Produces: `app.ServiceConfig`, `app.Option`, `app.LogMode`, option constructors, `app.NewService(ServiceConfig, ...Option) (*Service, error)`, and `app.Service` methods matching the current public service.

- [ ] **Step 1: Write compile-failing internal composition tests**

Create `internal/app/service_config_test.go` in package `app`. Move and adapt `TestResolveConfigUsesExactDefaults`, `TestResolveConfigHonorsExplicitZerosAndJSONLogging`, and `TestNewResolvesHomeOnlyWhenFilesystemStorageIsNeeded` from `pkg/agentwb/agentwb_test.go`. Use these exact internal names:

```go
func TestResolveServiceConfigUsesExactDefaults(t *testing.T) {
    resolved, err := resolveServiceConfig(ServiceConfig{}, nil)
    require.NoError(t, err)
    home, err := os.UserHomeDir()
    require.NoError(t, err)
    require.Equal(t, filepath.Join(home, ".agent-whiteboard"), resolved.rootDir)
    require.Equal(t, int64(86400), resolved.defaultExpiration)
    require.Equal(t, 15*time.Minute, resolved.cleanupInterval)
    require.Equal(t, "127.0.0.1", resolved.host)
    require.Equal(t, 8567, resolved.port)
    require.Equal(t, 10*time.Second, resolved.shutdownTimeout)
    require.Equal(t, int64(10<<20), resolved.maxWhiteboardBytes)
    require.Equal(t, int64(25<<20), resolved.maxImageBytes)
    require.Equal(t, int64(100<<20), resolved.maxImageRequestBytes)
    require.Equal(t, LogModeConsole, resolved.logMode)
    require.IsType(t, &slog.TextHandler{}, resolved.logger.Handler())
}

func TestResolveServiceConfigHonorsExplicitZerosAndJSONLogging(t *testing.T) {
    resolved, err := resolveServiceConfig(ServiceConfig{LogMode: LogModeJSON}, []Option{
        WithPort(0), WithDefaultExpiration(0),
    })
    require.NoError(t, err)
    require.Zero(t, resolved.port)
    require.Zero(t, resolved.defaultExpiration)
    require.IsType(t, &slog.JSONHandler{}, resolved.logger.Handler())
}
```

For the home-resolution test, define minimal `recordingWhiteboardStore` and `recordingImageStore` fakes using `whiteboard.Whiteboard` and `image.Image`, override the package variable `userHomeDir`, call `NewService`, and retain the current assertions that home lookup is skipped when both stores are supplied and required when either built-in store is needed.

- [ ] **Step 2: Run internal app tests and verify red**

Run: `go test ./internal/app -run 'TestResolveServiceConfig|TestNewServiceResolvesHome' -v`

Expected: FAIL to compile because `ServiceConfig`, `resolveServiceConfig`, and `NewService` do not exist.

- [ ] **Step 3: Move configuration ownership into `internal/app/service_config.go`**

Move the current contents of `pkg/agentwb/config.go` into package `app` with these exact public declarations:

```go
type LogMode string

const (
    LogModeConsole LogMode = "console"
    LogModeJSON    LogMode = "json"
)

type ServiceConfig struct {
    WhiteboardStore whiteboard.Store
    ImageStore      image.Store
    RootDir                  string
    DefaultExpirationSeconds int64
    CleanupInterval          time.Duration
    Host            string
    Port            int
    ShutdownTimeout time.Duration
    MaxWhiteboardBytes   int64
    MaxImageBytes        int64
    MaxImageRequestBytes int64
    LogMode LogMode
    Logger  *slog.Logger
}

type Option func(*optionValues) error

func WithPort(port int) Option
func WithDefaultExpiration(seconds int64) Option
func WithClock(clock common.Clock) Option
func WithIDGenerator(ids common.IDGenerator) Option
func WithListener(listener net.Listener) Option
func WithViewerAssets(css, js []byte) Option
```

Rename private symbols while preserving their bodies and validation order:

```text
resolvedConfig        -> resolvedServiceConfig
resolveConfig         -> resolveServiceConfig
validateResolvedConfig -> validateResolvedServiceConfig
invalidFacadeConfig   -> invalidServiceConfig
```

Use `whiteboard.Store`, `image.Store`, `common.Clock`, and `common.IDGenerator` directly. Keep `userHomeDir = os.UserHomeDir`, all exact defaults, byte cloning, logger selection, listener validation, and `common.IsNil` checks unchanged.

- [ ] **Step 4: Move service composition into `internal/app/service.go`**

Combine the current `pkg/agentwb/agentwb.go` and `pkg/agentwb/service.go` implementation under package `app`:

```go
type Service struct {
    whiteboards *whiteboard.Service
    images      *image.Service
    server      *Server
}

func NewService(config ServiceConfig, options ...Option) (*Service, error) {
    resolved, err := resolveServiceConfig(config, options)
    if err != nil {
        return nil, err
    }

    whiteboardStore := resolved.whiteboardStore
    imageStore := resolved.imageStore
    closers := make([]io.Closer, 0, 2)
    var ownedFilesystem *filesystemLifecycle
    if whiteboardStore == nil || imageStore == nil {
        lifetimeCtx, cancelLifetime := context.WithCancel(context.Background())
        filesystem, fsErr := store.NewFS(store.Config{
            Root: resolved.rootDir, CleanupInterval: resolved.cleanupInterval,
            Clock: resolved.clock, Context: lifetimeCtx,
        })
        if fsErr != nil {
            cancelLifetime()
            return nil, fsErr
        }
        ownedFilesystem = &filesystemLifecycle{cancel: cancelLifetime, filesystem: filesystem}
        closers = append(closers, ownedFilesystem)
        if whiteboardStore == nil {
            whiteboardStore = filesystem.Whiteboards()
        }
        if imageStore == nil {
            imageStore = filesystem.Images()
        }
    }
    if resolved.whiteboardStore != nil {
        closers = append(closers, resolved.whiteboardStore)
    }
    if resolved.imageStore != nil {
        closers = append(closers, resolved.imageStore)
    }
    fail := func(constructionErr error) (*Service, error) {
        if ownedFilesystem == nil {
            return nil, constructionErr
        }
        return nil, errors.Join(constructionErr, ownedFilesystem.Close())
    }

    whiteboards, err := whiteboard.NewService(whiteboardStore, resolved.clock, resolved.ids, resolved.defaultExpiration)
    if err != nil {
        return fail(err)
    }
    images, err := image.NewService(imageStore, resolved.clock, resolved.ids, resolved.defaultExpiration, resolved.logger)
    if err != nil {
        return fail(err)
    }
    viewer, err := whiteboard.NewViewer(whiteboard.ViewerConfig{CSS: resolved.viewerCSS, JS: resolved.viewerJS})
    if err != nil {
        return fail(err)
    }
    whiteboardHandler, err := whiteboard.NewHandler(whiteboards, viewer, whiteboard.HandlerConfig{MaxBytes: resolved.maxWhiteboardBytes})
    if err != nil {
        return fail(err)
    }
    imageHandler, err := image.NewHandler(images, image.HandlerConfig{
        MaxImageBytes: resolved.maxImageBytes, MaxRequestBytes: resolved.maxImageRequestBytes,
    })
    if err != nil {
        return fail(err)
    }
    application, err := New(Config{
        Whiteboards: whiteboardHandler, Images: imageHandler,
        Readiness: []Readiness{whiteboardStore, imageStore},
    })
    if err != nil {
        return fail(err)
    }
    server, err := NewServer(ServerConfig{
        App: application, Logger: resolved.logger, Host: resolved.host, Port: resolved.port,
        ShutdownTimeout: resolved.shutdownTimeout, Closers: closers, Listener: resolved.listener,
    })
    if err != nil {
        return fail(err)
    }
    return &Service{whiteboards: whiteboards, images: images, server: server}, nil
}

func (service *Service) CreateMarkdown(ctx context.Context, input whiteboard.CreateInput) (whiteboard.Result, error)
func (service *Service) CreateHTML(ctx context.Context, input whiteboard.CreateInput) (whiteboard.Result, error)
func (service *Service) GetWhiteboard(ctx context.Context, id string) (whiteboard.Whiteboard, error)
func (service *Service) UpdateWhiteboard(ctx context.Context, input whiteboard.UpdateInput) (whiteboard.Result, error)
func (service *Service) DeleteWhiteboard(ctx context.Context, kind whiteboard.Kind, id string) error
func (service *Service) CreateImages(ctx context.Context, input image.CreateInput) ([]image.Result, error)
func (service *Service) GetImage(ctx context.Context, id string) (image.Image, error)
func (service *Service) UpdateImage(ctx context.Context, input image.UpdateInput) (image.Result, error)
func (service *Service) DeleteImage(ctx context.Context, id string) error
func (service *Service) Handler() http.Handler
func (service *Service) ListenAndServe(ctx context.Context) error
func (service *Service) Serve(ctx context.Context, listener net.Listener) error
func (service *Service) Shutdown(ctx context.Context) error
func (service *Service) Close() error

type filesystemLifecycle struct {
    cancel     context.CancelFunc
    filesystem *store.FS
}

func (lifecycle *filesystemLifecycle) Close() error {
    lifecycle.cancel()
    return lifecycle.filesystem.Close()
}
```

Implement each `Service` forwarding method with the exact one-line delegation currently in `pkg/agentwb/service.go`; only replace package-level public aliases with their owning `whiteboard` or `image` types shown in the signatures above.

- [ ] **Step 5: Remove the three moved private tests from `pkg/agentwb/agentwb_test.go` and verify green**

Delete only the tests that require private config state or `userHomeDir`; retain public constructor validation, external API, forwarding, handler, listener, and lifecycle tests in `pkg/agentwb` for Task 4.

Run:

```bash
gofmt -w internal/app/service_config.go internal/app/service_config_test.go internal/app/service.go pkg/agentwb/agentwb_test.go
go test ./internal/app -run 'TestResolveServiceConfig|TestNewServiceResolvesHome' -v
go test ./internal/app
```

Expected: PASS. At this checkpoint, the old public implementation may still exist temporarily; Task 4 removes it after the internal implementation is proven.

- [ ] **Step 6: Commit**

```bash
git add internal/app/service_config.go internal/app/service_config_test.go internal/app/service.go pkg/agentwb/agentwb_test.go
git commit -m "refactor: move service composition into app"
```

---

### Task 4: Make `pkg/agentwb` a thin facade and switch the CLI to internal app types

**Files:**
- Create: `internal/cli/dependency_test.go`
- Modify: `internal/cli/main.go`
- Modify: `internal/cli/cli.go`
- Modify: `internal/cli/serve.go`
- Modify: `internal/cli/main_test.go`
- Modify: `internal/cli/cli_test.go`
- Modify: `internal/cli/serve_test.go`
- Modify: `pkg/agentwb/config.go`
- Modify: `pkg/agentwb/agentwb.go`
- Modify: `pkg/agentwb/service.go`
- Test: `pkg/agentwb/external_test.go`
- Test: `pkg/agentwb/agentwb_test.go`

**Interfaces:**
- Consumes: `app.ServiceConfig`, `app.Option`, `app.NewService`, and `app.Service` from Task 3.
- Produces: an internal-only CLI dependency direction and an unchanged external `agentwb` API.

- [ ] **Step 1: Add a failing CLI dependency-boundary test**

Create `internal/cli/dependency_test.go`:

```go
package cli

import (
    "go/parser"
    "go/token"
    "path/filepath"
    "strconv"
    "testing"

    "github.com/stretchr/testify/require"
)

func TestCLIDoesNotImportPublicAgentWBFacade(t *testing.T) {
    files, err := filepath.Glob("*.go")
    require.NoError(t, err)
    for _, file := range files {
        parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
        require.NoError(t, err)
        for _, imported := range parsed.Imports {
            path, err := strconv.Unquote(imported.Path.Value)
            require.NoError(t, err)
            require.NotEqual(t, "github.com/edocsss/agent-whiteboard/pkg/agentwb", path, file)
        }
    }
}
```

- [ ] **Step 2: Run the boundary test and verify red**

Run: `go test ./internal/cli -run TestCLIDoesNotImportPublicAgentWBFacade -v`

Expected: FAIL and name `main.go`, `cli.go`, `serve.go`, or their tests as importing `pkg/agentwb`.

- [ ] **Step 3: Replace the public implementation with aliases and forwarders**

Reduce `pkg/agentwb/config.go` to the stable facade declarations:

```go
package agentwb

import (
    "net"

    "github.com/edocsss/agent-whiteboard/internal/app"
)

type Config = app.ServiceConfig
type Option = app.Option
type LogMode = app.LogMode

const (
    LogModeConsole = app.LogModeConsole
    LogModeJSON    = app.LogModeJSON
)

func WithPort(port int) Option                         { return app.WithPort(port) }
func WithDefaultExpiration(seconds int64) Option      { return app.WithDefaultExpiration(seconds) }
func WithClock(clock Clock) Option                    { return app.WithClock(clock) }
func WithIDGenerator(ids IDGenerator) Option          { return app.WithIDGenerator(ids) }
func WithListener(listener net.Listener) Option       { return app.WithListener(listener) }
func WithViewerAssets(css, js []byte) Option           { return app.WithViewerAssets(css, js) }
```

Reduce `pkg/agentwb/service.go` to:

```go
package agentwb

import "github.com/edocsss/agent-whiteboard/internal/app"

type Service = app.Service
```

Reduce `pkg/agentwb/agentwb.go` to:

```go
package agentwb

import "github.com/edocsss/agent-whiteboard/internal/app"

func New(config Config, options ...Option) (*Service, error) {
    return app.NewService(config, options...)
}
```

- [ ] **Step 4: Switch production CLI types and factory to `internal/app`**

Use these declarations:

```go
// internal/cli/cli.go
type Dependencies struct {
    Stdout         io.Writer
    Stderr         io.Writer
    Getenv         func(string) string
    NewClient      func(httpx.ClientConfig) (Client, error)
    NewApplication func(app.ServiceConfig, ...app.Option) (Application, error)
}

// internal/cli/main.go
NewApplication: func(config app.ServiceConfig, options ...app.Option) (Application, error) {
    return app.NewService(config, options...)
},

// internal/cli/serve.go
type applicationArguments struct {
    config            app.ServiceConfig
    port              int
    defaultExpiration int64
}
```

In `serve.go`, use `app.LogModeJSON`, `app.LogMode(settings.logMode)`, `app.WithPort`, and `app.WithDefaultExpiration`. Remove all `pkg/agentwb` imports from production CLI files.

- [ ] **Step 5: Switch CLI tests to internal app types**

Replace factory signatures from `agentwb.Config, ...agentwb.Option` to `app.ServiceConfig, ...app.Option`, replace log mode/config/option references with `app`, and change the one public error assertion in `serve_test.go`:

```go
require.False(t, common.HasCode(err, common.CodeInvalidRequest), "error: %v", err)
```

Production CLI and its same-package tests must contain no public facade import.

- [ ] **Step 6: Verify the boundary and public compatibility**

Run:

```bash
gofmt -w internal/cli pkg/agentwb
go test ./internal/cli -run TestCLIDoesNotImportPublicAgentWBFacade -v
go test ./internal/cli ./pkg/agentwb
go test ./pkg/agentwb -run TestExternalConsumerCanUseCompleteFacade -v
go list -deps ./internal/cli | rg 'github.com/edocsss/agent-whiteboard/pkg/agentwb' && exit 1 || true
```

Expected: all tests PASS; the final dependency check prints nothing. The external consumer test proves all public names and service methods remain usable without importing `internal` packages.

- [ ] **Step 7: Commit**

```bash
git add internal/cli pkg/agentwb
git commit -m "refactor: decouple cli from public facade"
```

---

### Task 5: Synchronize architecture documentation and run full verification

**Files:**
- Modify: `docs/go-api.md`
- Modify: `docs/superpowers/specs/2026-07-16-agent-whiteboard-master-design.md`
- Modify: other current documentation only if repository search finds a conflicting ownership statement

**Interfaces:**
- Consumes: the completed internal composition and public facade boundary.
- Produces: accurate maintainer and external API documentation plus full verification evidence.

- [ ] **Step 1: Update current architecture wording**

Change the opening of `docs/go-api.md` to preserve external usage while stating the implementation boundary:

```markdown
Import `github.com/edocsss/agent-whiteboard/pkg/agentwb`. This stable public facade forwards to the internal application composition root, which assembles domain services, HTTP handlers, lifecycle, and default filesystem storage.
```

In the master design, add `nil.go` to the `internal/common` responsibility list and replace the constructor ownership sentence with:

```markdown
The internal application configuration resolver chooses production defaults only when optional overrides are omitted. The public `agentwb.New(Config, ...Option)` constructor is a thin forwarding facade over that composition root. Every API accepting `context.Context` requires a non-nil context and propagates valid contexts unchanged.
```

Do not rewrite the historical implementation plans; they document the sequence that created the earlier implementation.

- [ ] **Step 2: Check documentation and dependency claims**

Run:

```bash
rg -n 'pkg/agentwb.*composition root|owns production defaults|internal/cli.*pkg/agentwb' README.md docs skills --glob '*.md'
go list -deps ./internal/cli | rg 'github.com/edocsss/agent-whiteboard/pkg/agentwb' && exit 1 || true
git diff --check
```

Expected: no current documentation contradicts the new ownership boundary; historical plan matches may remain; the CLI dependency check is empty; `git diff --check` succeeds.

- [ ] **Step 3: Run all Go checks**

Run:

```bash
go test ./...
go test -race ./...
go vet ./...
```

Expected: all commands PASS with no race or vet diagnostics.

- [ ] **Step 4: Run JavaScript, asset, integration, and browser checks**

Run:

```bash
pnpm test
pnpm run check:assets
pnpm run test:browser
```

Expected: all commands PASS. Tests remain hermetic and use local temporary storage, ephemeral ports, and the committed browser configuration.

- [ ] **Step 5: Confirm the final diff is scoped**

Run:

```bash
git status --short
git diff --stat HEAD~4
git diff --check
```

Expected: only nil handling, context handling, internal composition, CLI/facade boundary, tests, and current architecture documentation changed; no generated assets or unrelated user files changed.

- [ ] **Step 6: Commit documentation**

```bash
git add docs/go-api.md docs/superpowers/specs/2026-07-16-agent-whiteboard-master-design.md
git commit -m "docs: clarify application composition ownership"
```

- [ ] **Step 7: Invoke verification-before-completion before reporting success**

Re-run the directly relevant commands if any code changed after Steps 3–4. Report completion only from fresh command output, including any intentionally skipped check and its reason.
