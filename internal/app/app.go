package app

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"sync/atomic"

	"github.com/edocsss/agent-whiteboard/internal/common"
	httpx "github.com/edocsss/agent-whiteboard/internal/http"
	"github.com/edocsss/agent-whiteboard/internal/image"
	"github.com/edocsss/agent-whiteboard/internal/whiteboard"
)

type Readiness interface {
	Ready(context.Context) error
}

type Config struct {
	Whiteboards *whiteboard.Handler
	Images      *image.Handler
	Readiness   []Readiness
}

type App struct {
	handler   http.Handler
	readiness *readiness
}

type readiness struct {
	accepting    atomic.Bool
	dependencies []Readiness
}

func New(config Config) (*App, error) {
	switch {
	case config.Whiteboards == nil:
		return nil, common.NewError(common.CodeInvalidRequest, "whiteboard handler is required", nil)
	case config.Images == nil:
		return nil, common.NewError(common.CodeInvalidRequest, "image handler is required", nil)
	}
	for _, dependency := range config.Readiness {
		if isNilReadiness(dependency) {
			return nil, common.NewError(common.CodeInvalidRequest, "readiness dependency is required", nil)
		}
	}

	state := &readiness{dependencies: append([]Readiness(nil), config.Readiness...)}
	mux := http.NewServeMux()
	config.Whiteboards.Register(mux)
	config.Images.Register(mux)
	httpx.RegisterHealth(mux, state)

	return &App{handler: mux, readiness: state}, nil
}

func (a *App) Handler() http.Handler {
	return a.handler
}

func (a *App) SetReady(ready bool) {
	a.readiness.accepting.Store(ready)
}

func (r *readiness) Ready(ctx context.Context) error {
	if !r.accepting.Load() {
		return errors.New("not accepting requests")
	}
	for _, dependency := range r.dependencies {
		if err := dependency.Ready(ctx); err != nil {
			return err
		}
	}
	return nil
}

func isNilReadiness(value Readiness) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}
