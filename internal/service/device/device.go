// Package device 提供 Linux 可移动存储的设备管理能力：列举块设备 / 分区、
// 挂载与卸载（底层调用 lsblk 与 udisksctl），并把已挂载设备动态注册进设备 Mux
// （storage.Mux）的 /drive/<id> 命名空间，使其可像普通目录一样浏览与传输。
//
// 平台隔离：Linux 有真实实现（device_linux.go），其他平台为空实现（device_other.go），
// 两者共享本文件的类型与接口。上层 handler 面向 Service 接口，通过 Supported() 判断能力。
package device

import (
	"context"
	"errors"
)

// Device 是一个块设备 / 分区的对外视图。
type Device struct {
	Device     string // 分区块设备路径，如 /dev/sdc1（操作时的稳定标识，服务端会重新校验）
	ID         string // 虚拟命名空间挂载点名（路径安全，用于 /drive/<id>），采用 kernel name
	Name       string // 展示名（通常同 kernel name）
	Label      string // 卷标（可空，仅展示）
	FSType     string // 文件系统类型（可空 / 未知）
	Size       int64  // 容量字节
	Mounted    bool   // 是否已挂载
	Mountpoint string // OS 挂载目录（仅 Mounted 时有值）
	Removable  bool   // 是否可移动设备（USB / 热插拔 / RM 位）
	Readonly   bool   // 是否只读
	System     bool   // 是否为系统关键挂载（根 / 引导分区），不应卸载
}

// Service 抽象设备控制能力。Linux 有真实实现，其他平台为空实现。
type Service interface {
	// Supported 返回设备管理是否可用（Linux 且 lsblk/udisksctl 均存在）。
	Supported() bool
	// List 列出当前系统的块设备 / 分区及挂载状态。
	List(ctx context.Context) ([]Device, error)
	// Mount 挂载指定分区并注册进设备 Mux，返回更新后的设备状态。
	Mount(ctx context.Context, device string) (*Device, error)
	// Unmount 卸载指定分区并从设备 Mux 摘除，返回更新后的设备状态。
	Unmount(ctx context.Context, device string) (*Device, error)
}

// 设备服务错误词表，handler 据此映射对外错误码。
var (
	ErrUnsupported = errors.New("device management not supported") // 平台不支持 / 命令缺失
	ErrNotFound    = errors.New("device not found")                // 指定设备不存在或非分区
	ErrBusy        = errors.New("device busy")                     // 卸载时设备正忙
	ErrForbidden   = errors.New("mount not authorized")            // polkit 未授权
	ErrInvalid     = errors.New("invalid device")                 // 入参非法
	ErrCommand     = errors.New("device command failed")           // lsblk/udisksctl 执行失败
)

// safeID 校验 / 归一化设备标识为路径安全的挂载点名（无 /、空格、.. 等）。
// kernel name（sdc1 / nvme0n1p2 / mmcblk0p1）天然安全，此处做白名单兜底。
func safeID(name string) (string, bool) {
	if name == "" || len(name) > 64 {
		return "", false
	}
	if name == "." || name == ".." {
		return "", false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-', c == '_':
			continue
		default:
			return "", false
		}
	}
	return name, true
}
