package agentwb

import (
	"context"
	"net"
	"net/http"

	"github.com/edocsss/agent-whiteboard/internal/app"
	"github.com/edocsss/agent-whiteboard/internal/image"
	"github.com/edocsss/agent-whiteboard/internal/whiteboard"
)

type Service struct {
	whiteboards *whiteboard.Service
	images      *image.Service
	server      *app.Server
}

func (service *Service) CreateMarkdown(ctx context.Context, input CreateWhiteboardInput) (WhiteboardResult, error) {
	return service.whiteboards.CreateMarkdown(ctx, input)
}

func (service *Service) CreateHTML(ctx context.Context, input CreateWhiteboardInput) (WhiteboardResult, error) {
	return service.whiteboards.CreateHTML(ctx, input)
}

func (service *Service) GetWhiteboard(ctx context.Context, id string) (Whiteboard, error) {
	return service.whiteboards.Get(ctx, id)
}

func (service *Service) UpdateWhiteboard(ctx context.Context, input UpdateWhiteboardInput) (WhiteboardResult, error) {
	return service.whiteboards.Update(ctx, input)
}

func (service *Service) DeleteWhiteboard(ctx context.Context, kind WhiteboardKind, id string) error {
	return service.whiteboards.Delete(ctx, kind, id)
}

func (service *Service) CreateImages(ctx context.Context, input CreateImagesInput) ([]ImageResult, error) {
	return service.images.CreateImages(ctx, input)
}

func (service *Service) GetImage(ctx context.Context, id string) (Image, error) {
	return service.images.Get(ctx, id)
}

func (service *Service) UpdateImage(ctx context.Context, input UpdateImageInput) (ImageResult, error) {
	return service.images.Update(ctx, input)
}

func (service *Service) DeleteImage(ctx context.Context, id string) error {
	return service.images.Delete(ctx, id)
}

func (service *Service) Handler() http.Handler {
	return service.server.Handler()
}

func (service *Service) ListenAndServe(ctx context.Context) error {
	return service.server.ListenAndServe(ctx)
}

func (service *Service) Serve(ctx context.Context, listener net.Listener) error {
	return service.server.Serve(ctx, listener)
}

func (service *Service) Shutdown(ctx context.Context) error {
	return service.server.Shutdown(ctx)
}

func (service *Service) Close() error {
	return service.server.Close()
}
