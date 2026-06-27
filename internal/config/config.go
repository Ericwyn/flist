package config

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"
)

// Config 保存服务运行所需的全部配置。
type Config struct {
	Addr       string        // HTTP 监听地址
	Root       string        // 用户提供的 root（未标准化）
	Data       string        // 数据目录（SQLite 等）
	AdminUser  string        // 初始管理员用户名
	AdminPass  string        // 初始管理员密码，空表示随机生成
	SessionTTL time.Duration // 会话有效期
	LongPath   bool          // Windows 长路径支持
	CORSOrigin string        // 允许的 CORS 来源（dev 调试用），空表示不开启
}

const (
	defaultAddr       = ":16550"
	defaultData       = "./data"
	defaultAdminUser  = "admin"
	defaultSessionTTL = 24 * time.Hour
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

	addr := fs.String("addr", envOr("FLIST_ADDR", defaultAddr), "HTTP 监听地址")
	root := fs.String("root", envOr("FLIST_ROOT", ""), "允许管理的根目录（必填）")
	data := fs.String("data", envOr("FLIST_DATA", defaultData), "数据目录（SQLite 等）")
	adminUser := fs.String("admin-user", envOr("FLIST_ADMIN_USER", defaultAdminUser), "初始管理员用户名")
	adminPass := fs.String("admin-pass", envOr("FLIST_ADMIN_PASS", ""), "初始管理员密码，缺省随机生成并打印")
	sessionTTL := fs.Duration("session-ttl", sessionTTLDefault, "会话有效期")
	longPath := fs.Bool("long-path", longPathDefault, "启用 Windows 长路径支持（仅 Windows 有效）")
	corsOrigin := fs.String("cors-origin", envOr("FLIST_CORS_ORIGIN", ""), "允许的 CORS 来源（前后端分离调试用），空表示关闭")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	if *root == "" {
		return nil, fmt.Errorf("--root 为必填项：未指定根目录，拒绝启动（避免误将整个文件系统暴露）")
	}

	cfg := &Config{
		Addr:       *addr,
		Root:       *root,
		Data:       *data,
		AdminUser:  *adminUser,
		AdminPass:  *adminPass,
		SessionTTL: *sessionTTL,
		LongPath:   *longPath,
		CORSOrigin: *corsOrigin,
	}
	return cfg, nil
}
