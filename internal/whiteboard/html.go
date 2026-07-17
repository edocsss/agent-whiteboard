package whiteboard

import (
	"bytes"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/edocsss/agent-whiteboard/internal/common"
	"golang.org/x/net/html"
)

func validateHTML(source []byte) error {
	if !utf8.Valid(source) {
		return common.NewError(common.CodeInvalidRequest, "html must be UTF-8", nil)
	}

	var hasDoctype, hasHTML, hasHead, hasBody bool
	tokenizer := html.NewTokenizer(bytes.NewReader(source))
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			err := tokenizer.Err()
			if err != nil && err != io.EOF {
				return common.NewError(common.CodeInvalidRequest, "html is invalid", err)
			}
			return requireDocumentTokens(hasDoctype, hasHTML, hasHead, hasBody)
		case html.DoctypeToken:
			hasDoctype = true
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			switch {
			case strings.EqualFold(token.Data, "html"):
				hasHTML = true
			case strings.EqualFold(token.Data, "head"):
				hasHead = true
			case strings.EqualFold(token.Data, "body"):
				hasBody = true
			case strings.EqualFold(token.Data, "script") && hasAttribute(token, "src"):
				return common.NewError(common.CodeInvalidRequest, "html must not include scripts with src", nil)
			case strings.EqualFold(token.Data, "link") && attributeHasToken(token, "rel", "stylesheet"):
				return common.NewError(common.CodeInvalidRequest, "html must not include stylesheet links", nil)
			}
		}
	}
}

func requireDocumentTokens(hasDoctype, hasHTML, hasHead, hasBody bool) error {
	switch {
	case !hasDoctype:
		return common.NewError(common.CodeInvalidRequest, "html must include a doctype", nil)
	case !hasHTML:
		return common.NewError(common.CodeInvalidRequest, "html must include an html element", nil)
	case !hasHead:
		return common.NewError(common.CodeInvalidRequest, "html must include a head element", nil)
	case !hasBody:
		return common.NewError(common.CodeInvalidRequest, "html must include a body element", nil)
	default:
		return nil
	}
}

func hasAttribute(token html.Token, name string) bool {
	for _, attribute := range token.Attr {
		if strings.EqualFold(attribute.Key, name) {
			return true
		}
	}
	return false
}

func attributeHasToken(token html.Token, name, want string) bool {
	for _, attribute := range token.Attr {
		if !strings.EqualFold(attribute.Key, name) {
			continue
		}
		for _, value := range strings.Fields(attribute.Val) {
			if strings.EqualFold(value, want) {
				return true
			}
		}
	}
	return false
}
