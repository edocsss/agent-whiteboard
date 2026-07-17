package whiteboard

import "context"

type Store interface {
	Create(context.Context, Whiteboard) error
	Get(context.Context, string) (Whiteboard, error)
	Replace(context.Context, Whiteboard) error
	Delete(context.Context, string) error
	Ready(context.Context) error
	Close() error
}
