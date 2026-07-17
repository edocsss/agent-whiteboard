package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/edocsss/agent-whiteboard/internal/common"
	httpx "github.com/edocsss/agent-whiteboard/internal/http"
	"github.com/stretchr/testify/require"
)

const clientTestID = "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3"

func TestNewClientAcceptsOnlyHTTPOrigins(t *testing.T) {
	t.Parallel()

	client := &http.Client{}
	for _, server := range []string{"http://example.test", "https://example.test:8443", "http://example.test/"} {
		server := server
		t.Run("accepts "+server, func(t *testing.T) {
			t.Parallel()
			got, err := httpx.NewClient(httpx.ClientConfig{Server: server, HTTPClient: client})
			require.NoError(t, err)
			require.NotNil(t, got)
		})
	}

	invalid := []string{
		"", "example.test", "/local", "ftp://example.test", "http://", "http:///missing-host",
		"http://user@example.test", "http://example.test/api", "http://example.test//",
		"http://example.test?debug=1", "http://example.test/#fragment",
	}
	for _, server := range invalid {
		server := server
		t.Run("rejects "+server, func(t *testing.T) {
			t.Parallel()
			got, err := httpx.NewClient(httpx.ClientConfig{Server: server, HTTPClient: client})
			require.Error(t, err)
			require.Nil(t, got)
		})
	}

	got, err := httpx.NewClient(httpx.ClientConfig{Server: "http://example.test"})
	require.Error(t, err)
	require.Nil(t, got)
}

func TestClientRejectsUnsupportedWhiteboardKindBeforeTransport(t *testing.T) {
	t.Parallel()

	transportCalled := false
	client := newTestClient(t, "http://example.test", &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		transportCalled = true
		return nil, errors.New("unexpected transport call")
	})})

	_, err := client.CreateWhiteboard(context.Background(), httpx.WhiteboardKind("text"), httpx.File{
		Name: "board.txt", Reader: strings.NewReader("body"),
	}, nil)
	require.Error(t, err)
	require.False(t, transportCalled)
}

