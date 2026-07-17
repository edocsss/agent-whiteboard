package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const serverTestTimeout = 2 * time.Second

func TestNewServerRejectsInvalidDependenciesAndConfiguration(t *testing.T) {
	application := newLifecycleApp(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var typedNilApp *App
	var typedNilLogger *slog.Logger
	var typedNilCloser *countingCloser
	var typedNilListener *net.TCPListener

	tests := []struct {
		name   string
		config ServerConfig
	}{
		{name: "nil app", config: validServerConfig(nil, logger)},
		{name: "typed nil app", config: validServerConfig(typedNilApp, logger)},
		{name: "zero app", config: validServerConfig(&App{}, logger)},
		{name: "nil logger", config: validServerConfig(application, nil)},
		{name: "typed nil logger", config: validServerConfig(application, typedNilLogger)},
		{name: "nil closer", config: withServerClosers(validServerConfig(application, logger), nil)},
		{name: "typed nil closer", config: withServerClosers(validServerConfig(application, logger), typedNilCloser)},
		{name: "typed nil listener", config: withServerListener(validServerConfig(application, logger), typedNilListener)},
		{name: "blank host", config: withServerHost(validServerConfig(application, logger), "")},
		{name: "whitespace host", config: withServerHost(validServerConfig(application, logger), "bad host")},
		{name: "host containing port", config: withServerHost(validServerConfig(application, logger), "127.0.0.1:8567")},
		{name: "negative port", config: withServerPort(validServerConfig(application, logger), -1)},
		{name: "port above maximum", config: withServerPort(validServerConfig(application, logger), 65536)},
		{name: "zero shutdown timeout", config: withServerShutdownTimeout(validServerConfig(application, logger), 0)},
		{name: "negative shutdown timeout", config: withServerShutdownTimeout(validServerConfig(application, logger), -time.Second)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, err := NewServer(test.config)

			require.Nil(t, server)
			require.Error(t, err)
		})
	}
}

func TestServerLogsBoundAddressBeforeReadinessAndServesRealRequests(t *testing.T) {
	application := newLifecycleApp(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	listener := listenLocal(t)
	loggerHandler := newListeningLogBarrier()
	closer := &countingCloser{}
	server := newTestServer(t, application, slog.New(loggerHandler), listener, time.Second, closer)
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := serveInBackground(server, ctx, listener)

	receive(t, loggerHandler.entered)
	require.Error(t, application.readiness.Ready(context.Background()), "readiness changed before the listening log completed")
	require.Equal(t, listener.Addr().String(), server.Address().String())
	require.Equal(t, "server listening", loggerHandler.record.Message)
	require.Equal(t, listener.Addr().String(), loggerHandler.attributes["address"])
	require.Equal(t, "http://"+listener.Addr().String(), loggerHandler.attributes["url"])
	close(loggerHandler.release)
	waitForReady(t, application)

	response, err := serverTestHTTPClient().Get("http://" + server.Address().String())
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusNoContent, response.StatusCode)

	cancel()
	require.NoError(t, receive(t, serveDone))
	require.Error(t, application.readiness.Ready(context.Background()))
	require.Equal(t, int32(1), closer.calls.Load())
}

func TestServerCancellationWhileStartupLogBlockedShutsDownWithoutWaitingForLogger(t *testing.T) {
	application := newLifecycleApp(http.NotFoundHandler())
	listener := listenLocal(t)
	loggerHandler := newListeningLogBarrier()
	closer := &countingCloser{}
	server := newTestServer(t, application, slog.New(loggerHandler), listener, time.Second, closer)
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := serveInBackground(server, ctx, listener)
	serveReturned := false
	t.Cleanup(func() {
		cancel()
		releaseListeningLog(loggerHandler)
		if !serveReturned {
			receive(t, serveDone)
		}
	})
	receive(t, loggerHandler.entered)

	cancel()
	select {
	case err := <-serveDone:
		serveReturned = true
		require.NoError(t, err)
	case <-time.After(serverTestTimeout):
		t.Fatal("serve cancellation waited for the blocked startup logger")
	}

	require.False(t, application.readiness.accepting.Load(), "server became ready after its lifetime context was canceled")
	require.Equal(t, int32(1), closer.calls.Load())
	requireListenerUnavailable(t, listener.Addr().String())
}

