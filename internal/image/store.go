package image

import "context"

type Store interface {
	Create(context.Context, Image) error
	Get(context.Context, string) (Image, error)
	Replace(context.Context, Image) error
	Delete(context.Context, string) error
	Ready(context.Context) error
	Close() error
}
