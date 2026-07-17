package whiteboard_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/edocsss/agent-whiteboard/internal/common"
	httpx "github.com/edocsss/agent-whiteboard/internal/http"
	"github.com/edocsss/agent-whiteboard/internal/whiteboard"
	"github.com/edocsss/agent-whiteboard/internal/whiteboard/mocks"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const (
	testWhiteboardID = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	defaultMaxBytes  = int64(1 << 20)
)

type handlerContextKey struct{}

type whiteboardRoute struct {
	name       string
	kind       whiteboard.Kind
	apiPath    string
	publicPath string
}

var whiteboardRoutes = []whiteboardRoute{
	{name: "markdown", kind: whiteboard.KindMarkdown, apiPath: httpx.APIWhiteboardMarkdown, publicPath: httpx.PublicMarkdown},
	{name: "html", kind: whiteboard.KindHTML, apiPath: httpx.APIWhiteboardHTML, publicPath: httpx.PublicHTML},
}

func TestHandlerConstructorRejectsInvalidDependenciesAndLimits(t *testing.T) {
	viewer := newViewer(t)
	var typedNil *mocks.MockOperations

	tests := []struct {
		name       string
		operations whiteboard.Operations
		viewer     *whiteboard.Viewer
		maxBytes   int64
	}{
		{name: "nil operations", viewer: viewer},
		{name: "typed nil operations", operations: typedNil, viewer: viewer},
		{name: "nil viewer", operations: mocks.NewMockOperations(t)},
		{name: "negative max bytes", operations: mocks.NewMockOperations(t), viewer: viewer, maxBytes: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, err := whiteboard.NewHandler(tt.operations, tt.viewer, whiteboard.HandlerConfig{MaxBytes: tt.maxBytes})

			require.Nil(t, handler)
			require.Error(t, err)
			require.True(t, common.HasCode(err, common.CodeInvalidRequest), "expected invalid_request, got %v", err)
		})
	}
}

func TestHandlerConstructorAcceptsZeroLimit(t *testing.T) {
	handler, err := whiteboard.NewHandler(mocks.NewMockOperations(t), newViewer(t), whiteboard.HandlerConfig{})

	require.NoError(t, err)
	require.NotNil(t, handler)
}

func TestHandlerCreateReturnsResourceAndPassesExactContext(t *testing.T) {
	createdAt := time.Date(2026, time.July, 17, 3, 4, 5, 0, time.UTC)
	expiresAt := createdAt.Add(5 * time.Minute)
	expiresIn := int64(300)

	for _, route := range whiteboardRoutes {
		t.Run(route.name, func(t *testing.T) {
			operations := mocks.NewMockOperations(t)
			ctx := context.WithValue(context.Background(), handlerContextKey{}, "sentinel")
			result := whiteboard.Result{
				ID:        testWhiteboardID,
				Kind:      route.kind,
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				ExpiresAt: &expiresAt,
			}
			expectedContext := mock.MatchedBy(func(got context.Context) bool {
				return got == ctx && got.Value(handlerContextKey{}) == "sentinel"
			})
			expectedInput := mock.MatchedBy(func(got whiteboard.CreateInput) bool {
				return bytes.Equal(got.Source, []byte("source body")) &&
					got.ExpiresInSeconds != nil && *got.ExpiresInSeconds == expiresIn
			})
			if route.kind == whiteboard.KindMarkdown {
				operations.EXPECT().CreateMarkdown(expectedContext, expectedInput).Return(result, nil).Once()
			} else {
				operations.EXPECT().CreateHTML(expectedContext, expectedInput).Return(result, nil).Once()
			}
			handler := newHandler(t, operations, defaultMaxBytes)
			body, contentType := multipartRequestBody(t,
				multipartField{name: "file", filename: "board.txt", value: "source body"},
				multipartField{name: "expires_in_seconds", value: fmt.Sprint(expiresIn)},
			)
			req := httptest.NewRequest(http.MethodPost, route.apiPath, bytes.NewReader(body)).WithContext(ctx)
			req.Header.Set("Content-Type", contentType)
			rr := httptest.NewRecorder()

			handlerMux(t, handler).ServeHTTP(rr, req)

			require.Equal(t, http.StatusCreated, rr.Code)
			require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
			resource := decodeResource(t, rr)
			require.Equal(t, httpx.Resource{
				ID:        testWhiteboardID,
				Type:      string(route.kind),
				Path:      route.publicPath + testWhiteboardID,
				CreatedAt: createdAt,
				UpdatedAt: createdAt,
				ExpiresAt: int64Pointer(expiresAt.Unix()),
				Permanent: false,
			}, resource)
		})
	}
}

