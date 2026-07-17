package whiteboard_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/edocsss/agent-whiteboard/internal/common"
	"github.com/edocsss/agent-whiteboard/internal/whiteboard"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/html"
)

const (
	testViewerCSS = "#agent-whiteboard-content{color:rgb(1,2,3)}"
	testViewerJS  = "globalThis.agentWhiteboardViewerLoaded=true;"
)

func TestNewViewerRejectsMissingAssets(t *testing.T) {
	tests := []struct {
		name   string
		config whiteboard.ViewerConfig
	}{
		{name: "nil assets"},
		{name: "nil CSS", config: whiteboard.ViewerConfig{JS: []byte(testViewerJS)}},
		{name: "empty CSS", config: whiteboard.ViewerConfig{CSS: []byte{}, JS: []byte(testViewerJS)}},
		{name: "nil JavaScript", config: whiteboard.ViewerConfig{CSS: []byte(testViewerCSS)}},
		{name: "empty JavaScript", config: whiteboard.ViewerConfig{CSS: []byte(testViewerCSS), JS: []byte{}}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viewer, err := whiteboard.NewViewer(tt.config)

			require.Nil(t, viewer)
			require.Error(t, err)
			require.True(t, common.HasCode(err, common.CodeInvalidRequest), "expected invalid_request, got %v", err)
		})
	}
}

func TestViewerCopiesAssetsAndRendersStandardsValidShell(t *testing.T) {
	css := []byte(testViewerCSS)
	js := []byte(testViewerJS)
	viewer, err := whiteboard.NewViewer(whiteboard.ViewerConfig{CSS: css, JS: js})
	require.NoError(t, err)

	copy(css, strings.Repeat("x", len(css)))
	copy(js, strings.Repeat("y", len(js)))
	source := "# Whiteboard\n</script><tag>&\u2028\u2029"
	var output bytes.Buffer
	require.NoError(t, viewer.Render(&output, whiteboard.Whiteboard{
		ID:     testWhiteboardID,
		Kind:   whiteboard.KindMarkdown,
		Source: []byte(source),
	}))

	document, err := html.Parse(bytes.NewReader(output.Bytes()))
	require.NoError(t, err)
	require.Equal(t, 1, countNodes(document, func(node *html.Node) bool {
		return node.Type == html.DoctypeNode && strings.EqualFold(node.Data, "html")
	}))
	require.Equal(t, 1, countElements(document, "html"))
	require.Equal(t, 1, countElements(document, "head"))
	require.Equal(t, 1, countElements(document, "body"))
	titles := findElements(document, "title", nil)
	require.Len(t, titles, 1)
	require.Equal(t, "Untitled whiteboard", textContent(titles[0]))

	robots := findElements(document, "meta", func(node *html.Node) bool {
		return attribute(node, "name") == "robots"
	})
	require.Len(t, robots, 1)
	require.Equal(t, "noindex, nofollow, noarchive", attribute(robots[0], "content"))

	styles := findElements(document, "style", nil)
	require.Len(t, styles, 1)
	require.Equal(t, testViewerCSS, textContent(styles[0]))
	require.Empty(t, attribute(styles[0], "href"))

	sourceScripts := findElements(document, "script", func(node *html.Node) bool {
		return attribute(node, "id") == "agent-whiteboard-source"
	})
	require.Len(t, sourceScripts, 1)
	require.Equal(t, "application/json", attribute(sourceScripts[0], "type"))
	var payload struct {
		Markdown string `json:"markdown"`
	}
	require.NoError(t, json.Unmarshal([]byte(textContent(sourceScripts[0])), &payload))
	require.Equal(t, source, payload.Markdown)

	scripts := findElements(document, "script", nil)
	require.Len(t, scripts, 2)
	require.Equal(t, testViewerJS, textContent(scripts[1]))
	for _, script := range scripts {
		require.Empty(t, attribute(script, "src"))
	}
	require.Empty(t, findElements(document, "link", func(node *html.Node) bool {
		return strings.EqualFold(attribute(node, "rel"), "stylesheet") || attribute(node, "href") != ""
	}))

	noscript := findElements(document, "noscript", nil)
	require.Len(t, noscript, 1)
	require.NotEmpty(t, strings.TrimSpace(textContent(noscript[0])))

	raw := output.String()
	require.Contains(t, raw, `\u003c/script\u003e`)
	require.Contains(t, raw, `\u003ctag\u003e\u0026`)
	require.Contains(t, raw, `\u2028`)
	require.Contains(t, raw, `\u2029`)
	require.NotContains(t, raw, source)
}

func countElements(root *html.Node, name string) int {
	return countNodes(root, func(node *html.Node) bool {
		return node.Type == html.ElementNode && node.Data == name
	})
}

func countNodes(root *html.Node, matches func(*html.Node) bool) int {
	count := 0
	walkNodes(root, func(node *html.Node) {
		if matches(node) {
			count++
		}
	})
	return count
}

func findElements(root *html.Node, name string, matches func(*html.Node) bool) []*html.Node {
	var elements []*html.Node
	walkNodes(root, func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == name && (matches == nil || matches(node)) {
			elements = append(elements, node)
		}
	})
	return elements
}

func walkNodes(root *html.Node, visit func(*html.Node)) {
	visit(root)
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		walkNodes(child, visit)
	}
}

func attribute(node *html.Node, name string) string {
	for _, attr := range node.Attr {
		if attr.Key == name {
			return attr.Val
		}
	}
	return ""
}

func textContent(node *html.Node) string {
	var content strings.Builder
	walkNodes(node, func(current *html.Node) {
		if current.Type == html.TextNode {
			content.WriteString(current.Data)
		}
	})
	return content.String()
}
