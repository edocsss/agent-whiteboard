# agent-whiteboard Public Facade, Server Lifecycle, and CLI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Assemble the domain and HTTP layers into a reusable public Go package and deliver the production `agent-whiteboard` binary with deterministic configuration, graceful lifecycle, HTTP client operations, and stable human/JSON output.

**Architecture:** `pkg/agentwb` is the only public application facade and owns production defaults. `internal/app` mounts already-injected domain handlers and coordinates readiness and `http.Server`. `internal/http/client.go` is the private outbound API client used by the CLI. `internal/cli` handles configuration, files, signals, and presentation; `cmd/agent-whiteboard/main.go` only invokes it.

**Tech Stack:** Go 1.25+, standard `net/http`, `log/slog`, Cobra v1.10.2, Mockery v3.7.1, Testify v1.11.1

## Global Constraints

- Complete the core/storage and HTTP/viewer plans first and continue on the same feature branch.
- Use constructor dependency injection at every boundary; no global application, logger, client, or configuration.
- Public and internal request APIs accept `context.Context` first and propagate it unchanged.
- Standard mutexes remain non-context-aware; do not introduce a custom semaphore or locking library.
- CLI precedence is flags, then `AGENT_WHITEBOARD_*` environment variables, then defaults.
- There is no config file, named profile, browser opening, public HTTP client, or server-generated absolute URL.
- Human output completes returned paths against `--server`; JSON output is schema version 1 and stdout-clean.
- `cmd/agent-whiteboard/main.go` contains no business or parsing logic.
- Graceful shutdown uses a fresh bounded context, not the canceled signal context.

---

### Task 1: Implement the application router and readiness composition

**Files:**
- Create: `internal/app/app.go`
- Create: `internal/app/app_test.go`
- Modify: `.mockery.yaml`
- Generate: `internal/app/mocks/mock_readiness.go`

**Interfaces:**
- Consumes already-constructed whiteboard/image handlers and injected readiness dependencies.
- Produces the root `http.Handler` and readiness state used by server lifecycle.

- [ ] **Step 1: Define the composition contracts**

```go
type Readiness interface { Ready(context.Context) error }

type Config struct {
    Whiteboards *whiteboard.Handler
    Images      *image.Handler
    Readiness   []Readiness
}

type App struct {
    handler http.Handler
    readiness *readiness
}

func New(Config) (*App, error)
func (a *App) Handler() http.Handler
func (a *App) SetReady(bool)
```

The private readiness value implements `internal/http.Readiness`; it first checks an atomic accepting flag and then calls each injected dependency with the exact incoming context.

- [ ] **Step 2: Generate a readiness mock and write failing tests**

Add the app package and `Readiness` to `.mockery.yaml` with `dir: "{{.InterfaceDir}}/mocks"`, `pkgname: mocks`, `filename: mock_readiness.go`, and `structname: MockReadiness`; run `go run github.com/vektra/mockery/v3@v3.7.1`, and test:

- whiteboard, image, health, and readiness routes coexist on one `http.ServeMux`.
- `/healthz` stays 200 when readiness is false.
- `/readyz` is 503 before startup, 200 after `SetReady(true)`, and 503 immediately after `SetReady(false)`.
- the same context sentinel reaches both readiness dependencies in order.
- one dependency failure short-circuits later readiness calls and returns no internal details.
- nil handlers or readiness entries fail construction.

- [ ] **Step 3: Run tests to verify failure**

Run: `go test ./internal/app -run App -v`

Expected: FAIL because `App` is not implemented.

- [ ] **Step 4: Implement router composition**

Construct one `http.ServeMux`, let each domain handler register itself, register health through `httpx.RegisterHealth`, and expose the mux only as `http.Handler`. Use `atomic.Bool` for the accepting flag; do not make service/stores global.

- [ ] **Step 5: Verify and commit**

Run: `go test ./internal/app -run App -v`

Expected: PASS.

```bash
git add internal/app .mockery.yaml
git commit -m "feat: compose application HTTP routes"
```

### Task 2: Add graceful server lifecycle and structured logging