func TestHandlerUpdateReturnsResourceAndPassesExactContext(t *testing.T) {
	createdAt := time.Date(2026, time.July, 16, 3, 4, 5, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)

	for _, route := range whiteboardRoutes {
		t.Run(route.name, func(t *testing.T) {
			operations := mocks.NewMockOperations(t)
			ctx := context.WithValue(context.Background(), handlerContextKey{}, "sentinel")
			operations.EXPECT().Update(
				mock.MatchedBy(func(got context.Context) bool {
					return got == ctx && got.Value(handlerContextKey{}) == "sentinel"
				}),
				mock.MatchedBy(func(got whiteboard.UpdateInput) bool {
					return got.ID == testWhiteboardID && got.Kind == route.kind &&
						bytes.Equal(got.Source, []byte("replacement")) && got.ExpiresInSeconds == nil
				}),
			).Return(whiteboard.Result{
				ID:        testWhiteboardID,
				Kind:      route.kind,
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			}, nil).Once()
			handler := newHandler(t, operations, defaultMaxBytes)
			body, contentType := multipartRequestBody(t,
				multipartField{name: "file", filename: "board.txt", value: "replacement"},
			)
			req := httptest.NewRequest(http.MethodPut, route.apiPath+"/"+testWhiteboardID, bytes.NewReader(body)).WithContext(ctx)
			req.Header.Set("Content-Type", contentType)
			rr := httptest.NewRecorder()

			handlerMux(t, handler).ServeHTTP(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)
			resource := decodeResource(t, rr)
			require.Equal(t, httpx.Resource{
				ID:        testWhiteboardID,
				Type:      string(route.kind),
				Path:      route.publicPath + testWhiteboardID,
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
				Permanent: true,
			}, resource)
		})
	}
}

func TestHandlerDeleteReturnsNoContentAndPassesExactContext(t *testing.T) {
	for _, route := range whiteboardRoutes {
		t.Run(route.name, func(t *testing.T) {
			operations := mocks.NewMockOperations(t)
			ctx := context.WithValue(context.Background(), handlerContextKey{}, "sentinel")
			operations.EXPECT().Delete(
				mock.MatchedBy(func(got context.Context) bool {
					return got == ctx && got.Value(handlerContextKey{}) == "sentinel"
				}),
				route.kind,
				testWhiteboardID,
			).Return(nil).Once()
			handler := newHandler(t, operations, defaultMaxBytes)
			req := httptest.NewRequest(http.MethodDelete, route.apiPath+"/"+testWhiteboardID, nil).WithContext(ctx)
			rr := httptest.NewRecorder()

			handlerMux(t, handler).ServeHTTP(rr, req)

			require.Equal(t, http.StatusNoContent, rr.Code)
			require.Empty(t, rr.Body.String())
		})
	}
}

func TestHandlerViewMarkdownRendersShellWithExactContextAndPublicHeaders(t *testing.T) {
	ctx := context.WithValue(context.Background(), handlerContextKey{}, "sentinel")
	operations := mocks.NewMockOperations(t)
	operations.EXPECT().Get(
		mock.MatchedBy(func(got context.Context) bool {
			return got == ctx && got.Value(handlerContextKey{}) == "sentinel"
		}),
		testWhiteboardID,
	).Return(whiteboard.Whiteboard{
		ID:     testWhiteboardID,
		Kind:   whiteboard.KindMarkdown,
		Source: []byte("# Public whiteboard"),
	}, nil).Once()
	req := httptest.NewRequest(http.MethodGet, httpx.PublicMarkdown+testWhiteboardID, nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	handlerMux(t, newHandler(t, operations, defaultMaxBytes)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))
	require.Contains(t, rr.Body.String(), testViewerCSS)
	require.Contains(t, rr.Body.String(), testViewerJS)
	require.Contains(t, rr.Body.String(), `{"markdown":"# Public whiteboard"}`)
	assertPublicWhiteboardHeaders(t, rr)
}

