package whiteboard

import (
	"context"
	"net/http"

	"github.com/edocsss/agent-whiteboard/internal/common"
	httpx "github.com/edocsss/agent-whiteboard/internal/http"
)

type Operations interface {
	CreateMarkdown(context.Context, CreateInput) (Result, error)
	CreateHTML(context.Context, CreateInput) (Result, error)
	Get(context.Context, string) (Whiteboard, error)
	Update(context.Context, UpdateInput) (Result, error)
	Delete(context.Context, Kind, string) error
}

type HandlerConfig struct {
	MaxBytes int64
}

type Handler struct {
	operations Operations
	viewer     *Viewer
	maxBytes   int64
}

func NewHandler(operations Operations, viewer *Viewer, config HandlerConfig) (*Handler, error) {
	switch {
	case isNilDependency(operations):
		return nil, common.NewError(common.CodeInvalidRequest, "operations are required", nil)
	case viewer == nil:
		return nil, common.NewError(common.CodeInvalidRequest, "viewer is required", nil)
	case config.MaxBytes < 0:
		return nil, common.NewError(common.CodeInvalidRequest, "max bytes must not be negative", nil)
	}

	return &Handler{
		operations: operations,
		viewer:     viewer,
		maxBytes:   config.MaxBytes,
	}, nil
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST "+httpx.APIWhiteboardMarkdown, h.createMarkdown)
	mux.HandleFunc("PUT "+httpx.APIWhiteboardMarkdown+"/{id}", h.updateMarkdown)
	mux.HandleFunc("DELETE "+httpx.APIWhiteboardMarkdown+"/{id}", h.deleteMarkdown)
	mux.HandleFunc("POST "+httpx.APIWhiteboardHTML, h.createHTML)
	mux.HandleFunc("PUT "+httpx.APIWhiteboardHTML+"/{id}", h.updateHTML)
	mux.HandleFunc("DELETE "+httpx.APIWhiteboardHTML+"/{id}", h.deleteHTML)
	mux.HandleFunc("GET "+httpx.PublicMarkdown+"{id}", h.viewMarkdown)
	mux.HandleFunc("GET "+httpx.PublicHTML+"{id}", h.viewHTML)
}

func (h *Handler) viewMarkdown(w http.ResponseWriter, r *http.Request) {
	h.view(w, r, KindMarkdown)
}

func (h *Handler) viewHTML(w http.ResponseWriter, r *http.Request) {
	h.view(w, r, KindHTML)
}

func (h *Handler) view(w http.ResponseWriter, r *http.Request, kind Kind) {
	httpx.SetPublicHeaders(w, false)
	id := r.PathValue("id")
	if err := common.ValidateID(id); err != nil {
		httpx.WriteError(w, notFound())
		return
	}

	board, err := h.operations.Get(r.Context(), id)
	if err != nil {
		if common.HasCode(err, common.CodeNotFound) {
			err = notFound()
		}
		httpx.WriteError(w, err)
		return
	}
	if board.Kind != kind {
		httpx.WriteError(w, notFound())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if kind == KindMarkdown {
		_ = h.viewer.Render(w, board)
		return
	}
	_, _ = w.Write(board.Source)
}

func (h *Handler) createMarkdown(w http.ResponseWriter, r *http.Request) {
	h.create(w, r, KindMarkdown)
}

func (h *Handler) createHTML(w http.ResponseWriter, r *http.Request) {
	h.create(w, r, KindHTML)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request, kind Kind) {
	input, err := h.readCreateInput(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}

	var result Result
	if kind == KindMarkdown {
		result, err = h.operations.CreateMarkdown(r.Context(), input)
	} else {
		result, err = h.operations.CreateHTML(r.Context(), input)
	}
	if err != nil {
		httpx.WriteError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, httpx.ResourceResponse{
		Resource: resourceFromResult(result, kind),
	})
}

func (h *Handler) updateMarkdown(w http.ResponseWriter, r *http.Request) {
	h.update(w, r, KindMarkdown)
}

func (h *Handler) updateHTML(w http.ResponseWriter, r *http.Request) {
	h.update(w, r, KindHTML)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request, kind Kind) {
	id := r.PathValue("id")
	if err := common.ValidateID(id); err != nil {
		httpx.WriteError(w, notFound())
		return
	}

	form, err := h.readSingleFile(w, r)
	if err != nil {
		httpx.WriteError(w, err)
		return
	}
	result, err := h.operations.Update(r.Context(), UpdateInput{
		ID:               id,
		Kind:             kind,
		Source:           form.Files[0].Content,
		ExpiresInSeconds: form.ExpiresInSeconds,
	})
	if err != nil {
		httpx.WriteError(w, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, httpx.ResourceResponse{
		Resource: resourceFromResult(result, kind),
	})
}

func (h *Handler) deleteMarkdown(w http.ResponseWriter, r *http.Request) {
	h.delete(w, r, KindMarkdown)
}

func (h *Handler) deleteHTML(w http.ResponseWriter, r *http.Request) {
	h.delete(w, r, KindHTML)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request, kind Kind) {
	id := r.PathValue("id")
	if err := common.ValidateID(id); err != nil {
		httpx.WriteError(w, notFound())
		return
	}
	if err := h.operations.Delete(r.Context(), kind, id); err != nil {
		httpx.WriteError(w, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) readCreateInput(w http.ResponseWriter, r *http.Request) (CreateInput, error) {
	form, err := h.readSingleFile(w, r)
	if err != nil {
		return CreateInput{}, err
	}
	return CreateInput{
		Source:           form.Files[0].Content,
		ExpiresInSeconds: form.ExpiresInSeconds,
	}, nil
}

func (h *Handler) readSingleFile(w http.ResponseWriter, r *http.Request) (httpx.MultipartForm, error) {
	form, err := httpx.ReadMultipart(w, r, h.maxBytes, h.maxBytes, "file")
	if err != nil {
		return httpx.MultipartForm{}, err
	}
	if len(form.Files) != 1 {
		return httpx.MultipartForm{}, common.NewError(common.CodeInvalidRequest, "exactly one file is required", nil)
	}
	return form, nil
}

func resourceFromResult(result Result, kind Kind) httpx.Resource {
	resource := httpx.Resource{
		ID:        result.ID,
		Type:      string(kind),
		Path:      publicPath(kind) + result.ID,
		CreatedAt: result.CreatedAt,
		UpdatedAt: result.UpdatedAt,
		Permanent: result.ExpiresAt == nil,
	}
	if result.ExpiresAt != nil {
		expiresAt := result.ExpiresAt.Unix()
		resource.ExpiresAt = &expiresAt
	}
	return resource
}

func publicPath(kind Kind) string {
	if kind == KindMarkdown {
		return httpx.PublicMarkdown
	}
	return httpx.PublicHTML
}