**Files:**
- Create: `internal/app/server.go`
- Create: `internal/app/server_test.go`

**Interfaces:**
- Produces injected-listener serving, graceful shutdown, forced close fallback, and idempotent resource closure.

- [ ] **Step 1: Define lifecycle configuration**

```go
type ServerConfig struct {
    App             *App
    Logger          *slog.Logger
    Host            string
    Port            int
    ShutdownTimeout time.Duration
    Closers         []io.Closer
    Listener        net.Listener // optional; used by embedding/tests
}

type Server struct { /* http server, listener ownership, once values, lifecycle state */ }

func NewServer(ServerConfig) (*Server, error)
func (s *Server) Handler() http.Handler
func (s *Server) ListenAndServe(context.Context) error
func (s *Server) Serve(context.Context, net.Listener) error
func (s *Server) Shutdown(context.Context) error
func (s *Server) Close() error
func (s *Server) Address() net.Addr
```

- [ ] **Step 2: Write failing lifecycle tests with real listeners**

Use `net.Listen("tcp", "127.0.0.1:0")`, real HTTP requests, and deterministic closers. Assert:

- readiness changes to true only after a listener is active.
- cancellation marks readiness false before shutdown.
- an in-flight handler completes within the configured deadline.
- an over-deadline handler is force-closed after `Shutdown` times out.
- the shutdown context is still live even though the serve context is canceled.
- `http.ErrServerClosed` is normalized to nil.
- `Close` stops storage/background closers once logically; repeated calls return the same joined error.
- `Serve` rejects simultaneous second serving attempts.
- `Address` returns the actual OS-selected address.

- [ ] **Step 3: Run tests to verify failure**

Run: `go test ./internal/app -run Server -v`

Expected: FAIL because the lifecycle is not implemented.

- [ ] **Step 4: Implement the exact shutdown sequence**

On serve-context cancellation:

1. call `App.SetReady(false)`;
2. create `context.WithTimeout(context.Background(), ShutdownTimeout)`;
3. call `http.Server.Shutdown`;
4. if the deadline expires, call `http.Server.Close`;
5. call `Close` to stop injected store/background closers;
6. wait for the serve goroutine and join non-benign errors.

On normal start, log `server listening` with `address` and `url` fields after the listener exists and before setting readiness true. Never log request bodies or full resource IDs. Use only standard-library synchronization.

`ListenAndServe` uses `ServerConfig.Listener` when injected; otherwise it creates `net.Listen("tcp", net.JoinHostPort(Host, strconv.Itoa(Port)))`. `Serve` always uses its explicit non-nil listener and rejects a call when an injected/configured listener has already been consumed. `Shutdown` always marks readiness false before calling the standard server shutdown method but does not close stores; context-driven `ListenAndServe`/`Serve` calls `Close` after HTTP shutdown, while embedders calling `Shutdown` directly retain explicit control of `Close`.

- [ ] **Step 5: Verify cancellation and race behavior**

Run:

```bash
go test ./internal/app -run Server -count=20
go test -race ./internal/app
```

Expected: PASS with no hangs or race reports.

- [ ] **Step 6: Commit**

```bash
git add internal/app
git commit -m "feat: add graceful application lifecycle"
```

### Task 3: Build the public `pkg/agentwb` facade

**Files:**
- Create: `pkg/agentwb/types.go`
- Create: `pkg/agentwb/errors.go`
- Create: `pkg/agentwb/storage.go`
- Create: `pkg/agentwb/config.go`
- Create: `pkg/agentwb/service.go`
- Create: `pkg/agentwb/agentwb.go`
- Create: `pkg/agentwb/agentwb_test.go`
- Create: `pkg/agentwb/external_test.go`

**Interfaces:**
- Produces the only supported public Go import: models, errors, store contracts, service methods, handler, and lifecycle.

- [ ] **Step 1: Write compile-failing external API tests**

Use package `agentwb_test` and compile-time assertions for a custom external store implemented only with exported names:

