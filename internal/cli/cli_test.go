package cli

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/edocsss/agent-whiteboard/internal/cli/mocks"
	httpx "github.com/edocsss/agent-whiteboard/internal/http"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestClientSettingsPrecedence(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		args    []string
		server  string
		timeout time.Duration
	}{
		{name: "defaults", server: "http://127.0.0.1:8567", timeout: 30 * time.Second},
		{name: "environment", env: map[string]string{"AGENT_WHITEBOARD_SERVER": "https://env.test", "AGENT_WHITEBOARD_TIMEOUT": "9s"}, server: "https://env.test", timeout: 9 * time.Second},
		{name: "flags", env: map[string]string{"AGENT_WHITEBOARD_SERVER": "https://env.test", "AGENT_WHITEBOARD_TIMEOUT": "9s"}, args: []string{"--server", "https://flag.test", "--timeout", "7s"}, server: "https://flag.test", timeout: 7 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var got httpx.ClientConfig
			client := mocks.NewMockClient(t)
			root, err := NewRoot(Dependencies{
				Stdout: io.Discard, Stderr: io.Discard, Getenv: mapGetenv(test.env),
				NewClient: func(config httpx.ClientConfig) (Client, error) { got = config; return client, nil },
			})
			require.NoError(t, err)
			client.EXPECT().DeleteImage(mock.Anything, "abc").Return(nil).Once()
			root.SetArgs(append(test.args, "image", "delete", "abc"))
			require.NoError(t, root.ExecuteContext(context.Background()))
			require.Equal(t, test.server, got.Server)
			require.NotNil(t, got.HTTPClient)
			require.Equal(t, test.timeout, got.HTTPClient.Timeout)
		})
	}
}

func TestServerSettingsPrecedence(t *testing.T) {
	defaults := resolvedServerSettings{
		host: "127.0.0.1", port: 8567, storage: defaultStoragePath(),
		cleanupInterval: 15 * time.Minute, defaultExpiration: 86400,
		shutdownTimeout: 10 * time.Second, logMode: "console",
		maxWhiteboardBytes: 10 << 20, maxImageBytes: 25 << 20, maxImageRequestBytes: 100 << 20,
	}
	environment := resolvedServerSettings{
		host: "env.host", port: 9001, storage: "/env/storage",
		cleanupInterval: time.Minute, defaultExpiration: 42,
		shutdownTimeout: 2 * time.Second, logMode: "json",
		maxWhiteboardBytes: 11, maxImageBytes: 12, maxImageRequestBytes: 13,
	}
	flags := resolvedServerSettings{
		host: "flag.host", port: 9002, storage: "/flag/storage",
		cleanupInterval: 2 * time.Minute, defaultExpiration: 43,
		shutdownTimeout: 3 * time.Second, logMode: "console",
		maxWhiteboardBytes: 21, maxImageBytes: 22, maxImageRequestBytes: 23,
	}
	env := map[string]string{
		"AGENT_WHITEBOARD_HOST":                    environment.host,
		"AGENT_WHITEBOARD_PORT":                    "9001",
		"AGENT_WHITEBOARD_STORAGE":                 environment.storage,
		"AGENT_WHITEBOARD_CLEANUP_INTERVAL":        "1m",
		"AGENT_WHITEBOARD_DEFAULT_EXPIRES_IN":      "42",
		"AGENT_WHITEBOARD_SHUTDOWN_TIMEOUT":        "2s",
		"AGENT_WHITEBOARD_LOG_MODE":                environment.logMode,
		"AGENT_WHITEBOARD_MAX_WHITEBOARD_BYTES":    "11",
		"AGENT_WHITEBOARD_MAX_IMAGE_BYTES":         "12",
		"AGENT_WHITEBOARD_MAX_IMAGE_REQUEST_BYTES": "13",
	}
	flagArgs := []string{
		"--host", flags.host, "--port", "9002", "--storage", flags.storage,
		"--cleanup-interval", "2m", "--default-expires-in", "43",
		"--shutdown-timeout", "3s", "--log-mode", flags.logMode,
		"--max-whiteboard-bytes", "21", "--max-image-bytes", "22", "--max-image-request-bytes", "23",
	}

	for _, test := range []struct {
		name string
		env  map[string]string
		args []string
		want resolvedServerSettings
	}{
		{name: "defaults", want: defaults},
		{name: "environment", env: env, want: environment},
		{name: "flags", env: env, args: flagArgs, want: flags},
	} {
		t.Run(test.name, func(t *testing.T) {
			root, err := NewRoot(Dependencies{Stdout: io.Discard, Stderr: io.Discard, Getenv: mapGetenv(test.env), NewClient: unusedClient})
			require.NoError(t, err)
			root.SetArgs(append([]string{"serve"}, test.args...))
			err = root.ExecuteContext(context.Background())
			var boundary *serveNotImplementedError
			require.ErrorAs(t, err, &boundary)
			require.Equal(t, test.want, boundary.settings)
		})
	}
}

