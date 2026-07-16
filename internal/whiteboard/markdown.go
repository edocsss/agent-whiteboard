package whiteboard

import (
	"unicode/utf8"

	"github.com/edocsss/agent-whiteboard/internal/common"
)

func validateMarkdown(source []byte) error {
	if !utf8.Valid(source) {
		return common.NewError(common.CodeInvalidRequest, "markdown must be UTF-8", nil)
	}
	return nil
}
