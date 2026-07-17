# Go API

Import `github.com/edocsss/agent-whiteboard/pkg/agentwb`. This stable public facade forwards to the internal application composition root, which assembles domain services, HTTP handlers, lifecycle, and default filesystem storage.

```go
service, err := agentwb.New(agentwb.Config{
    RootDir: "/var/lib/agent-whiteboard",
    Host: "127.0.0.1",
    Port: 8567,
})
if err != nil { return err }
defer service.Close()
return service.ListenAndServe(ctx)
```

`Config` accepts `WhiteboardStore` and `ImageStore` independently. Missing stores use the built-in filesystem. Domain boundaries communicate through services; custom stores implement only their domain-owned interface:

```go
type WhiteboardStore interface {
    Create(context.Context, agentwb.Whiteboard) error
    Get(context.Context, string) (agentwb.Whiteboard, error)
    Replace(context.Context, agentwb.Whiteboard) error
    Delete(context.Context, string) error
    Ready(context.Context) error
    Close() error
}
```

`ImageStore` has the same method shape with `agentwb.Image`. Implementations must honor context cancellation, distinguish invalid/not-found/storage errors, preserve model metadata, make replacements atomic, report readiness, and make `Close` safe to call more than once. Inject them through `Config`; tests can inject testify/mock implementations. `WithClock`, `WithIDGenerator`, `WithListener`, `WithPort`, `WithDefaultExpiration`, and `WithViewerAssets` provide narrower dependency injection.

```go
service, err := agentwb.New(agentwb.Config{
    WhiteboardStore: myWhiteboardStore,
    ImageStore: myImageStore,
})
```

The service methods are `CreateMarkdown`, `CreateHTML`, `GetWhiteboard`, `UpdateWhiteboard`, `DeleteWhiteboard`, `CreateImages`, `GetImage`, `UpdateImage`, and `DeleteImage`. `Whiteboard` carries `ID`, `Kind`, `Source`, `CreatedAt`, `UpdatedAt`, and `ExpiresAt`; `Image` carries the same lifecycle fields plus `Extension`, `MediaType`, and `Content`. Input/result aliases expose the corresponding create/update metadata. Create expiration omission uses the configured default; zero means permanent. Update omission preserves expiration.

Every operation uses the caller's `context.Context`; cancellation/deadlines are propagated to the store and HTTP request. Do not replace a request context with a background context. The internally owned filesystem lifetime and bounded shutdown cleanup are the intentional exceptions.

`Handler()` embeds the application in another HTTP server. `Serve(ctx, listener)` uses an injected listener, `ListenAndServe(ctx)` uses the configured host/port, `Shutdown(ctx)` performs caller-bounded graceful shutdown, and `Close()` is idempotent and releases owned/custom stores. A supplied listener is selected with `agentwb.WithListener(listener)`.

Errors expose stable codes:

```go
var domainErr *agentwb.Error
if errors.As(err, &domainErr) { /* inspect domainErr.Code and domainErr.Message */ }
if agentwb.HasErrorCode(err, agentwb.CodeNotFound) { /* handle absence */ }
```

Exported codes are `CodeInvalidRequest`, `CodeNotFound`, `CodeContentTooLarge`, `CodeUnsupportedMediaType`, `CodeStorageUnavailable`, and `CodeInternal`. Prefer `errors.As`/`errors.Is` and `agentwb.HasErrorCode`; do not parse messages.
