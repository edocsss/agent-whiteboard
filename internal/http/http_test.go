package http_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/edocsss/agent-whiteboard/internal/common"
	httpx "github.com/edocsss/agent-whiteboard/internal/http"
	"github.com/stretchr/testify/require"
)

func TestRouteConstants(t *testing.T) {
	require.Equal(t, "/api/v1/whiteboards/markdown", httpx.APIWhiteboardMarkdown)
	require.Equal(t, "/api/v1/whiteboards/html", httpx.APIWhiteboardHTML)
	require.Equal(t, "/api/v1/images", httpx.APIImages)
	require.Equal(t, "/whiteboards/markdown/", httpx.PublicMarkdown)
	require.Equal(t, "/whiteboards/html/", httpx.PublicHTML)
	require.Equal(t, "/images/", httpx.PublicImages)
}

func TestWriteJSONUsesExactWireShapeAndTrailingNewline(t *testing.T) {
	createdAt := time.Date(2026, time.July, 16, 12, 0, 0, 0, time.UTC)
	expiresAt := int64(1784289600)
	rr := httptest.NewRecorder()

	httpx.WriteJSON(rr, http.StatusCreated, httpx.ResourceResponse{Resource: httpx.Resource{
		ID:        "resource-id",
		Type:      "markdown",
		Path:      "/whiteboards/markdown/resource-id",
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
		ExpiresAt: &expiresAt,
		Permanent: false,
	}})

	require.Equal(t, http.StatusCreated, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.Equal(t, "{\"resource\":{\"id\":\"resource-id\",\"type\":\"markdown\",\"path\":\"/whiteboards/markdown/resource-id\",\"created_at\":\"2026-07-16T12:00:00Z\",\"updated_at\":\"2026-07-16T12:00:00Z\",\"expires_at\":1784289600,\"permanent\":false}}\n", rr.Body.String())
}

func TestWriteError(t *testing.T) {
	tests := []struct {
		name       string
		code       common.ErrorCode
		statusCode int
	}{
		{name: "invalid request", code: common.CodeInvalidRequest, statusCode: http.StatusBadRequest},
		{name: "not found", code: common.CodeNotFound, statusCode: http.StatusNotFound},
		{name: "content too large", code: common.CodeContentTooLarge, statusCode: http.StatusRequestEntityTooLarge},
		{name: "unsupported media type", code: common.CodeUnsupportedMediaType, statusCode: http.StatusUnsupportedMediaType},
		{name: "storage unavailable", code: common.CodeStorageUnavailable, statusCode: http.StatusServiceUnavailable},
		{name: "internal error", code: common.CodeInternal, statusCode: http.StatusInternalServerError},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			err := fmt.Errorf("operation failed: %w", common.NewError(test.code, test.name, errors.New("secret path")))

			httpx.WriteError(rr, err)

			require.Equal(t, test.statusCode, rr.Code)
			require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
			require.Equal(t, fmt.Sprintf("{\"error\":{\"code\":%q,\"message\":%q}}\n", test.code, test.name), rr.Body.String())
			require.NotContains(t, rr.Body.String(), "secret path")
			require.NotContains(t, rr.Body.String(), "operation failed")
		})
	}
}

func TestWriteErrorHidesUnknownErrors(t *testing.T) {
	rr := httptest.NewRecorder()

	httpx.WriteError(rr, errors.New("database password leaked"))

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.Equal(t, "{\"error\":{\"code\":\"internal_error\",\"message\":\"internal error\"}}\n", rr.Body.String())
	require.NotContains(t, rr.Body.String(), "database password")
}

func TestWriteErrorHidesInternalDomainMessage(t *testing.T) {
	rr := httptest.NewRecorder()

	httpx.WriteError(rr, common.NewError(common.CodeInternal, "database password leaked", errors.New("secret path")))

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.Equal(t, "{\"error\":{\"code\":\"internal_error\",\"message\":\"internal error\"}}\n", rr.Body.String())
	require.NotContains(t, rr.Body.String(), "database password")
	require.NotContains(t, rr.Body.String(), "secret path")
}

