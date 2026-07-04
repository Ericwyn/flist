//go:build linux

package device

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"flist/internal/storage"
	"flist/internal/storage/local"
	"flist/internal/util"
)

// New 构造 Linux 设备服务。探测 lsblk 与 udisksctl 是否存在：任一缺失则返回一个
// Supported()==false 的实例（而非 nil），使上层逻辑统一。mux 为设备 Mux（/drive 下的内层命名空间）。
func New(mux *storage.Mux, logger *slog.Logger) Service {
	s := &linuxService{mux: mux, logger: logger}
	if p, err := exec.LookPath("lsblk"); err == nil {
		s.lsblkPath = p
	}
	if p, err := exec.LookPath("udisksctl"); err == nil {
		s.udisksPath = p
	}
	s.run = s.execCommand
	if logger != nil {
		logger.Info("device management probe",
			slog.Bool("supported", s.Supported()),
			slog.String("lsblk", s.lsblkPath),
			slog.String("udisksctl", s.udisksPath),
		)
	}
	return s
}

type linuxService struct {
	mux        *storage.Mux
	logger     *slog.Logger
	lsblkPath  string
	udisksPath string
	mu         sync.Mutex // 串行化挂 / 卸，避免并发操作同设备

	// run 执行外部命令并返回合并输出，抽成字段便于测试注入固定输出。
	run func(ctx context.Context, name string, args ...string) ([]byte, error)
}

func (s *linuxService) Supported() bool {
	return s.lsblkPath != "" && s.udisksPath != ""
}

// execCommand 以 30s 超时执行命令，捕获 stdout+stderr 合并返回。
func (s *linuxService) execCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.Bytes(), err
}

// List 列出块设备 / 分区。
func (s *linuxService) List(ctx context.Context) ([]Device, error) {
	if !s.Supported() {
		return nil, ErrUnsupported
	}
	out, err := s.run(ctx, s.lsblkPath, "-J", "-b",
		"-o", "NAME,PATH,TYPE,SIZE,FSTYPE,LABEL,MOUNTPOINT,RM,RO,HOTPLUG,TRAN")
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("lsblk failed", slog.String("output", string(out)), slog.Any("error", err))
		}
		return nil, fmt.Errorf("%w: lsblk: %v", ErrCommand, err)
	}
	parsed, err := parseLsblk(out)
	if err != nil {
		return nil, fmt.Errorf("%w: parse lsblk: %v", ErrCommand, err)
	}
	// 本功能只管理移动设备（U 盘 / 移动硬盘）；本地磁盘 / 系统盘一律不纳入。
	// 对已挂载的移动设备（含被系统自动挂载、或 flist 重启后仍挂着的）做惰性注册，
	// 使「进入」在任何情况下都能路由到 /drive/<id>，避免 page_not_found。
	devices := make([]Device, 0, len(parsed))
	for i := range parsed {
		if !parsed[i].Removable {
			continue
		}
		if parsed[i].Mounted {
			s.ensureRegistered(&parsed[i])
		}
		devices = append(devices, parsed[i])
	}
	return devices, nil
}

// Mount 挂载指定分区并注册进设备 Mux。
func (s *linuxService) Mount(ctx context.Context, device string) (*Device, error) {
	if !s.Supported() {
		return nil, ErrUnsupported
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	dev, err := s.findAndValidate(ctx, device)
	if err != nil {
		return nil, err
	}

	if dev.Mounted {
		// 已挂载（可能被系统自动挂载）：确保已注册进 mux。
		s.ensureRegistered(dev)
		return dev, nil
	}

	out, err := s.run(ctx, s.udisksPath, "mount", "-b", dev.Device)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("udisksctl mount failed",
				slog.String("device", dev.Device), slog.String("output", string(out)), slog.Any("error", err))
		}
		return nil, mapUdisksErr(out)
	}

	mp := parseMountpoint(string(out))
	if mp == "" {
		// 兜底：重新 lsblk 读挂载点。
		if fresh, ferr := s.findAndValidate(ctx, dev.Device); ferr == nil {
			mp = fresh.Mountpoint
		}
	}
	if mp == "" {
		return nil, fmt.Errorf("%w: cannot determine mountpoint", ErrCommand)
	}

	dev.Mounted = true
	dev.Mountpoint = mp
	s.ensureRegistered(dev)

	if s.logger != nil {
		s.logger.Info("device mounted",
			slog.String("device", dev.Device), slog.String("id", dev.ID), slog.String("mountpoint", mp))
	}
	return dev, nil
}

