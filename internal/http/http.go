package http

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	standardhttp "net/http"
	"strconv"
	"time"

	"github.com/edocsss/agent-whiteboard/internal/common"
)

const (
	APIWhiteboardMarkdown = "/api/v1/whiteboards/markdown"
	APIWhiteboardHTML     = "/api/v1/whiteboards/html"
	APIImages             = "/api/v1/images"
	PublicMarkdown        = "/whiteboards/markdown/"
	PublicHTML            = "/whiteboards/html/"
	PublicImages          = "/images/"
)

type ErrorBody struct {
	Code    common.ErrorCode `json:"code"`
	Message string           `json:"message"`
}

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type Resource struct {
	ID        string    `json:"id"`
	Type      string    `json:"type,omitempty"`
	Filename  string    `json:"filename,omitempty"`
	Extension string    `json:"extension,omitempty"`
	MediaType string    `json:"media_type,omitempty"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	ExpiresAt *int64    `json:"expires_at"`
	Permanent bool      `json:"permanent"`
}

type ResourceResponse struct {
	Resource Resource `json:"resource"`
}

type ImagesResponse struct {
	Images []Resource `json:"images"`
}

type MultipartFile struct {
	FieldName string
	Filename  string
	Content   []byte
}

type MultipartForm struct {
	Files            []MultipartFile
	ExpiresInSeconds *int64
}

func WriteJSON(w standardhttp.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteError(w standardhttp.ResponseWriter, err error) {
	status, body := publicError(err)
	WriteJSON(w, status, ErrorResponse{Error: body})
}

func SetPublicHeaders(w standardhttp.ResponseWriter, image bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	robots := "noindex, nofollow, noarchive"
	if image {
		robots += ", noimageindex"
	}
	w.Header().Set("X-Robots-Tag", robots)
}

func ReadMultipart(
	w standardhttp.ResponseWriter,
	r *standardhttp.Request,
	requestLimit int64,
	partLimit int64,
	allowedFileFields ...string,
) (MultipartForm, error) {
	if requestLimit < 0 || partLimit < 0 || len(allowedFileFields) == 0 {
		return MultipartForm{}, invalidRequest("invalid multipart limits or fields", nil)
	}

	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" || params["boundary"] == "" {
		return MultipartForm{}, invalidRequest("invalid multipart form", err)
	}

	r.Body = standardhttp.MaxBytesReader(w, r.Body, requestLimit)
	reader := multipart.NewReader(r.Body, params["boundary"])
	allowed := make(map[string]struct{}, len(allowedFileFields))
	for _, field := range allowedFileFields {
		if field == "" || field == "expires_in_seconds" {
			return MultipartForm{}, invalidRequest("invalid multipart file field", nil)
		}
		allowed[field] = struct{}{}
	}

	form := MultipartForm{Files: make([]MultipartFile, 0)}
	sawPart := false
	for {
		part, nextErr := reader.NextPart()
		if errors.Is(nextErr, io.EOF) {
			if !sawPart {
				return MultipartForm{}, invalidRequest("invalid multipart form", nil)
			}
			return form, nil
		}
		if nextErr != nil {
			return MultipartForm{}, multipartReadError(nextErr)
		}
		sawPart = true

		fieldName := part.FormName()
		filename := part.FileName()
		content, readErr := ReadPart(part, partLimit)
		closeErr := part.Close()
		if readErr != nil {
			return MultipartForm{}, readErr
		}
		if closeErr != nil {
			return MultipartForm{}, multipartReadError(closeErr)
		}

		if fieldName == "expires_in_seconds" {
			if filename != "" || form.ExpiresInSeconds != nil {
				return MultipartForm{}, invalidRequest("duplicate or invalid expires_in_seconds", nil)
			}
			expires, parseErr := strconv.ParseInt(string(content), 10, 64)
			if parseErr != nil {
				return MultipartForm{}, invalidRequest("invalid expires_in_seconds", parseErr)
			}
			form.ExpiresInSeconds = &expires
			continue
		}

		if _, ok := allowed[fieldName]; !ok || filename == "" {
			return MultipartForm{}, invalidRequest("unexpected multipart field", nil)
		}
		form.Files = append(form.Files, MultipartFile{
			FieldName: fieldName,
			Filename:  filename,
			Content:   content,
		})
	}
}

func ReadPart(part *multipart.Part, limit int64) ([]byte, error) {
	if part == nil || limit < 0 {
		return nil, invalidRequest("invalid multipart part", nil)
	}

	content, err := io.ReadAll(&io.LimitedReader{R: part, N: limit})
	if err != nil {
		return nil, multipartReadError(err)
	}

	var overflow [1]byte
	read, err := part.Read(overflow[:])
	if read != 0 {
		return nil, common.NewError(common.CodeContentTooLarge, "content too large", nil)
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, multipartReadError(err)
	}
	return content, nil
}

func publicError(err error) (int, ErrorBody) {
	var domainErr *common.Error
	if !errors.As(err, &domainErr) || domainErr == nil {
		return standardhttp.StatusInternalServerError, ErrorBody{
			Code:    common.CodeInternal,
			Message: "internal error",
		}
	}

	status, ok := errorStatuses[domainErr.Code]
	if !ok {
		return standardhttp.StatusInternalServerError, ErrorBody{
			Code:    common.CodeInternal,
			Message: "internal error",
		}
	}
	return status, ErrorBody{Code: domainErr.Code, Message: domainErr.Message}
}

var errorStatuses = map[common.ErrorCode]int{
	common.CodeInvalidRequest:       standardhttp.StatusBadRequest,
	common.CodeNotFound:             standardhttp.StatusNotFound,
	common.CodeContentTooLarge:      standardhttp.StatusRequestEntityTooLarge,
	common.CodeUnsupportedMediaType: standardhttp.StatusUnsupportedMediaType,
	common.CodeStorageUnavailable:   standardhttp.StatusServiceUnavailable,
	common.CodeInternal:             standardhttp.StatusInternalServerError,
}

func multipartReadError(err error) error {
	var maxBytesErr *standardhttp.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		return common.NewError(common.CodeContentTooLarge, "content too large", err)
	}
	return invalidRequest("invalid multipart form", err)
}

func invalidRequest(message string, err error) error {
	return common.NewError(common.CodeInvalidRequest, message, err)
}
