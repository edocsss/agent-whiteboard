package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCIContract(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", ".."))
	workflowPath := filepath.Join(root, ".github", "workflows", "ci.yml")
	content, err := os.ReadFile(workflowPath)
	require.NoError(t, err)
	workflow := string(content)

	for _, required := range []string{
		"ubuntu-latest",
		"macos-latest",
		"1.25.x",
		"1.26.x",
		"actions/checkout@v6",
		"persist-credentials: false",
		"actions/setup-go@v6",
		"cache-dependency-path: go.sum",
		"go mod download",
		"go vet ./...",
		"go test ./...",
		"go test -race ./...",
		"go build -trimpath ./cmd/agent-whiteboard",
		"actions/setup-node@v6",
		"node-version: 24.x",
		"npm install --global corepack@0.35.0",
		"corepack prepare pnpm@11.4.0 --activate",
		"pnpm install --frozen-lockfile",
		"pnpm store path --silent",
		"actions/cache@v5",
		"~/.cache/ms-playwright",
		"pnpm test",
		"pnpm run check:assets",
		"pnpm exec playwright install --with-deps chromium",
		"pnpm run test:browser",
		"git diff --exit-code",
	} {
		require.Contains(t, workflow, required, "CI workflow must contain %q", required)
	}

	require.GreaterOrEqual(t, strings.Count(workflow, "timeout-minutes:"), 2,
		"every CI job must have an explicit timeout")
	require.Equal(t, 2, strings.Count(workflow, "uses: actions/checkout@v6"),
		"both jobs must use the pinned checkout action")
	require.Equal(t, 2, strings.Count(workflow, "persist-credentials: false"),
		"both checkouts must disable credential persistence")
	require.Equal(t, 2, strings.Count(workflow, "uses: actions/setup-go@v6"),
		"both jobs must use the pinned Go setup action")
	require.Contains(t, workflow, "hashFiles('pnpm-lock.yaml')", "browser cache key must include the pnpm lock hash")
	require.Contains(t, workflow, "${{ runner.os }}-pnpm-playwright-", "browser cache key must include the runner OS")
	require.Contains(t, workflow, "${{ steps.pnpm-store.outputs.path }}", "cache path must use the resolved pnpm store output")
	browserJobIndex := strings.Index(workflow, "  browser-assets:")
	require.Positive(t, browserJobIndex, "workflow must define the browser-assets job after the Go job")
	goJob := workflow[:browserJobIndex]
	require.Contains(t, goJob, "strategy:\n      fail-fast: false\n      matrix:\n        os:",
		"Go operating systems must be entries in strategy.matrix")
	require.Contains(t, goJob, "        go-version:\n          - 1.25.x\n          - 1.26.x",
		"Go versions must be entries in strategy.matrix")
	require.Less(t, strings.Index(workflow, "npm install --global corepack@0.35.0"), strings.Index(workflow, "corepack enable"),
		"the pinned Corepack install must precede activation")
	require.Less(t, strings.Index(workflow, "corepack enable"), strings.Index(workflow, "corepack prepare pnpm@11.4.0 --activate"),
		"Corepack must be enabled before activating pinned pnpm")
	require.Less(t, strings.Index(workflow, "pnpm store path --silent"), strings.Index(workflow, "uses: actions/cache@v5"),
		"the pnpm store path must be resolved before restoring the cache")
	require.Less(t, strings.Index(workflow, "uses: actions/cache@v5"), strings.Index(workflow, "pnpm install --frozen-lockfile"),
		"dependency caches must be restored before package installation")
	require.Less(t, strings.Index(workflow, "pnpm exec playwright install --with-deps chromium"), strings.Index(workflow, "pnpm run test:browser"),
		"Chromium and its system dependencies must be installed before browser tests")
}
