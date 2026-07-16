# agent-whiteboard HTTP, Viewer, and Browser Assets Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose the domain services through the complete `/api/v1` and public-view HTTP contract, and ship a self-contained browser Markdown renderer with sanitization, Mermaid, syntax highlighting, and themes.

**Architecture:** Shared HTTP protocol types and bounded multipart helpers live in `internal/http/http.go`; business handlers stay in `internal/whiteboard` and `internal/image`. The whiteboard viewer receives compiled asset bytes through constructor injection. Browser sources live in `internal/assets/src`, compile to committed embedded files, and never depend on a CDN at runtime.

**Tech Stack:** Go 1.25+, `net/http`, Testify v1.11.1, Mockery v3.7.1, Node.js 24.x, pnpm 11.4, esbuild 0.28.1, Vitest 4.1.10, jsdom 29.1.1, markdown-it 14.2.0, DOMPurify 3.4.12, Mermaid 11.15.0, highlight.js 11.11.1

## Global Constraints

- Complete the core/storage plan first and implement on the same feature branch.
- Keep domain handlers beside their domain services; do not centralize them in a technical handler package.
- HTTP handlers depend on narrow operation interfaces, not concrete services or stores.
- Pass `request.Context()` unchanged into every domain call.
- Enforce 10 MiB whiteboard, 25 MiB per-image, and 100 MiB multi-image defaults through injected limits.
- Return stable paths only; this layer never constructs a deployment origin.
- Public routes are accessible by capability URL but non-indexable and `no-store`.
- Markdown rendering happens only in the browser. Raw HTML in Markdown is disabled and sanitized.
- Standalone HTML bytes are served unchanged after domain validation.
- Image routes are extensionless, and updates may change media type.
- Generated bundles and generated mocks are checked in.

---

### Task 1: Define shared HTTP protocol, errors, and bounded multipart reading

**Files:**
- Create: `internal/http/http.go`
- Create: `internal/http/http_test.go`
- Modify: `.mockery.yaml`

**Interfaces:**
- Produces route constants, versioned response structs, JSON/error writers, public-response headers, and multipart readers consumed by both domain handlers.

- [ ] **Step 1: Write failing protocol and error-mapping tests**

Create table-driven tests for all six domain error codes. Assert the exact content type, status, and body, including the absence of wrapped/internal error text:

```go
func TestWriteError(t *testing.T) {
    rr := httptest.NewRecorder()
    httpx.WriteError(rr, common.NewError(common.CodeNotFound, "resource not found", errors.New("secret path")))
    require.Equal(t, http.StatusNotFound, rr.Code)
    require.JSONEq(t, `{"error":{"code":"not_found","message":"resource not found"}}`, rr.Body.String())
    require.NotContains(t, rr.Body.String(), "secret path")
}
```

Add tests that `SetPublicHeaders` writes `Cache-Control: no-store`, `X-Content-Type-Options: nosniff`, and `X-Robots-Tag: noindex, nofollow, noarchive`; the image variant also appends `noimageindex`.

- [ ] **Step 2: Write failing multipart boundary tests**

Build multipart requests in memory and assert:

- `ReadMultipart` rejects a body one byte above the request limit with `content_too_large`.
- `ReadPart` rejects a file one byte above the per-part limit.
- malformed boundaries and invalid signed `expires_in_seconds` values return `invalid_request`.
- repeated `images` parts preserve submission order.
- unknown file fields return `invalid_request` rather than being ignored.

- [ ] **Step 3: Run tests to verify failure**

Run: `go test ./internal/http -v`

Expected: FAIL because the package does not exist.

- [ ] **Step 4: Implement the exact shared contract**

Define these routes and wire types in package `http` (callers import it as `httpx`):

