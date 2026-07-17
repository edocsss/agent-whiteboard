//go:build darwin || linux

package integration

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestProcessSIGINT(t *testing.T) {
	assertGracefulSignal(t, os.Interrupt)
}

func TestProcessSIGTERM(t *testing.T) {
	assertGracefulSignal(t, syscall.SIGTERM)
}

func assertGracefulSignal(t *testing.T, signal os.Signal) {
	t.Helper()
	server := startServer(t)
	connection := holdMultipartRequest(t, server.Address)
	server.Signal(t, signal)
	requireUnavailable(t, server.URL+"/readyz")
	require.False(t, server.exited(), "server exited before its in-flight request was released")
	require.NoError(t, connection.Close())
	require.NoError(t, server.Wait(t))
	require.NotContains(t, strings.ToLower(server.Stderr()), "internal error")
}

func holdMultipartRequest(t *testing.T, address string) net.Conn {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, processTimeout)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = connection.Close()
	})
	require.NoError(t, connection.SetDeadline(time.Now().Add(2*processTimeout)))

	_, err = fmt.Fprintf(connection,
		"POST /api/v1/images HTTP/1.1\r\nHost: %s\r\nContent-Type: multipart/form-data; boundary=held\r\nContent-Length: 1024\r\nExpect: 100-continue\r\nConnection: close\r\n\r\n",
		address,
	)
	require.NoError(t, err)
	response, err := http.ReadResponse(bufio.NewReader(connection), &http.Request{Method: http.MethodPost})
	require.NoError(t, err)
	defer response.Body.Close()
	require.Equal(t, http.StatusContinue, response.StatusCode)
	return connection
}