func TestServerFailureWhileStartupLogBlockedReturnsWithoutMarkingReady(t *testing.T) {
	serveFailure := errors.New("accept failed")
	failAccept := make(chan struct{})
	failureObserved := make(chan struct{})
	listener := &failingAcceptListener{
		Listener: listenLocal(t),
		fail:     failAccept,
		failed:   failureObserved,
		err:      serveFailure,
	}
	application := newLifecycleApp(http.NotFoundHandler())
	loggerHandler := newListeningLogBarrier()
	closer := &countingCloser{}
	server := newTestServer(t, application, slog.New(loggerHandler), listener, time.Second, closer)
	serveDone := serveInBackground(server, context.Background(), listener)
	serveReturned := false
	t.Cleanup(func() {
		releaseListeningLog(loggerHandler)
		if !serveReturned {
			receive(t, serveDone)
		}
	})
	receive(t, loggerHandler.entered)

	close(failAccept)
	receive(t, failureObserved)
	require.False(t, application.readiness.accepting.Load(), "server became ready while Serve had already failed")
	select {
	case err := <-serveDone:
		serveReturned = true
		require.ErrorIs(t, err, serveFailure)
	case <-time.After(serverTestTimeout):
		t.Fatal("Serve failure waited for the blocked startup logger")
	}

	require.False(t, application.readiness.accepting.Load(), "server became ready after Serve failed")
	require.Equal(t, int32(1), closer.calls.Load())
	requireListenerUnavailable(t, listener.Addr().String())
}

func TestServerContainsStartupLoggingPanicAndTerminatesLifecycle(t *testing.T) {
	application := newLifecycleApp(http.NotFoundHandler())
	listener := listenLocal(t)
	closer := &countingCloser{}
	server := newTestServer(t, application, slog.New(panickingLogHandler{}), listener, time.Second, closer)

	err := receive(t, serveInBackground(server, context.Background(), listener))

	require.ErrorContains(t, err, "server listening log handler panicked")
	require.NotContains(t, err.Error(), "private panic detail")
	require.False(t, application.readiness.accepting.Load())
	require.Equal(t, int32(1), closer.calls.Load())
	requireListenerUnavailable(t, listener.Addr().String())
}

func TestServerCancellationUsesFreshShutdownContextAndLetsInflightRequestFinish(t *testing.T) {
	requestEntered := make(chan struct{})
	shutdownStarted := make(chan struct{})
	readinessFalseAtShutdown := make(chan bool, 1)
	releaseRequest := make(chan struct{})
	requestContextLive := make(chan bool, 1)
	application := newLifecycleApp(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		close(requestEntered)
		<-shutdownStarted
		select {
		case <-request.Context().Done():
			requestContextLive <- false
		default:
			requestContextLive <- true
		}
		<-releaseRequest
		_, _ = response.Write([]byte("finished"))
	}))
	baseListener := listenLocal(t)
	listener := &closeObservingListener{
		Listener: baseListener,
		onClose: func() {
			readinessFalseAtShutdown <- application.readiness.Ready(context.Background()) != nil
			close(shutdownStarted)
		},
	}
	closer := &countingCloser{}
	server := newTestServer(t, application, discardLogger(), listener, time.Second, closer)
	shutdownContexts := make(chan shutdownContextRequest, 1)
	server.newShutdownContext = func(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
		shutdownContexts <- shutdownContextRequest{parent: parent, timeout: timeout}
		return context.WithTimeout(parent, timeout)
	}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := serveInBackground(server, ctx, listener)
	waitForReady(t, application)
	responseDone := getInBackground("http://" + listener.Addr().String())
	receive(t, requestEntered)

	cancel()
	shutdownContext := receive(t, shutdownContexts)
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.True(t, shutdownContext.parent == context.Background(), "shutdown context was not rooted at context.Background")
	require.NoError(t, shutdownContext.parent.Err(), "fresh shutdown parent was already canceled")
	require.Equal(t, time.Second, shutdownContext.timeout)
	require.True(t, receive(t, readinessFalseAtShutdown), "listener closed before readiness became false")
	require.True(t, receive(t, requestContextLive), "request context was canceled instead of using a fresh graceful-shutdown context")
	close(releaseRequest)

	responseResult := receive(t, responseDone)
	require.NoError(t, responseResult.err)
	require.Equal(t, "finished", responseResult.body)
	require.NoError(t, receive(t, serveDone))
	require.Equal(t, int32(1), closer.calls.Load())
}