```go
type memoryWhiteboards struct{}
func (*memoryWhiteboards) Create(context.Context, agentwb.Whiteboard) error { return nil }
// implement Get, Replace, Delete, Ready, Close
var _ agentwb.WhiteboardStore = (*memoryWhiteboards)(nil)
```

Assert external code can construct `agentwb.Config`, call `agentwb.New`, call all service methods, obtain `Handler`, and invoke lifecycle methods without importing any `internal` package.

- [ ] **Step 2: Define exact public aliases and interfaces**

Expose aliases for domain value types and errors:

```go
type Whiteboard = whiteboard.Whiteboard
type WhiteboardKind = whiteboard.Kind
type Image = image.Image
type Error = common.Error
type ErrorCode = common.ErrorCode
type WhiteboardStore = whiteboard.Store
type ImageStore = image.Store
```

Re-export named kind/error constants. Define public input/result aliases where their semantics exactly match; otherwise define explicit structs and convert once at the facade edge. Do not duplicate validation or business logic.

- [ ] **Step 3: Define configuration and deterministic defaults**

```go
type Config struct {
    WhiteboardStore WhiteboardStore
    ImageStore ImageStore
    RootDir string
    DefaultExpirationSeconds int64
    CleanupInterval time.Duration
    Host string
    Port int
    ShutdownTimeout time.Duration
    MaxWhiteboardBytes int64
    MaxImageBytes int64
    MaxImageRequestBytes int64
    LogMode LogMode
    Logger *slog.Logger
}
```

Use these exact defaults when zero-valued fields are omitted: `~/.agent-whiteboard`, 86400 seconds, 15 minutes, `127.0.0.1`, 8567, 10 seconds, 10 MiB, 25 MiB, 100 MiB, and console logging. Because `0` is a meaningful permanent expiration, represent default-expiration override internally with a constructor option:

```go
func WithDefaultExpiration(seconds int64) Option
```

`Config.DefaultExpirationSeconds == 0` means use 86400; callers use `WithDefaultExpiration(0)` to choose permanent-by-default. `Config.Port == 0` means use 8567; callers and the CLI use `WithPort(0)` to request an OS-assigned port. Provide `WithPort`, `WithDefaultExpiration`, `WithClock`, `WithIDGenerator`, `WithListener`, and `WithViewerAssets(css, js []byte)` for explicit zero values, hermetic tests, and embedding. Validate negative sizes, ports outside 0..65535, nil injected values, invalid log modes, and non-positive shutdown/cleanup durations.

- [ ] **Step 4: Write failing forwarding and composition tests**

With fake clock/ID and `t.TempDir`, assert:

- defaults construct real filesystem storage and real handlers.
- custom stores are injected into the correct domain only.
- service methods forward the exact context and values.
- `Handler` serves health and both domain routes.
- public `ListenAndServe`, `Serve`, `Shutdown`, and `Close` delegate to one server lifecycle.
- repeated `Close` is safe when two custom domain views delegate to the same backend lifecycle owner.
- logger selection uses `slog.TextHandler` for console and `slog.JSONHandler` for JSON when no logger is supplied.

- [ ] **Step 5: Implement the facade composition root**

`New(Config, ...Option)` performs this sequence:

1. resolve and validate configuration;
2. create a dedicated application-lifetime context and one filesystem lifecycle owner for whichever domain stores were omitted, then inject its `Whiteboards()` and/or `Images()` view;
3. create common clock/ID defaults;
4. construct whiteboard and image services;
5. copy embedded or injected viewer asset bytes into `whiteboard.Viewer`;
6. construct domain handlers with limits;
7. construct `app.App` with readiness dependencies;
8. construct `app.Server` with idempotent closers.

The exported `Service` owns these components and provides the framework-independent methods listed in the master design plus lifecycle methods. Never import `internal/http/client.go` into the public facade.

- [ ] **Step 6: Verify the external boundary and commit**

Run:

```bash
go test ./pkg/agentwb -v
go vet ./pkg/agentwb
go list -deps ./pkg/agentwb >/dev/null
```

Expected: PASS, with no import cycles or public test imports of `internal`.

