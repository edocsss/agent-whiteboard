package integration

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProcessHealth(t *testing.T) {
	server := startServer(t)
	requireStatus(t, server.URL+"/healthz", http.StatusOK)
	requireStatus(t, server.URL+"/readyz", http.StatusOK)
}

func TestProcessIgnoresInheritedConfiguration(t *testing.T) {
	t.Setenv("AGENT_WHITEBOARD_SHUTDOWN_TIMEOUT", "invalid")
	t.Setenv("agent_whiteboard_max_whiteboard_bytes", "invalid")

	server := startServer(t)
	requireStatus(t, server.URL+"/readyz", http.StatusOK)
	for _, entry := range server.env {
		key, _, _ := strings.Cut(entry, "=")
		require.False(t, strings.HasPrefix(strings.ToUpper(key), "AGENT_WHITEBOARD_"), entry)
	}
}

func TestServerLogWriterCapturesChunkedLogAndTail(t *testing.T) {
	output := &lockedBuffer{}
	logs := make(chan serverLog, 1)
	writer := &serverLogWriter{output: output, logs: logs}
	writes := []string{
		`{"level":"INFO","msg":"server`,
		` listening","address":"127.0.0.1:1234","url":"http://127.0.0.1:1234"}` + "\npartial",
		" tail without newline",
	}
	for _, chunk := range writes {
		_, err := writer.Write([]byte(chunk))
		require.NoError(t, err)
	}

	require.Equal(t, strings.Join(writes, ""), output.String())
	select {
	case entry := <-logs:
		require.Equal(t, "server listening", entry.Message)
		require.Equal(t, "127.0.0.1:1234", entry.Address)
	case <-time.After(time.Second):
		require.FailNow(t, "structured listening log was not emitted")
	}
}