func TestApprovedClientCommands(t *testing.T) {
	dir := t.TempDir()
	first := writeFixture(t, dir, "first.md", "first-content")
	second := writeFixture(t, dir, "second.html", "second-content")

	tests := []struct {
		name   string
		args   []string
		expect func(*mocks.MockClient, *os.File)
	}{
		{name: "create markdown", args: []string{"create", "markdown", first, "--expires-in", "5"}, expect: expectCreateWhiteboard(httpx.WhiteboardMarkdown, "first.md", "first-content", int64Pointer(5))},
		{name: "create html", args: []string{"create", "html", second}, expect: expectCreateWhiteboard(httpx.WhiteboardHTML, "second.html", "second-content", nil)},
		{name: "update markdown", args: []string{"update", "markdown", "abc", first}, expect: expectUpdateWhiteboard(httpx.WhiteboardMarkdown, "abc", "first.md", "first-content")},
		{name: "update html", args: []string{"update", "html", "abc", second}, expect: expectUpdateWhiteboard(httpx.WhiteboardHTML, "abc", "second.html", "second-content")},
		{name: "delete markdown", args: []string{"delete", "markdown", "abc"}, expect: func(client *mocks.MockClient, _ *os.File) {
			client.EXPECT().DeleteWhiteboard(mock.Anything, httpx.WhiteboardMarkdown, "abc").Return(nil).Once()
		}},
		{name: "delete html", args: []string{"delete", "html", "abc"}, expect: func(client *mocks.MockClient, _ *os.File) {
			client.EXPECT().DeleteWhiteboard(mock.Anything, httpx.WhiteboardHTML, "abc").Return(nil).Once()
		}},
		{name: "image update", args: []string{"image", "update", "abc", first}, expect: func(client *mocks.MockClient, captured *os.File) {
			client.EXPECT().UpdateImage(mock.Anything, "abc", mock.Anything, (*int64)(nil)).RunAndReturn(func(_ context.Context, _ string, file httpx.File, _ *int64) (httpx.Resource, error) {
				verifyFile(t, file, "first.md", "first-content", captured)
				return resource("abc", "/images/abc", nil), nil
			}).Once()
		}},
		{name: "image delete", args: []string{"image", "delete", "abc"}, expect: func(client *mocks.MockClient, _ *os.File) {
			client.EXPECT().DeleteImage(mock.Anything, "abc").Return(nil).Once()
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := mocks.NewMockClient(t)
			var captured *os.File
			test.expect(client, captured)
			client.EXPECT().PublicURL(mock.Anything).RunAndReturn(func(path string) (string, error) { return "https://example.test" + path, nil }).Maybe()
			root := mustRoot(t, client, nil, io.Discard, io.Discard)
			root.SetArgs(test.args)
			require.NoError(t, root.ExecuteContext(context.Background()))
		})
	}

	t.Run("image upload preserves order and closes every handle", func(t *testing.T) {
		client := mocks.NewMockClient(t)
		var opened []*os.File
		client.EXPECT().CreateImages(mock.Anything, mock.Anything, int64Pointer(0)).RunAndReturn(func(_ context.Context, files []httpx.File, _ *int64) ([]httpx.Resource, error) {
			require.Len(t, files, 2)
			for index, want := range []struct{ name, content string }{{"first.md", "first-content"}, {"second.html", "second-content"}} {
				require.Equal(t, want.name, files[index].Name)
				opened = append(opened, files[index].Reader.(*os.File))
				content, err := io.ReadAll(files[index].Reader)
				require.NoError(t, err)
				require.Equal(t, want.content, string(content))
			}
			return []httpx.Resource{resource("one", "/images/one", nil), resource("two", "/images/two", nil)}, nil
		}).Once()
		client.EXPECT().PublicURL(mock.Anything).RunAndReturn(func(path string) (string, error) { return "https://example.test" + path, nil }).Twice()
		root := mustRoot(t, client, nil, io.Discard, io.Discard)
		root.SetArgs([]string{"image", "upload", first, second, "--expires-in", "0"})
		require.NoError(t, root.ExecuteContext(context.Background()))
		for _, file := range opened {
			_, err := file.Read(make([]byte, 1))
			require.ErrorIs(t, err, os.ErrClosed)
		}
	})
}