```bash
git add pkg/agentwb
git commit -m "feat: expose public agentwb facade"
```

### Task 4: Implement the private HTTP client used by the CLI

**Files:**
- Create: `internal/http/client.go`
- Create: `internal/http/client_test.go`

**Interfaces:**
- Produces an internal `Client` with context-aware multipart mutation methods and typed protocol results.

- [ ] **Step 1: Define the client API**

```go
type ClientConfig struct { Server string; HTTPClient *http.Client }
type File struct { Name string; Reader io.Reader }
type Client struct { /* validated base URL and injected transport */ }

func NewClient(ClientConfig) (*Client, error)
func (c *Client) CreateWhiteboard(context.Context, whiteboard.Kind, File, *int64) (Resource, error)
func (c *Client) UpdateWhiteboard(context.Context, whiteboard.Kind, string, File, *int64) (Resource, error)
func (c *Client) DeleteWhiteboard(context.Context, whiteboard.Kind, string) error
func (c *Client) CreateImages(context.Context, []File, *int64) ([]Resource, error)
func (c *Client) UpdateImage(context.Context, string, File, *int64) (Resource, error)
func (c *Client) DeleteImage(context.Context, string) error
func (c *Client) PublicURL(path string) (string, error)
```

- [ ] **Step 2: Write failing `httptest.Server` client tests**

Use a real HTTP test server and assert:

- the constructor accepts only absolute `http`/`https` origins with no query, fragment, userinfo, or non-root path.
- multipart names and ordered repeated `images` fields are exact.
- omitted expiration is absent; zero and positive expiration are serialized exactly.
- requests use `http.NewRequestWithContext` and cancellation reaches the transport.
- stable JSON errors become `*common.Error` without exposing unknown response bodies.
- malformed/oversized response JSON is rejected using a fixed 1 MiB response limit.
- `PublicURL` joins only server-returned absolute paths and rejects another origin or traversal.

- [ ] **Step 3: Run tests to verify failure**

Run: `go test ./internal/http -run Client -v`

Expected: FAIL because the client is not implemented.

- [ ] **Step 4: Implement streaming multipart requests**

Use `io.Pipe` plus `multipart.Writer` so files are not duplicated in memory. A writer goroutine closes the pipe with its own error. Build every request with `http.NewRequestWithContext`. Close response bodies on all paths and cap JSON reads with `io.LimitReader` plus overflow detection.

Treat context deadline/cancellation errors as their original context errors so the CLI can produce a stable timeout message. Do not retry mutations.

- [ ] **Step 5: Verify and commit**

Run: `go test ./internal/http -run Client -v`

Expected: PASS.

```bash
git add internal/http/client.go internal/http/client_test.go
git commit -m "feat: add private API client"
```

### Task 5: Implement CLI configuration, output, and command tree

**Files:**
- Create: `internal/cli/cli.go`
- Create: `internal/cli/output.go`
- Create: `internal/cli/whiteboard.go`
- Create: `internal/cli/image.go`
- Create: `internal/cli/cli_test.go`
- Create: `internal/cli/output_test.go`
- Create: `internal/cli/mocks/mock_client.go`
- Modify: `.mockery.yaml`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces a Cobra root command and all non-server client commands using an injected client interface.

- [ ] **Step 1: Add Cobra and define the testable CLI boundary**

Run `go get github.com/spf13/cobra@v1.10.2 && go mod tidy` to add Cobra at the pinned version.

```go
type Client interface {
    CreateWhiteboard(context.Context, whiteboard.Kind, httpx.File, *int64) (httpx.Resource, error)
    UpdateWhiteboard(context.Context, whiteboard.Kind, string, httpx.File, *int64) (httpx.Resource, error)
    DeleteWhiteboard(context.Context, whiteboard.Kind, string) error
    CreateImages(context.Context, []httpx.File, *int64) ([]httpx.Resource, error)
    UpdateImage(context.Context, string, httpx.File, *int64) (httpx.Resource, error)
    DeleteImage(context.Context, string) error
    PublicURL(string) (string, error)
}

type Dependencies struct {
    Stdout, Stderr io.Writer
    Getenv func(string) string
    NewClient func(httpx.ClientConfig) (Client, error)
    NewApplication func(agentwb.Config, ...agentwb.Option) (Application, error)
}
type Application interface {
    ListenAndServe(context.Context) error
    Close() error
}
func NewRoot(Dependencies) (*cobra.Command, error)
```

