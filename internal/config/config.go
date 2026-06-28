package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/pflag"
)

// Config 保存服务运行所需的全部配置。
type Config struct {
	Addr       string        // HTTP 监听地址
	Root       string        // 用户提供的 root（未标准化）
	Data       string        // 数据目录（SQLite 等）
	SessionTTL time.Duration // 会话有效期
	LongPath   bool          // Windows 长路径支持
	CORSOrigin string        // 允许的 CORS 来源（dev 调试用），空表示不开启
	MaxUpload  int64         // 单文件上传上限（字节），0 表示不限
	MaxEdit    int64         // 单文件在线编辑大小上限（字节），超过则拒绝编辑
	Debug      bool          // 调试日志（级别降到 Debug，输出上传分片等细节）
	ResetAdmin bool          // 为 true 时重置 id=1 的管理员凭据后退出，不启动服务
	ResetTOTP  bool          // 为 true 时清除 id=1 的 TOTP 配置后退出，不启动服务
}

const (
	defaultAddr       = ":16550"
	defaultData       = "./data"
	defaultSessionTTL = 24 * time.Hour
	defaultMaxEdit    = 5 << 20 // 5 MiB：单文件在线编辑大小上限
)

// envOr 返回环境变量值，缺失或为空时返回 fallback。
// 空字符串视为未设置，避免空值覆盖默认值或参与解析。
func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}

// Load 按「启动参数 > 环境变量 > 默认值」的优先级解析配置。
// args 为不含程序名的参数切片（通常是 os.Args[1:]）。
//
// 实现方式：先用「环境变量或默认值」作为 flag 的默认值，再解析命令行参数，
// 这样命令行显式传入的值自然覆盖环境变量，未传入的则回落到环境变量/默认值。
func Load(args []string) (*Config, error) {
	fs := pflag.NewFlagSet("flist", pflag.ContinueOnError)

	sessionTTLDefault := defaultSessionTTL
	if v := envOr("FLIST_SESSION_TTL", ""); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			sessionTTLDefault = d
		} else {
			return nil, fmt.Errorf("invalid FLIST_SESSION_TTL %q: %w", v, err)
		}
	}

	longPathDefault := envOr("FLIST_LONG_PATH", "") == "true"

	var maxUploadDefault int64
	if v := envOr("FLIST_MAX_UPLOAD", ""); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("invalid FLIST_MAX_UPLOAD %q: 须为非负整数字节数", v)
		}
		maxUploadDefault = n
	}

	maxEditDefault := int64(defaultMaxEdit)
	if v := envOr("FLIST_MAX_EDIT_SIZE", ""); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid FLIST_MAX_EDIT_SIZE %q: 须为正整数字节数", v)
		}
		maxEditDefault = n
	}

	addr := fs.String("addr", envOr("FLIST_ADDR", defaultAddr), "HTTP 监听地址")
	root := fs.String("root", envOr("FLIST_ROOT", ""), "允许管理的根目录（必填）")
	data := fs.String("data", envOr("FLIST_DATA", defaultData), "数据目录（SQLite 等）")
	sessionTTL := fs.Duration("session-ttl", sessionTTLDefault, "会话有效期")
	longPath := fs.Bool("long-path", longPathDefault, "启用 Windows 长路径支持（仅 Windows 有效）")
	corsOrigin := fs.String("cors-origin", envOr("FLIST_CORS_ORIGIN", ""), "允许的 CORS 来源（前后端分离调试用），空表示关闭")
	maxUpload := fs.Int64("max-upload", maxUploadDefault, "单文件上传上限（字节），0 表示不限")
	maxEdit := fs.Int64("max-edit-size", maxEditDefault, "单文件在线编辑大小上限（字节），超过则拒绝编辑")
	debug := fs.Bool("debug", envOr("FLIST_DEBUG", "") == "true", "调试日志：级别降到 Debug，输出上传分片(index/received/total_chunks)等细节")
	resetAdmin := fs.Bool("reset-admin", false, "重置管理员（id=1）的用户名和密码为 admin + 随机密码后退出，不启动服务。登录后可在设置中修改。")
	resetTotp := fs.Bool("reset-totp", false, "清除管理员（id=1）的 TOTP 配置，恢复为纯密码登录后退出，不启动服务。")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if *maxUpload < 0 {
		return nil, fmt.Errorf("--max-upload 不能为负数")
	}
	if *maxEdit <= 0 {
		return nil, fmt.Errorf("--max-edit-size 必须为正整数字节数")
	}

	// --reset-admin 和 --reset-totp 模式只需要访问数据库，不需要 root。
	if !*resetAdmin && !*resetTotp && *root == "" {
		return nil, fmt.Errorf("--root 为必填项：未指定根目录，拒绝启动（避免误将整个文件系统暴露）")
	}

	cfg := &Config{
		Addr:       *addr,
		Root:       *root,
		Data:       *data,
		SessionTTL: *sessionTTL,
		LongPath:   *longPath,
		CORSOrigin: *corsOrigin,
		MaxUpload:  *maxUpload,
		MaxEdit:    *maxEdit,
		Debug:      *debug,
		ResetAdmin: *resetAdmin,
		ResetTOTP:  *resetTotp,
	}
	return cfg, nil
}
