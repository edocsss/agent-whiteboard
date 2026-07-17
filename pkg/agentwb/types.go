package agentwb

import (
	"github.com/edocsss/agent-whiteboard/internal/common"
	"github.com/edocsss/agent-whiteboard/internal/image"
	"github.com/edocsss/agent-whiteboard/internal/whiteboard"
)

type Whiteboard = whiteboard.Whiteboard
type WhiteboardKind = whiteboard.Kind
type CreateWhiteboardInput = whiteboard.CreateInput
type UpdateWhiteboardInput = whiteboard.UpdateInput
type WhiteboardResult = whiteboard.Result

const (
	KindMarkdown = whiteboard.KindMarkdown
	KindHTML     = whiteboard.KindHTML
)

type Image = image.Image
type ImageUpload = image.Upload
type CreateImagesInput = image.CreateInput
type UpdateImageInput = image.UpdateInput
type ImageResult = image.Result

type Clock = common.Clock
type IDGenerator = common.IDGenerator