Add the CLI package and `Client` to `.mockery.yaml` with `dir: "{{.InterfaceDir}}/mocks"`, `pkgname: mocks`, `filename: mock_client.go`, and `structname: MockClient`; then run the pinned generator.

- [ ] **Step 2: Write failing precedence and validation tests**

For every client setting (`server`, `timeout`) and server setting (`host`, `port`, `storage`, `cleanup-interval`, `default-expires-in`, `shutdown-timeout`, `log-mode`, and three byte limits), assert flag > env > default. Use the exact env names:

```text
AGENT_WHITEBOARD_SERVER
AGENT_WHITEBOARD_TIMEOUT
AGENT_WHITEBOARD_HOST
AGENT_WHITEBOARD_PORT
AGENT_WHITEBOARD_STORAGE
AGENT_WHITEBOARD_CLEANUP_INTERVAL
AGENT_WHITEBOARD_DEFAULT_EXPIRES_IN
AGENT_WHITEBOARD_SHUTDOWN_TIMEOUT
AGENT_WHITEBOARD_LOG_MODE
AGENT_WHITEBOARD_MAX_WHITEBOARD_BYTES
AGENT_WHITEBOARD_MAX_IMAGE_BYTES
AGENT_WHITEBOARD_MAX_IMAGE_REQUEST_BYTES
```

Assert invalid URLs, durations, integers, negative expiration, non-existent files, directories supplied as files, zero files, and unexpected positional arguments fail before any client call.

- [ ] **Step 3: Write failing command and context tests**

Using generated mocks and temporary fixture files, cover every approved command. Assert exact kind/ID/content, ordered image files, optional expiration, and deletion calls. Set a 5 ms timeout and make the mock block on `<-ctx.Done()`; assert the command returns a stable timeout error and the client sees a deadline-bearing context derived from `cmd.Context()`.

- [ ] **Step 4: Write failing output-contract tests**

Human success output is one public URL per line. JSON success is exactly:

```json
{"schema_version":1,"resource":{"id":"id","url":"https://example.test/images/id","expires_at":null,"permanent":true}}
```

Multi-image output uses ordered `resources`. JSON errors contain `schema_version`, stable `code`, and `message`. Assert JSON mode writes no diagnostics to stdout, all errors go to stderr, and ordinary human expiration timestamps are readable while values outside RFC 3339 range fall back to Unix seconds.

- [ ] **Step 5: Run tests to verify failure**

Run: `go test ./internal/cli -v`

Expected: FAIL because commands and output are not implemented.

- [ ] **Step 6: Implement the command tree and output**

Create exactly these commands:

```text
serve
create markdown <file>
create html <file>
update markdown <id> <file>
update html <id> <file>
delete markdown <id>
delete html <id>
image upload <files...>
image update <id> <file>
image delete <id>
```

Use `RunE`, `cmd.Context()`, and fresh file handles closed by each command. Set `SilenceUsage` and `SilenceErrors`. Create a new `http.Client{Timeout: resolvedTimeout}` per invocation. Join returned stable paths through the private client's `PublicURL`; never open a browser.

- [ ] **Step 7: Verify and commit**

Run:

```bash
go test ./internal/cli -v
go test -race ./internal/cli
```

Expected: PASS.

```bash
git add go.mod go.sum .mockery.yaml internal/cli
git commit -m "feat: add client CLI commands"
```

### Task 6: Implement `serve` and the minimal binary entry point

**Files:**
- Create: `internal/cli/serve.go`
- Create: `internal/cli/serve_test.go`
- Create: `internal/cli/main.go`
- Create: `internal/cli/main_test.go`
- Create: `cmd/agent-whiteboard/main.go`