func TestServerForceClosesHandlerAfterShutdownDeadline(t *testing.T) {
	requestEntered := make(chan struct{})
	requestCanceled := make(chan struct{})
	application := newLifecycleApp(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(requestEntered)
		<-request.Context().Done()
		close(requestCanceled)
	}))
	listener := listenLocal(t)
	closer := &countingCloser{}
	server := newTestServer(t, application, discardLogger(), listener, 40*time.Millisecond, closer)
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := serveInBackground(server, ctx, listener)
	waitForReady(t, application)
	responseDone := getInBackground("http://" + listener.Addr().String())
	receive(t, requestEntered)

	cancel()
	serveErr := receive(t, serveDone)
	require.ErrorIs(t, serveErr, context.DeadlineExceeded)
	receive(t, requestCanceled)
	responseResult := receive(t, responseDone)
	require.Error(t, responseResult.err)
	require.Equal(t, int32(1), closer.calls.Load())
}

func TestServerDirectShutdownNormalizesErrServerClosedAndLeavesClosersOpen(t *testing.T) {
	application := newLifecycleApp(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	listener := listenLocal(t)
	closer := &countingCloser{}
	server := newTestServer(t, application, discardLogger(), listener, time.Second, closer)
	serveDone := serveInBackground(server, context.Background(), listener)
	waitForReady(t, application)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), serverTestTimeout)
	defer cancelShutdown()
	require.NoError(t, server.Shutdown(shutdownCtx))
	require.NoError(t, receive(t, serveDone))
	require.Error(t, application.readiness.Ready(context.Background()))
	require.Zero(t, closer.calls.Load(), "Shutdown closed application resources")

	require.NoError(t, server.Close())
	require.Equal(t, int32(1), closer.calls.Load())
}

func TestServerDirectShutdownDuringStartupNeverMarksReadyOrClosesStores(t *testing.T) {
	application := newLifecycleApp(http.NotFoundHandler())
	listener := listenLocal(t)
	loggerHandler := newListeningLogBarrier()
	closer := &countingCloser{}
	server := newTestServer(t, application, slog.New(loggerHandler), listener, time.Second, closer)
	serveDone := serveInBackground(server, context.Background(), listener)
	receive(t, loggerHandler.entered)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), serverTestTimeout)
	defer cancelShutdown()
	require.NoError(t, server.Shutdown(shutdownCtx))
	close(loggerHandler.release)

	require.NoError(t, receive(t, serveDone))
	require.Error(t, application.readiness.Ready(context.Background()))
	require.Zero(t, closer.calls.Load(), "direct Shutdown transferred store ownership to Serve")
	require.NoError(t, server.Close())
}

func TestServerCloseIsIdempotentAndReturnsSameJoinedError(t *testing.T) {
	firstError := errors.New("first close failed")
	secondError := errors.New("second close failed")
	first := &countingCloser{err: firstError}
	second := &countingCloser{err: secondError}
	server := newTestServer(t, newLifecycleApp(http.NotFoundHandler()), discardLogger(), nil, time.Second, first, second)

	firstResult := server.Close()
	secondResult := server.Close()

	require.ErrorIs(t, firstResult, firstError)
	require.ErrorIs(t, firstResult, secondError)
	require.True(t, firstResult == secondResult, "Close returned a newly-created error on repetition")
	require.Equal(t, int32(1), first.calls.Load())
	require.Equal(t, int32(1), second.calls.Load())
}