```go
const (
    APIWhiteboardMarkdown = "/api/v1/whiteboards/markdown"
    APIWhiteboardHTML     = "/api/v1/whiteboards/html"
    APIImages             = "/api/v1/images"
    PublicMarkdown        = "/whiteboards/markdown/"
    PublicHTML            = "/whiteboards/html/"
    PublicImages          = "/images/"
)

type ErrorBody struct { Code common.ErrorCode `json:"code"`; Message string `json:"message"` }
type ErrorResponse struct { Error ErrorBody `json:"error"` }

type Resource struct {
    ID string `json:"id"`
    Type string `json:"type,omitempty"`
    Filename string `json:"filename,omitempty"`
    Extension string `json:"extension,omitempty"`
    MediaType string `json:"media_type,omitempty"`
    Path string `json:"path"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
    ExpiresAt *int64 `json:"expires_at"`
    Permanent bool `json:"permanent"`
}
type ResourceResponse struct { Resource Resource `json:"resource"` }
type ImagesResponse struct { Images []Resource `json:"images"` }
```

Map `invalid_request`, `not_found`, `content_too_large`, `unsupported_media_type`, `storage_unavailable`, and `internal_error` to 400, 404, 413, 415, 503, and 500. Unknown errors become a generic `internal_error` response. JSON responses end with one newline.

Implement multipart parsing with `http.MaxBytesReader`, `mime/multipart.Reader.NextPart`, `io.LimitedReader`, and explicit overflow detection. Parse expiration with `strconv.ParseInt(..., 10, 64)`; return `nil` when omitted and reject duplicates.

- [ ] **Step 5: Verify and commit**

Run: `go test ./internal/http -v`

Expected: PASS.

```bash
git add internal/http .mockery.yaml
git commit -m "feat: establish shared HTTP protocol"
```

### Task 2: Implement the whiteboard API handler with injected operations

**Files:**
- Create: `internal/whiteboard/handler.go`
- Create: `internal/whiteboard/handler_test.go`
- Generate: `internal/whiteboard/mocks/mock_operations.go`
- Modify: `.mockery.yaml`

**Interfaces:**
- Consumes a handler-owned `Operations` interface and a viewer.
- Produces API mutation routes and public Markdown/HTML routes on an injected `*http.ServeMux`.

- [ ] **Step 1: Define and configure the handler boundary**

```go
type Operations interface {
    CreateMarkdown(context.Context, CreateInput) (Result, error)
    CreateHTML(context.Context, CreateInput) (Result, error)
    Get(context.Context, string) (Whiteboard, error)
    Update(context.Context, UpdateInput) (Result, error)
    Delete(context.Context, Kind, string) error
}