**Interfaces:**
- Completes process startup, signal cancellation, and graceful server execution.

- [ ] **Step 1: Write failing `serve` tests**

Inject `NewApplication` and a fake application lifecycle. Assert resolved flags/env/defaults become the exact `agentwb.Config` and `WithDefaultExpiration` option, the command calls `ListenAndServe(cmd.Context())`, cancellation is propagated, constructor failures are returned, and `Close` runs on every post-construction exit.

- [ ] **Step 2: Implement `serve`**

Use `agentwb.New` through the injected factory. Pass storage root, host, cleanup interval, shutdown timeout, limits, log mode, and logger configuration. Always pass `WithPort(resolvedPort)` so explicit `--port 0` selects an OS-assigned port, and use `WithDefaultExpiration` so an explicit `0` remains permanent-by-default rather than being mistaken for an omitted field.

- [ ] **Step 3: Implement the process edge inside the CLI package**

```go
func Run(ctx context.Context, stdout, stderr io.Writer, getenv func(string) string) int

func Main() int {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    code := Run(ctx, os.Stdout, os.Stderr, os.Getenv)
    stop()
    return code
}
```

`Run` creates production dependencies, constructs the CLI, and calls `ExecuteContext(ctx)`. It uses exit code 0 for success, 1 for an unexpected internal failure, 2 for CLI usage/configuration errors, 3 for a stable remote API/domain error, and 4 for deadline/cancellation. Graceful signal-driven server shutdown returns 0. It does not duplicate command logic.

- [ ] **Step 4: Write the minimal binary entry point**

`cmd/agent-whiteboard/main.go` contains only imports and:

```go
func main() { os.Exit(cli.Main()) }
```

It owns no flags, signals, configuration, dependencies, or business behavior.

- [ ] **Step 5: Test process-edge behavior without subprocesses**

In `internal/cli/main_test.go`, assert `Run` prints help successfully, writes command errors only to stderr, emits no extra text in JSON mode, and maps `context.DeadlineExceeded` to the documented timeout diagnostic. Signal subprocess behavior belongs in plan 4; there is no binary-package logic to unit test.

- [ ] **Step 6: Run full phase verification**

```bash
gofmt -w cmd internal/app internal/http internal/cli pkg/agentwb
go vet ./...
go test ./...
go test -race ./...
go build ./cmd/agent-whiteboard
```

Expected: every command exits 0 and the binary builds without network access.

- [ ] **Step 7: Commit**

```bash
git add cmd internal/cli pkg/agentwb internal/app internal/http go.mod go.sum .mockery.yaml
git commit -m "feat: deliver agent-whiteboard server CLI"
```

### Task 7: Facade and CLI acceptance checkpoint

**Files:**
- Modify only if verification reveals a phase defect.

**Interfaces:**
- Freezes the public Go, lifecycle, CLI, and machine-output contracts before full integration work.

- [ ] **Step 1: Regenerate mocks and assets**

```bash
go run github.com/vektra/mockery/v3@v3.7.1
corepack pnpm run check:assets
git diff --exit-code -- internal/common/mocks internal/app/mocks internal/cli/mocks internal/whiteboard/mocks internal/image/mocks internal/assets/dist internal/assets/manifest.json
```

Expected: no generation drift.

- [ ] **Step 2: Inspect public documentation surface**

Run: `go doc github.com/edocsss/agent-whiteboard/pkg/agentwb`

Expected: only intended aliases, configuration, options, service operations, handler, and lifecycle are public. No public API mentions the private HTTP client or requires a direct `internal` import.

- [ ] **Step 3: Run all verification**

```bash
go test ./...
go test -race ./...
go vet ./...
go build -trimpath -o /tmp/agent-whiteboard-plan-check ./cmd/agent-whiteboard
rm -f /tmp/agent-whiteboard-plan-check
```

Expected: PASS and the temporary binary is removed.

- [ ] **Step 4: Commit corrections if needed**

```bash
git add -A
git commit -m "test: verify public facade and CLI contracts"
```

Skip this commit only when the worktree is already clean.