func TestHandlerViewHTMLServesStoredDocumentBytesUnchanged(t *testing.T) {
	source := []byte("<!DOCTYPE html>\n<html><head><style>body { color: red; }</style></head>\n<body><script>globalThis.answer = 42;</script></body></html>\n")
	ctx := context.WithValue(context.Background(), handlerContextKey{}, "sentinel")
	operations := mocks.NewMockOperations(t)
	operations.EXPECT().Get(
		mock.MatchedBy(func(got context.Context) bool {
			return got == ctx && got.Value(handlerContextKey{}) == "sentinel"
		}),
		testWhiteboardID,
	).Return(whiteboard.Whiteboard{
		ID:     testWhiteboardID,
		Kind:   whiteboard.KindHTML,
		Source: source,
	}, nil).Once()
	req := httptest.NewRequest(http.MethodGet, httpx.PublicHTML+testWhiteboardID, nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	handlerMux(t, newHandler(t, operations, defaultMaxBytes)).ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	require.Equal(t, "text/html; charset=utf-8", rr.Header().Get("Content-Type"))
	require.Equal(t, source, rr.Body.Bytes())
	require.NotContains(t, rr.Body.String(), testViewerCSS)
	require.NotContains(t, rr.Body.String(), testViewerJS)
	assertPublicWhiteboardHeaders(t, rr)
}

func TestHandlerPublicViewsHideMalformedMissingExpiredAndWrongKindAsSameNotFound(t *testing.T) {
	wantBody := "{\"error\":{\"code\":\"not_found\",\"message\":\"resource not found\"}}\n"

	for _, route := range whiteboardRoutes {
		t.Run(route.name, func(t *testing.T) {
			tests := []struct {
				name       string
				id         string
				operations func(*testing.T) *mocks.MockOperations
			}{
				{
					name: "malformed",
					id:   "malformed",
					operations: func(t *testing.T) *mocks.MockOperations {
						return mocks.NewMockOperations(t)
					},
				},
				{
					name: "missing",
					id:   testWhiteboardID,
					operations: func(t *testing.T) *mocks.MockOperations {
						operations := mocks.NewMockOperations(t)
						operations.EXPECT().Get(mock.Anything, testWhiteboardID).Return(
							whiteboard.Whiteboard{},
							common.NewError(common.CodeNotFound, "resource not found", errors.New("missing private record")),
						).Once()
						return operations
					},
				},
				{
					name: "expired",
					id:   testWhiteboardID,
					operations: func(t *testing.T) *mocks.MockOperations {
						operations := mocks.NewMockOperations(t)
						operations.EXPECT().Get(mock.Anything, testWhiteboardID).Return(
							whiteboard.Whiteboard{},
							common.NewError(common.CodeNotFound, "resource not found", errors.New("expired private record")),
						).Once()
						return operations
					},
				},
				{
					name: "wrong kind",
					id:   testWhiteboardID,
					operations: func(t *testing.T) *mocks.MockOperations {
						operations := mocks.NewMockOperations(t)
						wrongKind := whiteboard.KindHTML
						if route.kind == whiteboard.KindHTML {
							wrongKind = whiteboard.KindMarkdown
						}
						operations.EXPECT().Get(mock.Anything, testWhiteboardID).Return(whiteboard.Whiteboard{
							ID: testWhiteboardID, Kind: wrongKind, Source: []byte("private source"),
						}, nil).Once()
						return operations
					},
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					req := httptest.NewRequest(http.MethodGet, route.publicPath+tt.id, nil)
					rr := httptest.NewRecorder()

					handlerMux(t, newHandler(t, tt.operations(t), defaultMaxBytes)).ServeHTTP(rr, req)

					require.Equal(t, http.StatusNotFound, rr.Code)
					require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
					require.Equal(t, wantBody, rr.Body.String())
					require.NotContains(t, rr.Body.String(), tt.name)
					require.NotContains(t, rr.Body.String(), "private")
					assertPublicWhiteboardHeaders(t, rr)
				})
			}
		})
	}
}