func TestClientTimeoutUsesCommandContext(t *testing.T) {
	client := mocks.NewMockClient(t)
	parentKey := struct{}{}
	parent := context.WithValue(context.Background(), parentKey, "present")
	client.EXPECT().DeleteImage(mock.Anything, "abc").RunAndReturn(func(ctx context.Context, _ string) error {
		require.Equal(t, "present", ctx.Value(parentKey))
		_, hasDeadline := ctx.Deadline()
		require.True(t, hasDeadline)
		<-ctx.Done()
		return ctx.Err()
	}).Once()
	root := mustRoot(t, client, nil, io.Discard, io.Discard)
	root.SetArgs([]string{"--timeout", "5ms", "image", "delete", "abc"})
	err := root.ExecuteContext(parent)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.EqualError(t, err, "request timed out")
}

func TestValidationHappensBeforeClientCreation(t *testing.T) {
	dir := t.TempDir()
	directory := filepath.Join(dir, "directory")
	require.NoError(t, os.Mkdir(directory, 0o755))

	tests := []struct {
		name string
		args []string
	}{
		{name: "invalid server", args: []string{"--server", "localhost:8567", "image", "delete", "abc"}},
		{name: "invalid timeout", args: []string{"--timeout", "zero", "image", "delete", "abc"}},
		{name: "negative timeout", args: []string{"--timeout", "-1s", "image", "delete", "abc"}},
		{name: "negative expiration", args: []string{"create", "markdown", "missing", "--expires-in", "-1"}},
		{name: "missing file", args: []string{"create", "markdown", filepath.Join(dir, "missing")}},
		{name: "directory as file", args: []string{"create", "html", directory}},
		{name: "zero image files", args: []string{"image", "upload"}},
		{name: "unexpected positionals", args: []string{"image", "delete", "abc", "extra"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			root, err := NewRoot(Dependencies{Stdout: io.Discard, Stderr: io.Discard, Getenv: mapGetenv(nil), NewClient: func(httpx.ClientConfig) (Client, error) { calls++; return mocks.NewMockClient(t), nil }})
			require.NoError(t, err)
			root.SetArgs(test.args)
			require.Error(t, root.ExecuteContext(context.Background()))
			require.Zero(t, calls)
		})
	}
}

func mustRoot(t *testing.T, client Client, getenv func(string) string, stdout, stderr io.Writer) interfaceRoot {
	t.Helper()
	if getenv == nil {
		getenv = mapGetenv(nil)
	}
	root, err := NewRoot(Dependencies{Stdout: stdout, Stderr: stderr, Getenv: getenv, NewClient: func(httpx.ClientConfig) (Client, error) { return client, nil }})
	require.NoError(t, err)
	return root
}

type interfaceRoot interface {
	SetArgs([]string)
	ExecuteContext(context.Context) error
}

func expectCreateWhiteboard(kind httpx.WhiteboardKind, name, content string, expires *int64) func(*mocks.MockClient, *os.File) {
	return func(client *mocks.MockClient, _ *os.File) {
		client.EXPECT().CreateWhiteboard(mock.Anything, kind, mock.Anything, expires).RunAndReturn(func(_ context.Context, _ httpx.WhiteboardKind, file httpx.File, _ *int64) (httpx.Resource, error) {
			got, err := io.ReadAll(file.Reader)
			if err != nil || file.Name != name || string(got) != content {
				return httpx.Resource{}, errors.New("unexpected file")
			}
			return resource("abc", "/whiteboards/"+string(kind)+"/abc", nil), nil
		}).Once()
	}
}

func expectUpdateWhiteboard(kind httpx.WhiteboardKind, id, name, content string) func(*mocks.MockClient, *os.File) {
	return func(client *mocks.MockClient, _ *os.File) {
		client.EXPECT().UpdateWhiteboard(mock.Anything, kind, id, mock.Anything, (*int64)(nil)).RunAndReturn(func(_ context.Context, _ httpx.WhiteboardKind, _ string, file httpx.File, _ *int64) (httpx.Resource, error) {
			got, err := io.ReadAll(file.Reader)
			if err != nil || file.Name != name || string(got) != content {
				return httpx.Resource{}, errors.New("unexpected file")
			}
			return resource(id, "/whiteboards/"+string(kind)+"/"+id, nil), nil
		}).Once()
	}
}

func verifyFile(t *testing.T, file httpx.File, name, content string, _ *os.File) {
	t.Helper()
	require.Equal(t, name, file.Name)
	got, err := io.ReadAll(file.Reader)
	require.NoError(t, err)
	require.Equal(t, content, string(got))
}

func writeFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func mapGetenv(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func unusedClient(httpx.ClientConfig) (Client, error) {
	return nil, errors.New("client must not be created")
}

func int64Pointer(value int64) *int64 { return &value }

func resource(id, path string, expires *int64) httpx.Resource {
	return httpx.Resource{ID: id, Path: path, ExpiresAt: expires, Permanent: expires == nil}
}