func TestWriteErrorHidesUnrecognizedDomainError(t *testing.T) {
	rr := httptest.NewRecorder()

	httpx.WriteError(rr, common.NewError(common.ErrorCode("future_error"), "private implementation detail", errors.New("secret cause")))

	require.Equal(t, http.StatusInternalServerError, rr.Code)
	require.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	require.Equal(t, "{\"error\":{\"code\":\"internal_error\",\"message\":\"internal error\"}}\n", rr.Body.String())
	require.NotContains(t, rr.Body.String(), "private implementation detail")
	require.NotContains(t, rr.Body.String(), "secret cause")
}

func TestSetPublicHeaders(t *testing.T) {
	tests := []struct {
		name   string
		image  bool
		robots string
	}{
		{name: "whiteboard", robots: "noindex, nofollow, noarchive"},
		{name: "image", image: true, robots: "noindex, nofollow, noarchive, noimageindex"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rr := httptest.NewRecorder()

			httpx.SetPublicHeaders(rr, test.image)

			require.Equal(t, "no-store", rr.Header().Get("Cache-Control"))
			require.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))
			require.Equal(t, test.robots, rr.Header().Get("X-Robots-Tag"))
		})
	}
}

func TestReadMultipartRejectsAggregateOverflow(t *testing.T) {
	body, contentType := multipartBody(t, []multipartValue{
		{fieldName: "file", filename: "board.md", content: "content"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	_, err := httpx.ReadMultipart(httptest.NewRecorder(), req, int64(len(body)-1), 1024, "file")

	require.Error(t, err)
	require.True(t, common.HasCode(err, common.CodeContentTooLarge), "expected content_too_large, got %v", err)
}

func TestReadMultipartAcceptsAggregateAtLimit(t *testing.T) {
	body, contentType := multipartBody(t, []multipartValue{
		{fieldName: "file", filename: "board.md", content: "content"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	form, err := httpx.ReadMultipart(httptest.NewRecorder(), req, int64(len(body)), 7, "file")

	require.NoError(t, err)
	require.Len(t, form.Files, 1)
	require.Equal(t, []byte("content"), form.Files[0].Content)
}

func TestReadMultipartRejectsChunkedOverflowAfterTerminalBoundary(t *testing.T) {
	prefix, contentType := multipartBody(t, []multipartValue{
		{fieldName: "file", filename: "board.md", content: "content"},
	})
	body := append(append([]byte(nil), prefix...), bytes.Repeat([]byte("epilogue"), 8)...)
	req := httptest.NewRequest(http.MethodPost, "/", &chunkedReader{content: body, chunkSize: 1})
	req.Header.Set("Content-Type", contentType)
	require.Equal(t, int64(-1), req.ContentLength)

	_, err := httpx.ReadMultipart(httptest.NewRecorder(), req, int64(len(prefix)), 1024, "file")

	require.Error(t, err)
	require.True(t, common.HasCode(err, common.CodeContentTooLarge), "expected content_too_large, got %v", err)
}

func TestReadPartRejectsPerPartOverflow(t *testing.T) {
	part := firstMultipartPart(t, "images", "image.png", "four")

	_, err := httpx.ReadPart(part, 3)

	require.Error(t, err)
	require.True(t, common.HasCode(err, common.CodeContentTooLarge), "expected content_too_large, got %v", err)
}

func TestReadPartAcceptsPartAtLimit(t *testing.T) {
	part := firstMultipartPart(t, "images", "image.png", "four")

	content, err := httpx.ReadPart(part, 4)

	require.NoError(t, err)
	require.Equal(t, []byte("four"), content)
}

func TestReadMultipartRejectsMalformedBoundary(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("--different\r\ncontent"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=expected")

	_, err := httpx.ReadMultipart(httptest.NewRecorder(), req, 1024, 1024, "file")

	require.Error(t, err)
	require.True(t, common.HasCode(err, common.CodeInvalidRequest), "expected invalid_request, got %v", err)
}

func TestReadMultipartRejectsInvalidSignedExpiration(t *testing.T) {
	for _, value := range []string{"", "--1", "1.5", "9223372036854775808"} {
		t.Run(fmt.Sprintf("value_%q", value), func(t *testing.T) {
			body, contentType := multipartBody(t, []multipartValue{
				{fieldName: "file", filename: "board.md", content: "content"},
				{fieldName: "expires_in_seconds", content: value},
			})
			req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
			req.Header.Set("Content-Type", contentType)

			_, err := httpx.ReadMultipart(httptest.NewRecorder(), req, int64(len(body)), 1024, "file")

			require.Error(t, err)
			require.True(t, common.HasCode(err, common.CodeInvalidRequest), "expected invalid_request, got %v", err)
		})
	}
}

func TestReadMultipartParsesSignedExpiration(t *testing.T) {
	body, contentType := multipartBody(t, []multipartValue{
		{fieldName: "file", filename: "board.md", content: "content"},
		{fieldName: "expires_in_seconds", content: "-1"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	form, err := httpx.ReadMultipart(httptest.NewRecorder(), req, int64(len(body)), 1024, "file")

	require.NoError(t, err)
	require.Equal(t, int64(-1), *form.ExpiresInSeconds)
}

func TestReadMultipartLeavesOmittedExpirationNil(t *testing.T) {
	body, contentType := multipartBody(t, []multipartValue{
		{fieldName: "file", filename: "board.md", content: "content"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	form, err := httpx.ReadMultipart(httptest.NewRecorder(), req, int64(len(body)), 1024, "file")

	require.NoError(t, err)
	require.Nil(t, form.ExpiresInSeconds)
}

func TestReadMultipartRejectsDuplicateExpiration(t *testing.T) {
	body, contentType := multipartBody(t, []multipartValue{
		{fieldName: "expires_in_seconds", content: "1"},
		{fieldName: "expires_in_seconds", content: "2"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	_, err := httpx.ReadMultipart(httptest.NewRecorder(), req, int64(len(body)), 1024, "file")

	require.Error(t, err)
	require.True(t, common.HasCode(err, common.CodeInvalidRequest), "expected invalid_request, got %v", err)
}

func TestReadMultipartPreservesRepeatedImageOrder(t *testing.T) {
	body, contentType := multipartBody(t, []multipartValue{
		{fieldName: "images", filename: "first.png", content: "first"},
		{fieldName: "images", filename: "second.png", content: "second"},
		{fieldName: "images", filename: "third.png", content: "third"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	form, err := httpx.ReadMultipart(httptest.NewRecorder(), req, int64(len(body)), 1024, "images")

	require.NoError(t, err)
	require.Equal(t, []string{"first.png", "second.png", "third.png"}, []string{
		form.Files[0].Filename,
		form.Files[1].Filename,
		form.Files[2].Filename,
	})
	require.Equal(t, []string{"first", "second", "third"}, []string{
		string(form.Files[0].Content),
		string(form.Files[1].Content),
		string(form.Files[2].Content),
	})
}

func TestReadMultipartRejectsUnknownFileField(t *testing.T) {
	body, contentType := multipartBody(t, []multipartValue{
		{fieldName: "thumbnail", filename: "image.png", content: "content"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)

	_, err := httpx.ReadMultipart(httptest.NewRecorder(), req, int64(len(body)), 1024, "images")

	require.Error(t, err)
	require.True(t, common.HasCode(err, common.CodeInvalidRequest), "expected invalid_request, got %v", err)
}

type multipartValue struct {
	fieldName string
	filename  string
	content   string
}

type chunkedReader struct {
	content   []byte
	chunkSize int
}

func (r *chunkedReader) Read(destination []byte) (int, error) {
	if len(r.content) == 0 {
		return 0, io.EOF
	}
	read := min(len(destination), r.chunkSize, len(r.content))
	copy(destination, r.content[:read])
	r.content = r.content[read:]
	return read, nil
}

func multipartBody(t *testing.T, values []multipartValue) ([]byte, string) {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, value := range values {
		var (
			partWriter io.Writer
			err        error
		)
		if value.filename == "" {
			partWriter, err = writer.CreateFormField(value.fieldName)
		} else {
			header := make(textproto.MIMEHeader)
			header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, value.fieldName, value.filename))
			header.Set("Content-Type", "application/octet-stream")
			partWriter, err = writer.CreatePart(header)
		}
		require.NoError(t, err)
		_, err = partWriter.Write([]byte(value.content))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	return body.Bytes(), writer.FormDataContentType()
}

func firstMultipartPart(t *testing.T, fieldName, filename, content string) *multipart.Part {
	t.Helper()

	body, contentType := multipartBody(t, []multipartValue{{
		fieldName: fieldName,
		filename:  filename,
		content:   content,
	}})
	_, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	reader := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	part, err := reader.NextPart()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, part.Close()) })
	return part
}