type HandlerConfig struct { MaxBytes int64 }
type Handler struct { operations Operations; viewer *Viewer; maxBytes int64 }
func NewHandler(operations Operations, viewer *Viewer, config HandlerConfig) (*Handler, error)
func (h *Handler) Register(*http.ServeMux)
```

Add `Operations` to the whiteboard package's `.mockery.yaml` interfaces with `filename: mock_operations.go` and `structname: MockOperations`, then run:

Run: `go run github.com/vektra/mockery/v3@v3.7.1`

- [ ] **Step 2: Write failing API handler tests**

Use the generated Testify mock and `httptest`. For Markdown and HTML separately assert:

- POST uses multipart field `file`, passes the exact request context, and returns 201 plus the stable path.
- PUT requires the URL kind to match the current resource and returns 200.
- DELETE returns 204 with an empty body.
- missing/extra files, duplicate expiration, malformed ID, oversized content, wrong method, and wrong-kind service errors map correctly.
- no body content or full capability ID is written to test logger output.

Use a context sentinel to prove identity rather than merely checking non-nil:

```go
type contextKey struct{}
ctx := context.WithValue(context.Background(), contextKey{}, "sentinel")
req = req.WithContext(ctx)
ops.EXPECT().CreateMarkdown(mock.MatchedBy(func(got context.Context) bool {
    return got.Value(contextKey{}) == "sentinel"
}), mock.Anything).Return(result, nil)
```

- [ ] **Step 3: Run tests to verify failure**

Run: `go test ./internal/whiteboard -run Handler -v`

Expected: FAIL because the handler is not implemented.

- [ ] **Step 4: Implement route registration and mutation handlers**

Register exact Go 1.22+ method patterns:

```go
mux.HandleFunc("POST "+httpx.APIWhiteboardMarkdown, h.createMarkdown)
mux.HandleFunc("PUT "+httpx.APIWhiteboardMarkdown+"/{id}", h.updateMarkdown)
mux.HandleFunc("DELETE "+httpx.APIWhiteboardMarkdown+"/{id}", h.deleteMarkdown)
mux.HandleFunc("POST "+httpx.APIWhiteboardHTML, h.createHTML)
mux.HandleFunc("PUT "+httpx.APIWhiteboardHTML+"/{id}", h.updateHTML)
mux.HandleFunc("DELETE "+httpx.APIWhiteboardHTML+"/{id}", h.deleteHTML)
```

Convert domain results to protocol resources with exact paths. Do not add a GET management route. Every service call receives `r.Context()` directly.

- [ ] **Step 5: Verify and commit the API half**

Run: `go test ./internal/whiteboard -run Handler -v`

Expected: mutation tests PASS; public viewer tests remain for Task 5.

```bash
git add internal/whiteboard .mockery.yaml
git commit -m "feat: add whiteboard HTTP mutations"
```

### Task 3: Implement the image API and extensionless public handler

**Files:**
- Create: `internal/image/handler.go`
- Create: `internal/image/handler_test.go`
- Generate: `internal/image/mocks/mock_operations.go`
- Modify: `.mockery.yaml`

**Interfaces:**
- Consumes an image-owned `Operations` interface.
- Produces ordered multi-image upload, replacement, deletion, and extensionless viewing.

- [ ] **Step 1: Define and generate the handler boundary**

```go
type Operations interface {
    CreateImages(context.Context, CreateInput) ([]Result, error)
    Get(context.Context, string) (Image, error)
    Update(context.Context, UpdateInput) (Result, error)
    Delete(context.Context, string) error
}
type HandlerConfig struct { MaxImageBytes, MaxRequestBytes int64 }
type Handler struct { operations Operations; maxImageBytes, maxRequestBytes int64 }
func NewHandler(operations Operations, config HandlerConfig) (*Handler, error)
func (h *Handler) Register(*http.ServeMux)
```

Add `Operations` to the image package's `.mockery.yaml` interfaces with `filename: mock_operations.go` and `structname: MockOperations`, then run the pinned generator.

- [ ] **Step 2: Write failing upload and mutation tests**

Assert:

- repeated `images` fields arrive in order and 201 returns the same ordered `images` array.
- no service call occurs when any part or the aggregate request exceeds its limit.
- PUT accepts one `file`, can return a different extension/media type, preserves `/images/{id}`, and returns 200.
- DELETE returns 204.
- invalid form fields, no image parts, invalid expiration, service failures, and cancellation map to the stable response contract.
- the exact incoming context reaches every mocked operation.

- [ ] **Step 3: Write failing public image tests**

Mock `Get` to return PNG and then WebP records for the same ID. Assert both requests use `/images/{id}`, the response `Content-Type` changes, and headers include:

```text
Content-Disposition: inline; filename="<id>.<extension>"
Cache-Control: no-store
X-Content-Type-Options: nosniff
X-Robots-Tag: noindex, nofollow, noarchive, noimageindex
```

Assert the bytes are exact and a missing/expired ID uses the JSON 404 contract.

- [ ] **Step 4: Run tests to verify failure**

Run: `go test ./internal/image -run Handler -v`

Expected: FAIL because the handler is not implemented.

- [ ] **Step 5: Implement all image routes**

Register:

```go
mux.HandleFunc("POST "+httpx.APIImages, h.create)
mux.HandleFunc("PUT "+httpx.APIImages+"/{id}", h.update)
mux.HandleFunc("DELETE "+httpx.APIImages+"/{id}", h.delete)
mux.HandleFunc("GET "+httpx.PublicImages+"{id}", h.view)
```

Validate IDs before service calls. Use only detected domain media metadata in responses; ignore submitted filename and MIME type. Because normalized extensions include their leading dot, set `Content-Disposition` from `mime.FormatMediaType("inline", map[string]string{"filename": image.ID + image.Extension})`.

- [ ] **Step 6: Verify and commit**

Run: `go test ./internal/image -run Handler -v`

Expected: PASS.

```bash
git add internal/image .mockery.yaml
git commit -m "feat: add image HTTP endpoints"
```

### Task 4: Build and embed the browser renderer assets

**Files:**
- Create: `package.json`
- Create: `pnpm-lock.yaml`
- Create: `internal/assets/src/viewer.js`
- Create: `internal/assets/src/viewer.css`
- Create: `internal/assets/src/viewer.test.js`
- Create: `internal/assets/scripts/build.mjs`
- Create: `internal/assets/scripts/check.mjs`
- Create: `internal/assets/dist/viewer.min.js`
- Create: `internal/assets/dist/viewer.min.css`
- Create: `internal/assets/manifest.json`
- Create: `internal/assets/assets.go`
- Create: `internal/assets/assets_test.go`

**Interfaces:**
- Produces immutable embedded `ViewerJS()` and `ViewerCSS()` bytes.
- Browser entry reads Markdown from `#agent-whiteboard-source`, renders it, and exports pure helpers for unit testing.