func TestClientWhiteboardMutationsUseExactProtocol(t *testing.T) {
	t.Parallel()

	type expectation struct {
		method     string
		path       string
		field      string
		filename   string
		content    string
		expiration *string
		status     int
	}
	expectations := make(chan expectation, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expect := <-expectations
		require.Equal(t, expect.method, r.Method)
		require.Equal(t, expect.path, r.URL.EscapedPath())
		if expect.field == "" {
			require.Empty(t, r.Header.Get("Content-Type"))
		} else {
			parts := readMultipartParts(t, r)
			require.Equal(t, []multipartPart{{
				Field: expect.field, Filename: expect.filename, Content: expect.content,
			}}, fileParts(parts))
			require.Equal(t, expect.expiration, expirationPart(parts))
		}
		if expect.status == http.StatusNoContent {
			w.WriteHeader(expect.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(expect.status)
		_ = json.NewEncoder(w).Encode(httpx.ResourceResponse{Resource: httpx.Resource{
			ID: clientTestID, Type: "markdown", Path: httpx.PublicMarkdown + clientTestID,
		}})
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server.URL, server.Client())

	expectations <- expectation{
		method: http.MethodPost, path: httpx.APIWhiteboardMarkdown, field: "file",
		filename: "board.md", content: "# created", status: http.StatusCreated,
	}
	created, err := client.CreateWhiteboard(context.Background(), httpx.WhiteboardMarkdown, httpx.File{
		Name: "board.md", Reader: strings.NewReader("# created"),
	}, nil)
	require.NoError(t, err)
	require.Equal(t, clientTestID, created.ID)

	zero := int64(0)
	zeroText := "0"
	expectations <- expectation{
		method: http.MethodPut, path: httpx.APIWhiteboardHTML + "/" + clientTestID, field: "file",
		filename: "board.html", content: "<!doctype html>", expiration: &zeroText, status: http.StatusOK,
	}
	updated, err := client.UpdateWhiteboard(context.Background(), httpx.WhiteboardHTML, clientTestID, httpx.File{
		Name: "board.html", Reader: strings.NewReader("<!doctype html>"),
	}, &zero)
	require.NoError(t, err)
	require.Equal(t, clientTestID, updated.ID)

	expectations <- expectation{
		method: http.MethodDelete, path: httpx.APIWhiteboardMarkdown + "/" + clientTestID,
		status: http.StatusNoContent,
	}
	require.NoError(t, client.DeleteWhiteboard(context.Background(), httpx.WhiteboardMarkdown, clientTestID))
}

func TestClientImageMutationsPreserveOrderAndExpiration(t *testing.T) {
	t.Parallel()

	type expectation struct {
		method     string
		path       string
		files      []multipartPart
		expiration *string
		status     int
		many       bool
	}
	expectations := make(chan expectation, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expect := <-expectations
		require.Equal(t, expect.method, r.Method)
		require.Equal(t, expect.path, r.URL.EscapedPath())
		if len(expect.files) > 0 {
			parts := readMultipartParts(t, r)
			require.Equal(t, expect.files, fileParts(parts))
			require.Equal(t, expect.expiration, expirationPart(parts))
		}
		if expect.status == http.StatusNoContent {
			w.WriteHeader(expect.status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(expect.status)
		resource := httpx.Resource{ID: clientTestID, Path: httpx.PublicImages + clientTestID}
		if expect.many {
			_ = json.NewEncoder(w).Encode(httpx.ImagesResponse{Images: []httpx.Resource{resource, {ID: "second"}}})
			return
		}
		_ = json.NewEncoder(w).Encode(httpx.ResourceResponse{Resource: resource})
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server.URL, server.Client())

	zero := int64(0)
	zeroText := "0"
	expectations <- expectation{
		method: http.MethodPost, path: httpx.APIImages, status: http.StatusCreated, many: true,
		files: []multipartPart{
			{Field: "images", Filename: "first.png", Content: "first"},
			{Field: "images", Filename: "second.jpg", Content: "second"},
		},
		expiration: &zeroText,
	}
	created, err := client.CreateImages(context.Background(), []httpx.File{
		{Name: "first.png", Reader: strings.NewReader("first")},
		{Name: "second.jpg", Reader: strings.NewReader("second")},
	}, &zero)
	require.NoError(t, err)
	require.Equal(t, []string{clientTestID, "second"}, []string{created[0].ID, created[1].ID})

	positive := int64(86400)
	positiveText := "86400"
	expectations <- expectation{
		method: http.MethodPut, path: httpx.APIImages + "/" + clientTestID, status: http.StatusOK,
		files:      []multipartPart{{Field: "file", Filename: "replacement.gif", Content: "replacement"}},
		expiration: &positiveText,
	}
	updated, err := client.UpdateImage(context.Background(), clientTestID, httpx.File{
		Name: "replacement.gif", Reader: strings.NewReader("replacement"),
	}, &positive)
	require.NoError(t, err)
	require.Equal(t, clientTestID, updated.ID)

	expectations <- expectation{method: http.MethodDelete, path: httpx.APIImages + "/" + clientTestID, status: http.StatusNoContent}
	require.NoError(t, client.DeleteImage(context.Background(), clientTestID))
}

func TestClientPreservesContextCancellationError(t *testing.T) {
	t.Parallel()

	requestStarted := make(chan struct{})
	transportCanceled := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.Copy(io.Discard, r.Body)
		require.NoError(t, err)
		require.NoError(t, r.Body.Close())
		close(requestStarted)
		<-r.Context().Done()
		transportCanceled <- r.Context().Err()
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server.URL, server.Client())

	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := client.CreateWhiteboard(ctx, httpx.WhiteboardMarkdown, httpx.File{
			Name: "blocked.md", Reader: strings.NewReader("body"),
		}, nil)
		result <- err
	}()
	<-requestStarted
	cancel()
	select {
	case err := <-result:
		require.True(t, err == context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("client did not return after cancellation")
	}
	select {
	case err := <-transportCanceled:
		require.True(t, err == context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("cancellation did not reach transport")
	}
}

func TestClientPreservesContextDeadlineError(t *testing.T) {
	t.Parallel()

	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	client := newTestClient(t, "http://example.test", &http.Client{Transport: transport})
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()

	err := client.DeleteImage(ctx, clientTestID)
	require.True(t, err == context.DeadlineExceeded)
}

func TestClientPreservesContextErrorsWhileReadingResponse(t *testing.T) {
	t.Parallel()

	for _, contextErr := range []error{context.Canceled, context.DeadlineExceeded} {
		contextErr := contextErr
		t.Run(contextErr.Error(), func(t *testing.T) {
			t.Parallel()
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusNoContent,
					Header:     make(http.Header),
					Body:       readCloser{Reader: errorReader{err: contextErr}},
					Request:    request,
				}, nil
			})
			client := newTestClient(t, "http://example.test", &http.Client{Transport: transport})

			err := client.DeleteImage(context.Background(), clientTestID)
			require.True(t, err == contextErr)
		})
	}
}

func TestClientDecodesStableErrorsAndHidesUnknownBodies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   int
		body     string
		wantCode common.ErrorCode
		wantMsg  string
		hidden   string
	}{
		{
			name: "protocol error", status: http.StatusNotFound,
			body:     `{"error":{"code":"not_found","message":"resource not found"}}`,
			wantCode: common.CodeNotFound, wantMsg: "resource not found",
		},
		{
			name: "unknown body", status: http.StatusBadGateway,
			body: "upstream password=secret", wantCode: common.CodeInternal,
			wantMsg: "server returned an invalid error response", hidden: "password",
		},
		{
			name: "unknown code", status: http.StatusBadRequest,
			body:     `{"error":{"code":"private_code","message":"secret database path"}}`,
			wantCode: common.CodeInternal, wantMsg: "server returned an invalid error response", hidden: "database",
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(test.status)
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(server.Close)
			client := newTestClient(t, server.URL, server.Client())

			err := client.DeleteImage(context.Background(), clientTestID)
			var protocolErr *common.Error
			require.ErrorAs(t, err, &protocolErr)
			require.Equal(t, test.wantCode, protocolErr.Code)
			require.Equal(t, test.wantMsg, protocolErr.Message)
			if test.hidden != "" {
				require.NotContains(t, err.Error(), test.hidden)
			}
		})
	}
}

func TestClientRejectsMalformedAndOversizedResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "malformed", body: `{"resource":`},
		{name: "oversized", body: strings.Repeat("x", (1<<20)+1)},
		{name: "trailing JSON", body: `{"resource":{"id":"` + clientTestID + `"}} {}`},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, test.body)
			}))
			t.Cleanup(server.Close)
			client := newTestClient(t, server.URL, server.Client())

			_, err := client.CreateWhiteboard(context.Background(), httpx.WhiteboardMarkdown, httpx.File{
				Name: "board.md", Reader: strings.NewReader("body"),
			}, nil)
			require.Error(t, err)
			require.NotContains(t, err.Error(), test.body)
		})
	}
}

func TestClientAcceptsResponseAtOneMiBLimit(t *testing.T) {
	t.Parallel()

	body := `{"resource":{"id":"` + clientTestID + `"}}`
	body += strings.Repeat(" ", (1<<20)-len(body))
	require.Len(t, body, 1<<20)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	client := newTestClient(t, server.URL, server.Client())

	resource, err := client.CreateWhiteboard(context.Background(), httpx.WhiteboardMarkdown, httpx.File{
		Name: "board.md", Reader: strings.NewReader("body"),
	}, nil)
	require.NoError(t, err)
	require.Equal(t, clientTestID, resource.ID)
}

func TestClientPublicURLAcceptsOnlySafeSameOriginAbsolutePaths(t *testing.T) {
	t.Parallel()

	client := newTestClient(t, "https://example.test:8443", &http.Client{})
	for path, want := range map[string]string{
		"/whiteboards/markdown/" + clientTestID: "https://example.test:8443/whiteboards/markdown/" + clientTestID,
		"/images/abc%20def":                     "https://example.test:8443/images/abc%20def",
	} {
		got, err := client.PublicURL(path)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}

	invalid := []string{
		"", "images/id", "//evil.test/images/id", "https://evil.test/images/id",
		"/../admin", "/images/../admin", "/images/%2e%2e/admin", "/images/%2E%2E/admin",
		"/images/id?secret=1", "/images/id#fragment", "/images/id\\..\\admin", "/images/id%5c..%5cadmin",
	}
	for _, path := range invalid {
		got, err := client.PublicURL(path)
		require.Error(t, err, path)
		require.Empty(t, got)
	}
}