// Unmount 卸载指定分区并从设备 Mux 摘除。先摘挂载点，unmount 失败则回滚重挂。
func (s *linuxService) Unmount(ctx context.Context, device string) (*Device, error) {
	if !s.Supported() {
		return nil, ErrUnsupported
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	dev, err := s.findAndValidate(ctx, device)
	if err != nil {
		return nil, err
	}

	// 先从 mux 摘除，避免卸载过程中被访问；保留 backend 以便失败回滚。
	prev := s.mux.RemoveMount(dev.ID)

	out, uerr := s.run(ctx, s.udisksPath, "unmount", "-b", dev.Device)
	if uerr != nil {
		if prev != nil {
			_ = s.mux.AddMount(storage.Mount{Name: dev.ID, Backend: prev})
		}
		if s.logger != nil {
			s.logger.Warn("udisksctl unmount failed",
				slog.String("device", dev.Device), slog.String("output", string(out)), slog.Any("error", uerr))
		}
		return nil, mapUdisksErr(out)
	}

	dev.Mounted = false
	dev.Mountpoint = ""
	if s.logger != nil {
		s.logger.Info("device unmounted", slog.String("device", dev.Device), slog.String("id", dev.ID))
	}
	return dev, nil
}

// findAndValidate 重新 List 并按 Device 路径查找，确保设备存在且为可挂载分区（不信任入参）。
func (s *linuxService) findAndValidate(ctx context.Context, device string) (*Device, error) {
	device = strings.TrimSpace(device)
	if device == "" || !strings.HasPrefix(device, "/dev/") {
		return nil, ErrInvalid
	}
	devices, err := s.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range devices {
		if devices[i].Device == device {
			d := devices[i]
			return &d, nil
		}
	}
	return nil, ErrNotFound
}

// ensureRegistered 确保设备已挂载点已注册进 mux（幂等）。
func (s *linuxService) ensureRegistered(dev *Device) {
	if dev.Mountpoint == "" || dev.ID == "" {
		return
	}
	real, err := util.ResolveRoot(dev.Mountpoint)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("resolve mountpoint failed",
				slog.String("mountpoint", dev.Mountpoint), slog.Any("error", err))
		}
		return
	}
	// 设备后端不承载分片上传暂存，staging 传空。
	err = s.mux.AddMount(storage.Mount{Name: dev.ID, Backend: local.New(real, "")})
	if err != nil && err != storage.ErrExists {
		if s.logger != nil {
			s.logger.Warn("register device mount failed", slog.String("id", dev.ID), slog.Any("error", err))
		}
	}
}

// ---- lsblk 输出解析 ----

// lsblkOutput 对应 lsblk -J 的顶层结构。
type lsblkOutput struct {
	BlockDevices []lsblkNode `json:"blockdevices"`
}

// lsblkNode 是块设备树节点。size/rm/ro 在不同 lsblk 版本可能为数字、布尔或字符串，
// 故用弹性类型解析。
type lsblkNode struct {
	Name       string      `json:"name"`
	Path       string      `json:"path"`
	Type       string      `json:"type"`
	Size       flexInt64   `json:"size"`
	FSType     string      `json:"fstype"`
	Label      string      `json:"label"`
	Mountpoint string      `json:"mountpoint"`
	RM         flexBool    `json:"rm"`
	RO         flexBool    `json:"ro"`
	Hotplug    flexBool    `json:"hotplug"`
	Tran       string      `json:"tran"`
	Children   []lsblkNode `json:"children"`
}

