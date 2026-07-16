package image

import (
	"bytes"
	"image/gif"
	"image/jpeg"
	"image/png"
	"net/http"

	"github.com/edocsss/agent-whiteboard/internal/common"
	"golang.org/x/image/webp"
)

func DetectFormat(content []byte) (string, string, error) {
	mediaType := http.DetectContentType(content)
	reader := bytes.NewReader(content)

	var err error
	var extension string
	switch mediaType {
	case "image/png":
		_, err = png.DecodeConfig(reader)
		extension = ".png"
	case "image/jpeg":
		_, err = jpeg.DecodeConfig(reader)
		extension = ".jpg"
	case "image/gif":
		_, err = gif.DecodeConfig(reader)
		extension = ".gif"
	case "image/webp":
		_, err = webp.DecodeConfig(reader)
		extension = ".webp"
	default:
		return "", "", unsupportedMediaType()
	}
	if err != nil {
		return "", "", unsupportedMediaType()
	}

	return extension, mediaType, nil
}

func unsupportedMediaType() error {
	return common.NewError(common.CodeUnsupportedMediaType, "unsupported image format", nil)
}
