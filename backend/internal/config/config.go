package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   Server   `mapstructure:"server"`
	Log      Log      `mapstructure:"log"`
	Database Database `mapstructure:"database"`
	Redis    Redis    `mapstructure:"redis"`
	JWT      JWT      `mapstructure:"jwt"`
	AI       AI       `mapstructure:"ai"`
	Storage  Storage  `mapstructure:"storage"`
}

type Server struct {
	Name            string        `mapstructure:"name"`
	Mode            string        `mapstructure:"mode"`
	Port            int           `mapstructure:"port"`
	ReadTimeout     time.Duration `mapstructure:"read_timeout"`
	WriteTimeout    time.Duration `mapstructure:"write_timeout"`
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`
	// IDKey 是对外 ID（画布 ID 等）十六进制编码的密钥，只通过环境变量 APP_SERVER_ID_KEY 提供；为空时退回 jwt.secret。
	// 同一环境的所有实例必须一致，换了密钥旧 ID（如书签里的画布地址）全部失效。
	IDKey string `mapstructure:"id_key"`
	// AllowedOrigins 是 WebSocket 升级时允许的 Origin 白名单（与 CORS 保持一致）；为空表示只允许同源。
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

func (s Server) Addr() string {
	return fmt.Sprintf(":%d", s.Port)
}

type Log struct {
	Level      string `mapstructure:"level"`
	Format     string `mapstructure:"format"`
	Filename   string `mapstructure:"filename"`
	MaxSize    int    `mapstructure:"max_size"`
	MaxBackups int    `mapstructure:"max_backups"`
	MaxAge     int    `mapstructure:"max_age"`
}

type Database struct {
	DSN             string        `mapstructure:"dsn"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	AutoMigrate     bool          `mapstructure:"auto_migrate"`
	LogLevel        string        `mapstructure:"log_level"`
}

type Redis struct {
	Enabled  bool   `mapstructure:"enabled"`
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type JWT struct {
	Secret      string `mapstructure:"secret"`
	Issuer      string `mapstructure:"issuer"`
	ExpireHours int    `mapstructure:"expire_hours"`
}

// AI 生成任务相关配置。协议插件、渠道、模型、凭证不在这里，存在数据库里（见协议插件设计）。
type AI struct {
	MaxActiveTasksPerUser int          `mapstructure:"max_active_tasks_per_user"` // 每个用户进行中任务上限
	InitialCredits        int          `mapstructure:"initial_credits"`           // 新用户初始积分
	SecretKey             string       `mapstructure:"secret_key"`                // 加解密 ai_secrets 的主密钥，只通过环境变量 APP_AI_SECRET_KEY 提供
	Worker                Worker       `mapstructure:"worker"`
	PluginRunner          PluginRunner `mapstructure:"plugin_runner"`
}

// PluginRunner 是 plugin-runner 进程（协议插件的沙箱执行环境）的部署与预算配置。
// 生产环境 runner 是独立容器（network_mode: none、mem_limit、restart: always），经共享卷里的 Unix socket 通信，mode 用 external；
// 开发环境（含 Windows）mode 用 spawn：主服务把自己的二进制以 plugin-runner 子命令拉起来，设 GOMEMLIMIT 软限制，崩溃后自动重启。
type PluginRunner struct {
	Mode        string        `mapstructure:"mode"`            // spawn（主服务拉起子进程）| external（连已有的 runner）| inprocess（同进程，无隔离，只给测试和排障）
	Network     string        `mapstructure:"network"`         // unix | tcp
	Address     string        `mapstructure:"address"`         // unix 是 socket 路径，tcp 是 host:port（开发默认 127.0.0.1:47650）
	HookTimeout time.Duration `mapstructure:"hook_timeout"`    // 单次钩子调用时限，默认 200ms（提案值，压测后定）
	PoolSize    int           `mapstructure:"pool_size"`       // 每个插件版本最多的 goja Runtime 数，默认 8
	MemoryLimit int64         `mapstructure:"memory_limit_mb"` // spawn 模式下子进程的 GOMEMLIMIT（MiB），默认 512
	StartupWait time.Duration `mapstructure:"startup_wait"`    // 启动时等 runner 就绪的最长时间，默认 15s
}

// Worker 是任务调度参数。
type Worker struct {
	Enabled     bool          `mapstructure:"enabled"`
	Concurrency int           `mapstructure:"concurrency"` // 执行池大小
	Tick        time.Duration `mapstructure:"tick"`        // 调度循环间隔
	BatchSize   int           `mapstructure:"batch_size"`  // 每个 tick 最多领取的任务数
	Lease       time.Duration `mapstructure:"lease"`       // 租约时长
}

// Storage 是素材存储的全局配置。对象存储（阿里云 OSS / 腾讯云 COS / S3 / R2）在后台“存储配置”里管理，
// 这里只保留内置的本地磁盘、上传 / 转存大小上限，以及稳定地址的站点前缀。
type Storage struct {
	MaxUpload int64         `mapstructure:"max_upload_bytes"` // 单个上传文件上限
	MaxResult int64         `mapstructure:"max_result_bytes"` // 单个生成产物转存上限
	SignedTTL time.Duration `mapstructure:"signed_ttl"`       // 存储自己没配签名有效期时的兜底（本地磁盘、旧版导入）
	Local     LocalStorage  `mapstructure:"local"`

	// Driver 与 S3 是旧版（升级前）用环境变量配置对象存储的方式，已废弃：
	// 只在升级后首次启动时读取，用来一次性导入为后台存储，之后不再使用。
	Driver string    `mapstructure:"driver"`
	S3     S3Storage `mapstructure:"s3"`
}

// LocalStorage 是内置的本地磁盘存储。
type LocalStorage struct {
	Dir     string `mapstructure:"dir"`      // 存放目录
	BaseURL string `mapstructure:"base_url"` // 素材稳定地址（/files/<key>）的站点前缀，为空时用相对路径；所有存储共用
}

// S3Storage 是旧版的 S3 兼容存储配置，仅用于升级导入（见 Storage.Driver）。
type S3Storage struct {
	Endpoint      string `mapstructure:"endpoint"`
	Region        string `mapstructure:"region"`
	Bucket        string `mapstructure:"bucket"`
	AccessKey     string `mapstructure:"access_key"`
	SecretKey     string `mapstructure:"secret_key"`
	UseSSL        bool   `mapstructure:"use_ssl"`
	PublicBaseURL string `mapstructure:"public_base_url"`
	PathPrefix    string `mapstructure:"path_prefix"`
}

// Load 读取 YAML 配置文件，并允许用 APP_ 前缀的环境变量覆盖（如 APP_SERVER_PORT）。
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetEnvPrefix("APP")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	// 没写进 YAML 的项也要能被环境变量覆盖（viper 只会为已知的 key 读取环境变量）
	// 其中 storage.driver 与 storage.s3.* 是旧版对象存储的环境变量，YAML 里已经没有这些项，仍需要绑定才能读到，
	// 否则升级时无法把它们导入为后台存储
	for _, key := range []string{
		"server.id_key", "ai.secret_key", "storage.driver",
		"storage.s3.endpoint", "storage.s3.region", "storage.s3.bucket", "storage.s3.access_key", "storage.s3.secret_key",
		"storage.s3.use_ssl", "storage.s3.public_base_url", "storage.s3.path_prefix",
	} {
		_ = v.BindEnv(key)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &cfg, nil
}