- [ ] **Step 1: Pin the JavaScript workspace**

Create `package.json` with `packageManager: "pnpm@11.4.0"`, `engines: {"node": ">=24 <25"}`, `type: "module"`, scripts `test`, `build`, and `check:assets`, exact runtime dependency versions from this plan, and exact dev dependencies `esbuild@0.28.1`, `vitest@4.1.10`, and `jsdom@29.1.1`. Set the test script to `vitest run --environment jsdom internal/assets/src/viewer.test.js` so no separate Vitest configuration file is needed.

Run: `corepack pnpm install`

Expected: `pnpm-lock.yaml` records exact resolved versions.

- [ ] **Step 2: Write failing renderer tests**

In `viewer.test.js`, assert:

- headings, tables, blockquotes, fenced code, links, and task lists render.
- raw Markdown HTML and `javascript:` links do not survive.
- the first H1 supplies the title; otherwise it is `Untitled whiteboard`.
- only `light`, `dark`, and `system` are accepted; unknown stored values normalize to `system`.
- each Mermaid fence becomes an indexed placeholder and retains source outside HTML attributes.
- an invalid Mermaid render replaces only its own placeholder with an error block.
- theme changes re-render all diagrams from retained source.

Mock only the Mermaid module's `initialize` and `render` functions in JavaScript unit tests; do not mock DOMPurify or markdown-it.

- [ ] **Step 3: Run tests to verify failure**

Run: `corepack pnpm test`

Expected: FAIL because `viewer.js` is not implemented.

- [ ] **Step 4: Implement the browser pipeline**

Configure markdown-it with `html: false`, `linkify: true`, and a custom fence renderer. For `mermaid` fences, push source into an array and emit `<div class="mermaid-placeholder" data-index="N"></div>`. Add a token-level rule that turns `[ ]` and `[x]` list-item prefixes into disabled checkboxes without enabling raw HTML input.

Render in this exact order:

```text
JSON source -> markdown-it -> DOMPurify -> DOM insertion
            -> highlight.js on code blocks
            -> mermaid.initialize({startOnLoad:false, securityLevel:"strict", theme})
            -> mermaid.render per placeholder -> DOMPurify SVG profile -> insertion
```

Catch each Mermaid render independently. Retain the source array for light/dark/system re-rendering. Store only `agent-whiteboard-theme` in localStorage and subscribe to live `prefers-color-scheme` changes in system mode.

Initialize Mermaid with `secure: ["secure", "securityLevel", "startOnLoad", "maxTextSize", "suppressErrorRendering", "maxEdges", "theme", "themeCSS", "themeVariables"]` in addition to the fixed values so document directives cannot weaken defaults or override application-selected themes. Resolve `system` to Mermaid's light or dark theme before each render.

- [ ] **Step 5: Build deterministic committed assets**

`build.mjs` invokes esbuild twice with fixed options: browser platform, IIFE JavaScript output, minification, `legalComments: "none"`, and a separate CSS output. Third-party notices are added and embedded in plan 4 before release acceptance. It writes `dist/viewer.min.js`, `dist/viewer.min.css`, and `manifest.json` containing the four runtime library versions plus the bundler version and SHA-256 hashes of both outputs.

`check.mjs` builds into a temporary directory, byte-compares all three generated files, reports differing names, and removes the temporary directory in `finally`.

Run:

```bash
corepack pnpm test
corepack pnpm build
corepack pnpm run check:assets
```

Expected: all commands PASS and the asset check reports no differences.

- [ ] **Step 6: Embed assets in Go with copy-safe accessors**

```go
//go:embed dist/viewer.min.js dist/viewer.min.css manifest.json
var files embed.FS

func ViewerJS() []byte
func ViewerCSS() []byte
func Manifest() []byte
```

Return fresh byte slices so callers cannot mutate shared embedded state. Go tests assert non-empty bytes, expected manifest versions, no `http://`, `https://`, `<script src`, or stylesheet link references, and mutation isolation between calls.

- [ ] **Step 7: Verify and commit**

Run: `go test ./internal/assets -v`

Expected: PASS.

```bash
git add package.json pnpm-lock.yaml internal/assets
git commit -m "feat: bundle browser whiteboard renderer"
```

### Task 5: Generate and serve the Markdown viewer shell and standalone HTML

**Files:**
- Create: `internal/whiteboard/viewer.go`
- Create: `internal/whiteboard/viewer_test.go`
- Modify: `internal/whiteboard/handler.go`
- Modify: `internal/whiteboard/handler_test.go`