func TestServerConcurrentCloseCallsPublishSameJoinedError(t *testing.T) {
	closeFailure := errors.New("close failed")
	closer := &blockingCloser{
		entered: make(chan struct{}),
		release: make(chan struct{}),
		err:     closeFailure,
	}
	server := newTestServer(t, newLifecycleApp(http.NotFoundHandler()), discardLogger(), nil, time.Second, closer)
	const callers = 16
	start := make(chan struct{})
	results := make(chan error, callers)
	for range callers {
		go func() {
			<-start
			results <- server.Close()
		}()
	}

	close(start)
	receive(t, closer.entered)
	close(closer.release)
	firstResult := receive(t, results)
	require.ErrorIs(t, firstResult, closeFailure)
	for range callers - 1 {
		result := receive(t, results)
		require.True(t, result == firstResult, "concurrent Close returned a different stored error")
	}
	require.Equal(t, int32(1), closer.calls.Load())
}

func TestServerDirectShutdownRacingCancellationDoesNotDeadlockOrDoubleClose(t *testing.T) {
	requestEntered := make(chan struct{})
	releaseRequest := make(chan struct{})
	application := newLifecycleApp(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		close(requestEntered)
		<-releaseRequest
		response.WriteHeader(http.StatusNoContent)
	}))
	listenerCloseEntered := make(chan struct{})
	releaseListenerClose := make(chan struct{})
	listener := &closeObservingListener{
		Listener: listenLocal(t),
		onClose: func() {
			close(listenerCloseEntered)
			<-releaseListenerClose
		},
	}
	closer := &countingCloser{}
	server := newTestServer(t, application, discardLogger(), listener, time.Second, closer)
	serveCtx, cancelServe := context.WithCancel(context.Background())
	serveDone := serveInBackground(server, serveCtx, listener)
	waitForReady(t, application)
	responseDone := getInBackground("http://" + listener.Addr().String())
	receive(t, requestEntered)

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), serverTestTimeout)
	defer cancelShutdown()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- server.Shutdown(shutdownCtx) }()
	receive(t, listenerCloseEntered)
	cancelServe()
	close(releaseListenerClose)
	close(releaseRequest)

	require.NoError(t, receive(t, shutdownDone))
	require.NoError(t, receive(t, serveDone))
	responseResult := receive(t, responseDone)
	require.NoError(t, responseResult.err)
	require.False(t, application.readiness.accepting.Load())
	require.Equal(t, int32(1), closer.calls.Load())
}

