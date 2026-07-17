package agentwb

import (
	"context"
	"errors"
	"io"

	"github.com/edocsss/agent-whiteboard/internal/app"
	"github.com/edocsss/agent-whiteboard/internal/image"
	"github.com/edocsss/agent-whiteboard/internal/store"
	"github.com/edocsss/agent-whiteboard/internal/whiteboard"
)

func New(config Config, options ...Option) (*Service, error) {
	resolved, err := resolveConfig(config, options)
	if err != nil {
		return nil, err
	}

	whiteboardStore := resolved.whiteboardStore
	imageStore := resolved.imageStore
	closers := make([]io.Closer, 0, 2)
	var ownedFilesystem *filesystemLifecycle
	if whiteboardStore == nil || imageStore == nil {
		lifetimeCtx, cancelLifetime := context.WithCancel(context.Background())
		filesystem, fsErr := store.NewFS(store.Config{
			Root:            resolved.rootDir,
			CleanupInterval: resolved.cleanupInterval,
			Clock:           resolved.clock,
			Context:         lifetimeCtx,
		})
		if fsErr != nil {
			cancelLifetime()
			return nil, fsErr
		}
		ownedFilesystem = &filesystemLifecycle{cancel: cancelLifetime, filesystem: filesystem}
		closers = append(closers, ownedFilesystem)
		if whiteboardStore == nil {
			whiteboardStore = filesystem.Whiteboards()
		}
		if imageStore == nil {
			imageStore = filesystem.Images()
		}
	}

	if resolved.whiteboardStore != nil {
		closers = append(closers, resolved.whiteboardStore)
	}
	if resolved.imageStore != nil {
		closers = append(closers, resolved.imageStore)
	}

	fail := func(constructionErr error) (*Service, error) {
		if ownedFilesystem == nil {
			return nil, constructionErr
		}
		return nil, errors.Join(constructionErr, ownedFilesystem.Close())
	}

	whiteboardService, err := whiteboard.NewService(whiteboardStore, resolved.clock, resolved.ids, resolved.defaultExpiration)
	if err != nil {
		return fail(err)
	}
	imageService, err := image.NewService(imageStore, resolved.clock, resolved.ids, resolved.defaultExpiration, resolved.logger)
	if err != nil {
		return fail(err)
	}
	viewer, err := whiteboard.NewViewer(whiteboard.ViewerConfig{CSS: resolved.viewerCSS, JS: resolved.viewerJS})
	if err != nil {
		return fail(err)
	}
	whiteboardHandler, err := whiteboard.NewHandler(whiteboardService, viewer, whiteboard.HandlerConfig{
		MaxBytes: resolved.maxWhiteboardBytes,
	})
	if err != nil {
		return fail(err)
	}
	imageHandler, err := image.NewHandler(imageService, image.HandlerConfig{
		MaxImageBytes:   resolved.maxImageBytes,
		MaxRequestBytes: resolved.maxImageRequestBytes,
	})
	if err != nil {
		return fail(err)
	}
	application, err := app.New(app.Config{
		Whiteboards: whiteboardHandler,
		Images:      imageHandler,
		Readiness:   []app.Readiness{whiteboardStore, imageStore},
	})
	if err != nil {
		return fail(err)
	}
	server, err := app.NewServer(app.ServerConfig{
		App:             application,
		Logger:          resolved.logger,
		Host:            resolved.host,
		Port:            resolved.port,
		ShutdownTimeout: resolved.shutdownTimeout,
		Closers:         closers,
		Listener:        resolved.listener,
	})
	if err != nil {
		return fail(err)
	}

	return &Service{whiteboards: whiteboardService, images: imageService, server: server}, nil
}

type filesystemLifecycle struct {
	cancel     context.CancelFunc
	filesystem *store.FS
}

func (lifecycle *filesystemLifecycle) Close() error {
	lifecycle.cancel()
	return lifecycle.filesystem.Close()
}