**Interfaces:**
- Produces `NewViewer(ViewerConfig)` and `Viewer.Render(io.Writer, Whiteboard) error` without importing the assets package.
- Completes public whiteboard routes.

- [ ] **Step 1: Write failing viewer-shell tests**

Inject sentinel CSS and JavaScript bytes. Render Markdown containing `</script>`, `<`, `>`, `&`, U+2028, and U+2029. Parse the output with `golang.org/x/net/html` and assert:

- one doctype and explicit html/head/body elements.
- a robots meta element with `noindex, nofollow, noarchive`.
- one `script[type=application/json][id=agent-whiteboard-source]` whose JSON decodes to the exact source.
- CSS and JavaScript appear inline, with no external `src` or stylesheet `href`.
- the shell includes a `<noscript>` explanation.
- the JSON source cannot terminate the script element.

- [ ] **Step 2: Implement a streaming viewer**

```go
type ViewerConfig struct { CSS, JS []byte }
type Viewer struct { css, js []byte }
func NewViewer(ViewerConfig) (*Viewer, error)
func (v *Viewer) Render(w io.Writer, board Whiteboard) error
```

Use `encoding/json.Encoder` with default HTML escaping to encode a struct such as `{"markdown":"..."}` inside the non-executable script. Copy injected assets in the constructor. Do not use `html/template` to transform user Markdown and do not render Markdown on the server.

- [ ] **Step 3: Write failing public-route tests**

Assert:

- `GET /whiteboards/markdown/{id}` calls `Get` with the same request context, rejects HTML kind as 404, returns the viewer shell as `text/html; charset=utf-8`, and sets all public headers.
- `GET /whiteboards/html/{id}` rejects Markdown kind as 404 and serves exact stored bytes, including inline script/style, without injecting or rewriting anything.
- missing, expired, malformed, and wrong-kind IDs all return the same JSON 404.
- POST/PUT/DELETE routes continue to pass after public routes are registered.

- [ ] **Step 4: Implement and register public routes**

```go
mux.HandleFunc("GET "+httpx.PublicMarkdown+"{id}", h.viewMarkdown)
mux.HandleFunc("GET "+httpx.PublicHTML+"{id}", h.viewHTML)
```

Validate the ID, fetch once, compare kind, set headers before writing, and never expose a raw Markdown route.

- [ ] **Step 5: Verify and commit**

Run: `go test ./internal/whiteboard -run 'Viewer|Handler' -v`

Expected: PASS.

```bash
git add internal/whiteboard
git commit -m "feat: serve public whiteboards"
```

### Task 6: Add health/readiness HTTP behavior and phase acceptance

**Files:**
- Modify: `internal/http/http.go`
- Modify: `internal/http/http_test.go`

**Interfaces:**
- Produces an injected readiness handler for the composition root in plan 3.

- [ ] **Step 1: Write failing health tests**

Define a narrow interface:

```go
type Readiness interface { Ready(context.Context) error }
func RegisterHealth(mux *http.ServeMux, readiness Readiness)
```

Assert `/healthz` always returns 200 while the router is alive. Assert `/readyz` passes the exact request context, returns 200 when ready, and returns 503 after readiness is disabled or dependency checks fail. Both endpoints return small JSON objects and `Cache-Control: no-store`.

- [ ] **Step 2: Implement the health handlers**

Register exact GET method patterns. Do not expose dependency error details. The readiness implementation itself will live in `internal/app` in plan 3.

- [ ] **Step 3: Regenerate mocks and verify generated output**

Run: `go run github.com/vektra/mockery/v3@v3.7.1`

Run: `git diff --exit-code -- internal/whiteboard/mocks internal/image/mocks`

Expected: exit 0 after generated files have been intentionally staged or no generator drift exists.

- [ ] **Step 4: Run phase verification**

```bash
gofmt -w internal/http internal/whiteboard internal/image internal/assets
go vet ./internal/...
go test ./internal/...
go test -race ./internal/...
corepack pnpm test
corepack pnpm run check:assets
```

Expected: every command exits 0 and the race detector reports no races.

- [ ] **Step 5: Commit**

```bash
git add internal/http internal/whiteboard internal/image internal/assets package.json pnpm-lock.yaml .mockery.yaml
git commit -m "test: verify HTTP and viewer contracts"
```

Skip this commit only when the worktree is already clean after verification.