func TestServerRejectsSimultaneousAndReusedServeCallsWithoutConsumingRejectedListeners(t *testing.T) {
	application := newLifecycleApp(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	firstListener := listenLocal(t)
	server := newTestServer(t, application, discardLogger(), nil, time.Second)
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	firstDone := serveInBackground(server, firstCtx, firstListener)
	waitForReady(t, application)

	secondListener := listenLocal(t)
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	cancelSecond()
	secondErr := receive(t, serveInBackground(server, secondCtx, secondListener))
	require.Error(t, secondErr)
	require.NoError(t, secondListener.Close(), "a rejected Serve call consumed its listener")

	cancelFirst()
	require.NoError(t, receive(t, firstDone))
	thirdListener := listenLocal(t)
	thirdCtx, cancelThird := context.WithCancel(context.Background())
	cancelThird()
	thirdErr := receive(t, serveInBackground(server, thirdCtx, thirdListener))
	require.Error(t, thirdErr)
	require.NoError(t, thirdListener.Close(), "a reused Serve call consumed its listener")
}

func TestServerServeUsesExplicitListenerWithoutConsumingConfiguredListener(t *testing.T) {
	configuredListener := listenLocal(t)
	explicitListener := listenLocal(t)
	application := newLifecycleApp(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	server := newTestServer(t, application, discardLogger(), configuredListener, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := serveInBackground(server, ctx, explicitListener)
	waitForReady(t, application)

	require.Equal(t, explicitListener.Addr().String(), server.Address().String())
	cancel()
	require.NoError(t, receive(t, serveDone))
	require.NoError(t, configuredListener.Close(), "Serve consumed the configured listener instead of its explicit listener")
}

func TestServerListenAndServeUsesConfiguredListenerAndReportsActualAddress(t *testing.T) {
	listener := listenLocal(t)
	application := newLifecycleApp(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	server := newTestServer(t, application, discardLogger(), listener, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ListenAndServe(ctx) }()
	waitForReady(t, application)

	require.NotNil(t, server.Address())
	require.Equal(t, listener.Addr().String(), server.Address().String())
	host, port, err := net.SplitHostPort(server.Address().String())
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1", host)
	require.NotEqual(t, "0", port)

	cancel()
	require.NoError(t, receive(t, serveDone))
}

func TestServerListenAndServeBindsConfiguredEphemeralPort(t *testing.T) {
	application := newLifecycleApp(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	server := newTestServer(t, application, discardLogger(), nil, time.Second)
	require.Nil(t, server.Address())
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.ListenAndServe(ctx) }()
	waitForReady(t, application)

	address := server.Address()
	require.NotNil(t, address)
	_, port, err := net.SplitHostPort(address.String())
	require.NoError(t, err)
	require.NotEqual(t, "0", port)
	response, err := serverTestHTTPClient().Get("http://" + address.String())
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())

	cancel()
	require.NoError(t, receive(t, serveDone))
}

func TestServerServeRejectsNilListeners(t *testing.T) {
	server := newTestServer(t, newLifecycleApp(http.NotFoundHandler()), discardLogger(), nil, time.Second)
	var typedNilListener *net.TCPListener

	require.Error(t, server.Serve(context.Background(), nil))
	require.Error(t, server.Serve(context.Background(), typedNilListener))
}

func TestServerCancellationJoinsResourceCloseErrors(t *testing.T) {
	closeError := errors.New("store close failed")
	closer := &countingCloser{err: closeError}
	listener := listenLocal(t)
	application := newLifecycleApp(http.NotFoundHandler())
	server := newTestServer(t, application, discardLogger(), listener, time.Second, closer)
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := serveInBackground(server, ctx, listener)
	waitForReady(t, application)

	cancel()
	require.ErrorIs(t, receive(t, serveDone), closeError)
	require.Equal(t, int32(1), closer.calls.Load())
}

type countingCloser struct {
	calls atomic.Int32
	err   error
}

type blockingCloser struct {
	calls   atomic.Int32
	entered chan struct{}
	release chan struct{}
	err     error
	once    sync.Once
}

func (closer *blockingCloser) Close() error {
	closer.calls.Add(1)
	closer.once.Do(func() { close(closer.entered) })
	<-closer.release
	return closer.err
}

type failingAcceptListener struct {
	net.Listener
	fail   <-chan struct{}
	failed chan<- struct{}
	err    error
	once   sync.Once
}

func (listener *failingAcceptListener) Accept() (net.Conn, error) {
	<-listener.fail
	listener.once.Do(func() { close(listener.failed) })
	return nil, listener.err
}

type panickingLogHandler struct{}

func (panickingLogHandler) Enabled(context.Context, slog.Level) bool { return true }

func (panickingLogHandler) Handle(context.Context, slog.Record) error {
	panic("private panic detail")
}

func (handler panickingLogHandler) WithAttrs([]slog.Attr) slog.Handler { return handler }

func (handler panickingLogHandler) WithGroup(string) slog.Handler { return handler }

func (closer *countingCloser) Close() error {
	closer.calls.Add(1)
	return closer.err
}

type closeObservingListener struct {
	net.Listener
	once    sync.Once
	onClose func()
}

func (listener *closeObservingListener) Close() error {
	listener.once.Do(listener.onClose)
	return listener.Listener.Close()
}

type listeningLogBarrier struct {
	entered    chan struct{}
	release    chan struct{}
	once       sync.Once
	record     slog.Record
	attributes map[string]any
}

func newListeningLogBarrier() *listeningLogBarrier {
	return &listeningLogBarrier{
		entered:    make(chan struct{}),
		release:    make(chan struct{}),
		attributes: make(map[string]any),
	}
}

func releaseListeningLog(handler *listeningLogBarrier) {
	select {
	case <-handler.release:
	default:
		close(handler.release)
	}
}

func (handler *listeningLogBarrier) Enabled(context.Context, slog.Level) bool { return true }

func (handler *listeningLogBarrier) Handle(_ context.Context, record slog.Record) error {
	if record.Message != "server listening" {
		return nil
	}
	handler.once.Do(func() {
		handler.record = record.Clone()
		record.Attrs(func(attribute slog.Attr) bool {
			handler.attributes[attribute.Key] = attribute.Value.Any()
			return true
		})
		close(handler.entered)
		<-handler.release
	})
	return nil
}

func (handler *listeningLogBarrier) WithAttrs([]slog.Attr) slog.Handler { return handler }

func (handler *listeningLogBarrier) WithGroup(string) slog.Handler { return handler }

type responseResult struct {
	body string
	err  error
}

type shutdownContextRequest struct {
	parent  context.Context
	timeout time.Duration
}

func getInBackground(url string) <-chan responseResult {
	done := make(chan responseResult, 1)
	go func() {
		response, err := serverTestHTTPClient().Get(url)
		if err != nil {
			done <- responseResult{err: err}
			return
		}
		defer response.Body.Close()
		body, readErr := io.ReadAll(response.Body)
		done <- responseResult{body: string(body), err: readErr}
	}()
	return done
}

func serverTestHTTPClient() *http.Client {
	return &http.Client{Timeout: serverTestTimeout}
}

func newLifecycleApp(handler http.Handler) *App {
	return &App{handler: handler, readiness: &readiness{}}
}

func validServerConfig(application *App, logger *slog.Logger) ServerConfig {
	return ServerConfig{
		App:             application,
		Logger:          logger,
		Host:            "127.0.0.1",
		Port:            0,
		ShutdownTimeout: time.Second,
	}
}

func withServerClosers(config ServerConfig, closers ...io.Closer) ServerConfig {
	config.Closers = closers
	return config
}

func withServerListener(config ServerConfig, listener net.Listener) ServerConfig {
	config.Listener = listener
	return config
}

func withServerHost(config ServerConfig, host string) ServerConfig {
	config.Host = host
	return config
}

func withServerPort(config ServerConfig, port int) ServerConfig {
	config.Port = port
	return config
}

func withServerShutdownTimeout(config ServerConfig, timeout time.Duration) ServerConfig {
	config.ShutdownTimeout = timeout
	return config
}

func newTestServer(
	t *testing.T,
	application *App,
	logger *slog.Logger,
	listener net.Listener,
	shutdownTimeout time.Duration,
	closers ...io.Closer,
) *Server {
	t.Helper()
	config := validServerConfig(application, logger)
	config.Listener = listener
	config.ShutdownTimeout = shutdownTimeout
	config.Closers = closers
	server, err := NewServer(config)
	require.NoError(t, err)
	require.NotNil(t, server.Handler())
	return server
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func listenLocal(t *testing.T) net.Listener {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return listener
}

func requireListenerUnavailable(t *testing.T, address string) {
	t.Helper()
	connection, err := net.DialTimeout("tcp", address, serverTestTimeout)
	if err == nil {
		require.NoError(t, connection.Close())
	}
	require.Error(t, err, "listener still accepted connections")
}

func serveInBackground(server *Server, ctx context.Context, listener net.Listener) <-chan error {
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	return done
}

func waitForReady(t *testing.T, application *App) {
	t.Helper()
	deadline := time.Now().Add(serverTestTimeout)
	for application.readiness.Ready(context.Background()) != nil {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for server readiness")
		}
		runtime.Gosched()
	}
}

func receive[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case value := <-channel:
		return value
	case <-time.After(serverTestTimeout):
		t.Fatal("timed out waiting for lifecycle event")
		var zero T
		return zero
	}
}