func TestHandlerRejectsInvalidFormsBeforeServiceCalls(t *testing.T) {
	for _, route := range whiteboardRoutes {
		t.Run(route.name, func(t *testing.T) {
			tests := []struct {
				name       string
				fields     []multipartField
				maxBytes   int64
				wantStatus int
			}{
				{
					name:       "missing file",
					fields:     []multipartField{{name: "expires_in_seconds", value: "60"}},
					maxBytes:   defaultMaxBytes,
					wantStatus: http.StatusBadRequest,
				},
				{
					name: "extra file",
					fields: []multipartField{
						{name: "file", filename: "one.txt", value: "one"},
						{name: "file", filename: "two.txt", value: "two"},
					},
					maxBytes:   defaultMaxBytes,
					wantStatus: http.StatusBadRequest,
				},
				{
					name: "duplicate expiration",
					fields: []multipartField{
						{name: "file", filename: "board.txt", value: "source"},
						{name: "expires_in_seconds", value: "1"},
						{name: "expires_in_seconds", value: "2"},
					},
					maxBytes:   defaultMaxBytes,
					wantStatus: http.StatusBadRequest,
				},
				{
					name:       "oversized content",
					fields:     []multipartField{{name: "file", filename: "board.txt", value: strings.Repeat("x", 1024)}},
					maxBytes:   64,
					wantStatus: http.StatusRequestEntityTooLarge,
				},
			}

			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					operations := mocks.NewMockOperations(t)
					handler := newHandler(t, operations, tt.maxBytes)
					body, contentType := multipartRequestBody(t, tt.fields...)
					req := httptest.NewRequest(http.MethodPost, route.apiPath, bytes.NewReader(body))
					req.Header.Set("Content-Type", contentType)
					rr := httptest.NewRecorder()

					handlerMux(t, handler).ServeHTTP(rr, req)

					require.Equal(t, tt.wantStatus, rr.Code)
				})
			}
		})
	}
}

func TestHandlerHidesMalformedCapabilityIDsBeforeReadingFormsOrCallingService(t *testing.T) {
	wantBody := "{\"error\":{\"code\":\"not_found\",\"message\":\"resource not found\"}}\n"

	for _, route := range whiteboardRoutes {
		t.Run(route.name, func(t *testing.T) {
			for _, method := range []string{http.MethodPut, http.MethodDelete} {
				t.Run(method, func(t *testing.T) {
					operations := mocks.NewMockOperations(t)
					handler := newHandler(t, operations, defaultMaxBytes)
					req := httptest.NewRequest(method, route.apiPath+"/malformed", strings.NewReader("not multipart"))
					rr := httptest.NewRecorder()

					handlerMux(t, handler).ServeHTTP(rr, req)

					require.Equal(t, http.StatusNotFound, rr.Code)
					require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
					require.Equal(t, wantBody, rr.Body.String())
					require.NotContains(t, rr.Body.String(), "malformed")
					require.NotContains(t, rr.Body.String(), "invalid resource id")
				})
			}
		})
	}
}

func TestHandlerMapsWrongKindServiceErrorsToNotFound(t *testing.T) {
	for _, route := range whiteboardRoutes {
		t.Run(route.name, func(t *testing.T) {
			operations := mocks.NewMockOperations(t)
			operations.EXPECT().Update(mock.Anything, mock.MatchedBy(func(got whiteboard.UpdateInput) bool {
				return got.ID == testWhiteboardID && got.Kind == route.kind
			})).Return(whiteboard.Result{}, common.NewError(common.CodeNotFound, "resource not found", errors.New("wrong kind"))).Once()
			handler := newHandler(t, operations, defaultMaxBytes)
			body, contentType := multipartRequestBody(t,
				multipartField{name: "file", filename: "board.txt", value: "replacement"},
			)
			req := httptest.NewRequest(http.MethodPut, route.apiPath+"/"+testWhiteboardID, bytes.NewReader(body))
			req.Header.Set("Content-Type", contentType)
			rr := httptest.NewRecorder()

			handlerMux(t, handler).ServeHTTP(rr, req)

			require.Equal(t, http.StatusNotFound, rr.Code)
			require.Equal(t, httpx.ErrorBody{Code: common.CodeNotFound, Message: "resource not found"}, decodeError(t, rr))
			require.NotContains(t, rr.Body.String(), "wrong kind")
		})
	}
}

