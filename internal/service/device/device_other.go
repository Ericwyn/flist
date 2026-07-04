//go:build !linux

package device

import (
	"context"
	"log/slog"

	"flist/internal/storage"
)

// New 在非 Linux 平台返回空实现：设备管理不可用，接口调用统一返回 ErrUnsupported。
func New(_ *storage.Mux, _ *slog.Logger) Service {
	return unsupported{}
}

type unsupported struct{}

func (unsupported) Supported() bool { return false }

func (unsupported) List(context.Context) ([]Device, error) {
	return nil, ErrUnsupported
}

func (unsupported) Mount(context.Context, string) (*Device, error) {
	return nil, ErrUnsupported
}

func (unsupported) Unmount(context.Context, string) (*Device, error) {
	return nil, ErrUnsupported
}