func TestClientStreamsMultipartWithoutReadingFileBeforeTransport(t *testing.T) {
	t.Parallel()

	reader := &transportGatedReader{allowed: make(chan struct{})}
	roundTripper := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		close(reader.allowed)
		_, err := io.Copy(io.Discard, request.Body)
		require.NoError(t, err)
		return &http.Response{
			StatusCode: http.StatusCreated,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(
				`{"resource":{"id":"` + clientTestID + `"}}`,
			)),
			Request: request,
		}, nil
	})
	client := newTestClient(t, "http://example.test", &http.Client{Transport: roundTripper})

	_, err := client.CreateWhiteboard(context.Background(), httpx.WhiteboardMarkdown, httpx.File{
		Name: "stream.md", Reader: reader,
	}, nil)
	require.NoError(t, err)
}

func TestClientClosesMultipartPipeAfterEarlyResponse(t *testing.T) {
	t.Parallel()

	requestBody := make(chan io.ReadCloser, 1)
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var firstByte [1]byte
		_, err := io.ReadFull(request.Body, firstByte[:])
		require.NoError(t, err)
		requestBody <- request.Body
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"error":{"code":"invalid_request","message":"rejected"}}`)),
			Request:    request,
		}, nil
	})
	client := newTestClient(t, "http://example.test", &http.Client{Transport: transport})
	result := make(chan error, 1)
	go func() {
		_, err := client.CreateWhiteboard(context.Background(), httpx.WhiteboardMarkdown, httpx.File{
			Name: "large.md", Reader: io.LimitReader(zeroReader{}, 1<<30),
		}, nil)
		result <- err
	}()

	select {
	case err := <-result:
		var protocolErr *common.Error
		require.ErrorAs(t, err, &protocolErr)
		require.Equal(t, common.CodeInvalidRequest, protocolErr.Code)
	case <-time.After(time.Second):
		t.Fatal("client stranded multipart writer after early response")
	}

	body := <-requestBody
	readResult := make(chan error, 1)
	go func() {
		var nextByte [1]byte
		read, err := body.Read(nextByte[:])
		if read != 0 {
			readResult <- errors.New("request body remained readable")
			return
		}
		readResult <- err
	}()
	select {
	case err := <-readResult:
		require.Error(t, err)
		require.NotEqual(t, "request body remained readable", err.Error())
	case <-time.After(time.Second):
		t.Fatal("request body remained open after early response")
	}
}

type multipartPart struct {
	Field    string
	Filename string
	Content  string
}

func readMultipartParts(t *testing.T, request *http.Request) []multipartPart {
	t.Helper()
	mediaType, params, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	reader := multipart.NewReader(request.Body, params["boundary"])
	var parts []multipartPart
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		content, err := io.ReadAll(part)
		require.NoError(t, err)
		parts = append(parts, multipartPart{Field: part.FormName(), Filename: part.FileName(), Content: string(content)})
	}
	return parts
}

func fileParts(parts []multipartPart) []multipartPart {
	files := make([]multipartPart, 0, len(parts))
	for _, part := range parts {
		if part.Field != "expires_in_seconds" {
			files = append(files, part)
		}
	}
	return files
}

func expirationPart(parts []multipartPart) *string {
	for _, part := range parts {
		if part.Field == "expires_in_seconds" {
			value := part.Content
			return &value
		}
	}
	return nil
}

func newTestClient(t *testing.T, server string, httpClient *http.Client) *httpx.Client {
	t.Helper()
	client, err := httpx.NewClient(httpx.ClientConfig{Server: server, HTTPClient: httpClient})
	require.NoError(t, err)
	return client
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type transportGatedReader struct {
	allowed chan struct{}
	read    bool
}

type zeroReader struct{}

type errorReader struct {
	err error
}

func (reader errorReader) Read([]byte) (int, error) {
	return 0, reader.err
}

type readCloser struct {
	io.Reader
}

func (readCloser) Close() error { return nil }

func (zeroReader) Read(destination []byte) (int, error) {
	for index := range destination {
		destination[index] = 0
	}
	return len(destination), nil
}

func (reader *transportGatedReader) Read(destination []byte) (int, error) {
	select {
	case <-reader.allowed:
	default:
		panic("file was read before the transport received the request")
	}
	if reader.read {
		return 0, io.EOF
	}
	reader.read = true
	return copy(destination, bytes.Repeat([]byte("x"), 128)), nil
}