func TestHandlerRegistersOnlyExactMutationAndPublicViewRoutes(t *testing.T) {
	handler := newHandler(t, mocks.NewMockOperations(t), defaultMaxBytes)
	mux := handlerMux(t, handler)

	for _, route := range whiteboardRoutes {
		t.Run(route.name+" wrong management method", func(t *testing.T) {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, route.apiPath, nil))
			require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
			require.Equal(t, http.MethodPost, rr.Header().Get("Allow"))
		})

		t.Run(route.name+" wrong public method", func(t *testing.T) {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, route.publicPath+testWhiteboardID, nil))
			require.Equal(t, http.StatusMethodNotAllowed, rr.Code)
			require.Equal(t, "GET, HEAD", rr.Header().Get("Allow"))
		})

		t.Run(route.name+" empty public id is not registered", func(t *testing.T) {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, route.publicPath, nil))
			require.Equal(t, http.StatusNotFound, rr.Code)
		})

		t.Run(route.name+" nested public path is not registered", func(t *testing.T) {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, route.publicPath+testWhiteboardID+"/extra", nil))
			require.Equal(t, http.StatusNotFound, rr.Code)
		})

		t.Run(route.name+" trailing slash is not registered", func(t *testing.T) {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, route.apiPath+"/", nil))
			require.Equal(t, http.StatusNotFound, rr.Code)
		})
	}
}

func TestHandlerDoesNotLogRequestBodiesOrCapabilityIDs(t *testing.T) {
	var logs bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	operations := mocks.NewMockOperations(t)
	operations.EXPECT().Update(mock.Anything, mock.Anything).Return(
		whiteboard.Result{},
		common.NewError(common.CodeStorageUnavailable, "storage unavailable", errors.New("private backend failure")),
	).Once()
	handler := newHandler(t, operations, defaultMaxBytes)
	bodySecret := "private-whiteboard-source"
	body, contentType := multipartRequestBody(t,
		multipartField{name: "file", filename: "board.txt", value: bodySecret},
	)
	req := httptest.NewRequest(http.MethodPut, httpx.APIWhiteboardMarkdown+"/"+testWhiteboardID, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rr := httptest.NewRecorder()

	handlerMux(t, handler).ServeHTTP(rr, req)

	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
	require.NotContains(t, logs.String(), bodySecret)
	require.NotContains(t, logs.String(), testWhiteboardID)
}

type multipartField struct {
	name     string
	filename string
	value    string
}

func multipartRequestBody(t *testing.T, fields ...multipartField) ([]byte, string) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, field := range fields {
		var (
			partWriter interface{ Write([]byte) (int, error) }
			err        error
		)
		if field.filename == "" {
			partWriter, err = writer.CreateFormField(field.name)
		} else {
			partWriter, err = writer.CreateFormFile(field.name, field.filename)
		}
		require.NoError(t, err)
		_, err = partWriter.Write([]byte(field.value))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return body.Bytes(), writer.FormDataContentType()
}

func newHandler(t *testing.T, operations whiteboard.Operations, maxBytes int64) *whiteboard.Handler {
	t.Helper()

	handler, err := whiteboard.NewHandler(operations, newViewer(t), whiteboard.HandlerConfig{MaxBytes: maxBytes})
	require.NoError(t, err)
	return handler
}

func newViewer(t *testing.T) *whiteboard.Viewer {
	t.Helper()

	viewer, err := whiteboard.NewViewer(whiteboard.ViewerConfig{
		CSS: []byte(testViewerCSS),
		JS:  []byte(testViewerJS),
	})
	require.NoError(t, err)
	return viewer
}

func handlerMux(t *testing.T, handler *whiteboard.Handler) *http.ServeMux {
	t.Helper()

	mux := http.NewServeMux()
	handler.Register(mux)
	return mux
}

func decodeResource(t *testing.T, rr *httptest.ResponseRecorder) httpx.Resource {
	t.Helper()

	var response httpx.ResourceResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
	return response.Resource
}

func decodeError(t *testing.T, rr *httptest.ResponseRecorder) httpx.ErrorBody {
	t.Helper()
	return decodeErrorBody(t, rr).Error
}

func decodeErrorBody(t *testing.T, rr *httptest.ResponseRecorder) httpx.ErrorResponse {
	t.Helper()

	var response httpx.ErrorResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &response))
	return response
}

func int64Pointer(value int64) *int64 {
	return &value
}

func assertPublicWhiteboardHeaders(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, "no-store", rr.Header().Get("Cache-Control"))
	require.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))
	require.Equal(t, "noindex, nofollow, noarchive", rr.Header().Get("X-Robots-Tag"))
}
