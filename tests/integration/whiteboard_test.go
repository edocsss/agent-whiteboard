package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMarkdownLifecycle(t *testing.T) {
	server := startServer(t)
	firstSource := "# First title\n\n`</script>` and unicode ✓\n"
	firstFile := writeFixture(t, "first.md", []byte(firstSource))
	created := runCLIResource(t, server, "--json", "create", "markdown", firstFile)
	require.Equal(t, 1, created.SchemaVersion)
	require.True(t, strings.HasPrefix(created.Resource.URL, server.URL+"/whiteboards/markdown/"))

	response, body := fetch(t, created.Resource.URL)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, "text/html; charset=utf-8", response.Header.Get("Content-Type"))
	require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
	require.Equal(t, "noindex, nofollow, noarchive", response.Header.Get("X-Robots-Tag"))
	require.Equal(t, "nosniff", response.Header.Get("X-Content-Type-Options"))
	require.Contains(t, body, `<meta name="robots" content="noindex, nofollow, noarchive">`)
	require.Contains(t, body, "<style>")
	require.Contains(t, body, "<script>")
	encodedSource, err := json.Marshal(map[string]string{"markdown": firstSource})
	require.NoError(t, err)
	require.Contains(t, body, string(encodedSource))
	assertNoExternalReferences(t, body)

	secondSource := "# Updated\n\nExact replacement.\n"
	secondFile := writeFixture(t, "second.md", []byte(secondSource))
	updated := runCLIResource(t, server, "--json", "update", "markdown", "--", created.Resource.ID, secondFile)
	require.Equal(t, created.Resource.URL, updated.Resource.URL)
	_, body = fetch(t, created.Resource.URL)
	encodedSource, err = json.Marshal(map[string]string{"markdown": secondSource})
	require.NoError(t, err)
	require.Contains(t, body, string(encodedSource))
	require.NotContains(t, body, string(mustJSON(t, map[string]string{"markdown": firstSource})))

	runCLIDelete(t, server, "--json", "delete", "markdown", "--", created.Resource.ID)
	response, body = fetch(t, created.Resource.URL)
	require.Equal(t, http.StatusNotFound, response.StatusCode)
	requireHTTPErrorCode(t, body, "not_found")
	ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
	defer cancel()
	stdout, stderr, err := server.RunCLI(ctx, "--json", "update", "markdown", "--", created.Resource.ID, secondFile)
	require.Error(t, err)
	require.Empty(t, stdout)
	requireJSONError(t, stderr, "not_found")
}

func TestHTMLLifecycleAndValidation(t *testing.T) {
	server := startServer(t)
	firstSource := []byte(`<!doctype html><html><head><style>body{color:#123}</style><script>window.inline=true</script></head><body><p>exact ✓</p></body></html>`)
	firstFile := writeFixture(t, "first.html", firstSource)
	created := runCLIResource(t, server, "--json", "create", "html", firstFile)
	require.True(t, strings.HasPrefix(created.Resource.URL, server.URL+"/whiteboards/html/"))

	response, body := fetch(t, created.Resource.URL)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.Equal(t, firstSource, []byte(body))
	require.Equal(t, "text/html; charset=utf-8", response.Header.Get("Content-Type"))
	require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
	require.Equal(t, "noindex, nofollow, noarchive", response.Header.Get("X-Robots-Tag"))
	require.Equal(t, "nosniff", response.Header.Get("X-Content-Type-Options"))

	secondSource := []byte(`<!doctype html><html><head><style>p{font-weight:bold}</style></head><body><script>window.updated=true</script><p>replacement</p></body></html>`)
	secondFile := writeFixture(t, "second.html", secondSource)
	updated := runCLIResource(t, server, "--json", "update", "html", "--", created.Resource.ID, secondFile)
	require.Equal(t, created.Resource.URL, updated.Resource.URL)
	_, body = fetch(t, created.Resource.URL)
	require.Equal(t, secondSource, []byte(body))

	runCLIDelete(t, server, "--json", "delete", "html", "--", created.Resource.ID)
	response, _ = fetch(t, created.Resource.URL)
	require.Equal(t, http.StatusNotFound, response.StatusCode)
	requireCategoryEmpty(t, server.Root, "whiteboards")

	invalid := []struct {
		name    string
		content []byte
	}{
		{name: "external script", content: []byte(`<!doctype html><html><head></head><body><script src="https://example.invalid/x.js"></script></body></html>`)},
		{name: "external stylesheet", content: []byte(`<!doctype html><html><head><link rel="stylesheet" href="https://example.invalid/x.css"></head><body></body></html>`)},
		{name: "missing doctype", content: []byte(`<html><head></head><body></body></html>`)},
		{name: "missing head", content: []byte(`<!doctype html><html><body></body></html>`)},
		{name: "invalid UTF-8", content: []byte{0xff, 0xfe}},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			path := writeFixture(t, "invalid.html", test.content)
			ctx, cancel := context.WithTimeout(context.Background(), integrationTimeout)
			defer cancel()
			stdout, stderr, err := server.RunCLI(ctx, "--json", "create", "html", path)
			require.Error(t, err)
			require.Empty(t, stdout)
			requireJSONError(t, stderr, "invalid_request")
			requireCategoryEmpty(t, server.Root, "whiteboards")
		})
	}
}

func assertNoExternalReferences(t *testing.T, body string) {
	t.Helper()
	require.NotRegexp(t, `(?i)(?:src|href)\s*=\s*["'](?:https?:)?//`, body)
	require.NotContains(t, strings.ToLower(body), "cdn.jsdelivr.net")
}

func requireHTTPErrorCode(t *testing.T, body, code string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal([]byte(body), &envelope), body)
	require.Equal(t, code, envelope.Error.Code)
}