// parseLsblk 解析 lsblk JSON，展平出可挂载的分区 / 无分区表的整盘。
// removable / tran 由整盘（父节点）决定，分区继承其所属盘的属性。
func parseLsblk(data []byte) ([]Device, error) {
	var out lsblkOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	var devices []Device
	// walk 携带所属整盘的可移动性与传输方式（分区自身不带 hotplug/tran，需继承父盘）。
	var walk func(nodes []lsblkNode, diskRemovable bool, diskTran string)
	walk = func(nodes []lsblkNode, diskRemovable bool, diskTran string) {
		for _, n := range nodes {
			// 顶层盘：以自身属性判定；分区：继承父盘属性。
			removable := diskRemovable
			tran := diskTran
			if n.Type == "disk" || n.Type == "loop" || n.Type == "rom" {
				removable = bool(n.RM) || bool(n.Hotplug) || n.Tran == "usb"
				tran = n.Tran
			}
			if isMountable(n) {
				if id, ok := safeID(n.Name); ok {
					devices = append(devices, Device{
						Device:     devicePath(n),
						ID:         id,
						Name:       n.Name,
						Label:      n.Label,
						FSType:     n.FSType,
						Size:       int64(n.Size),
						Mounted:    n.Mountpoint != "",
						Mountpoint: n.Mountpoint,
						Removable:  removable,
						Readonly:   bool(n.RO),
						System:     isSystemMount(n.Mountpoint),
					})
				}
			}
			if len(n.Children) > 0 {
				walk(n.Children, removable, tran)
			}
		}
	}
	walk(out.BlockDevices, false, "")
	return devices, nil
}

// isMountable 判断节点是否为「可作为存储管理」的条目。
//
// 过滤规则：
//   - loop（snap / squashfs 虚拟盘）、rom（光驱）一律排除，非用户关心的存储；
//   - swap / 无文件系统的裸分区排除（无法浏览）；
//   - 保留有文件系统的分区，以及无分区表直接格式化的整盘 / SD 卡。
func isMountable(n lsblkNode) bool {
	if n.Type == "loop" || n.Type == "rom" {
		return false
	}
	if n.FSType == "" || n.FSType == "swap" {
		return false // 无文件系统 / 交换分区不可浏览
	}
	switch n.Type {
	case "part":
		return true
	case "disk", "mmc":
		// 无分区表直接格式化的 U 盘 / SD 卡：有文件系统且无子分区。
		return len(n.Children) == 0
	default:
		return false
	}
}

// isSystemMount 判断挂载点是否为系统关键位置（根 / 引导分区），这类设备不应被卸载。
func isSystemMount(mp string) bool {
	if mp == "" {
		return false
	}
	if mp == "/" {
		return true
	}
	return mp == "/boot" || strings.HasPrefix(mp, "/boot/")
}

// devicePath 返回节点的块设备路径，缺 path 字段时回落 /dev/<name>。
func devicePath(n lsblkNode) string {
	if n.Path != "" {
		return n.Path
	}
	return "/dev/" + n.Name
}

// parseMountpoint 从 udisksctl mount 输出解析挂载目录：形如
// "Mounted /dev/sdc1 at /run/media/user/LABEL." 或不带尾点。取 " at " 之后、去尾点与空白。
func parseMountpoint(out string) string {
	out = strings.TrimSpace(out)
	const sep = " at "
	idx := strings.LastIndex(out, sep)
	if idx < 0 {
		return ""
	}
	mp := strings.TrimSpace(out[idx+len(sep):])
	mp = strings.TrimRight(mp, ".")
	return strings.TrimSpace(mp)
}

// mapUdisksErr 按 udisksctl 输出关键字归一化错误。
func mapUdisksErr(out []byte) error {
	s := strings.ToLower(string(out))
	switch {
	case strings.Contains(s, "not authorized"), strings.Contains(s, "authentication"), strings.Contains(s, "accessdenied"):
		return ErrForbidden
	case strings.Contains(s, "busy"), strings.Contains(s, "in use"):
		return ErrBusy
	default:
		return fmt.Errorf("%w: %s", ErrCommand, strings.TrimSpace(string(out)))
	}
}

// ---- 弹性 JSON 类型（兼容 lsblk 各版本的数字 / 布尔 / 字符串表示）----

type flexInt64 int64

func (f *flexInt64) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*f = 0
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*f = 0
			return nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return err
		}
		*f = flexInt64(n)
		return nil
	}
	var n int64
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexInt64(n)
	return nil
}

type flexBool bool

func (f *flexBool) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), `"`)
	switch strings.ToLower(s) {
	case "1", "true":
		*f = true
	default:
		*f = false
	}
	return nil
}
