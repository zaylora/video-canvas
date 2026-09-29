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

// AI 生成任务相关配置。平台协议、模型、凭证不在这里，存在数据库里（见平台协议配置化设计）。
type AI struct {
	MaxActiveTasksPerUser int    `mapstructure:"max_active_tasks_per_user"` // 每个用户进行中任务上限
	InitialCredits        int    `mapstructure:"initial_credits"`           // 新用户初始积分
	WebhookBaseURL        string `mapstructure:"webhook_base_url"`          // 为空时不注册 callback，纯轮询
	WebhookSecret         string `mapstructure:"webhook_secret"`            // 回调路径里的密钥，只通过环境变量提供
	SecretKey             string `mapstructure:"secret_key"`                // 加解密 ai_secrets 的主密钥，只通过环境变量 APP_AI_SECRET_KEY 提供
	Worker                Worker `mapstructure:"worker"`
}

// Worker 是任务调度参数。
type Worker struct {
	Enabled     bool          `mapstructure:"enabled"`
	Concurrency int           `mapstructure:"concurrency"` // 执行池大小
	Tick        time.Duration `mapstructure:"tick"`        // 调度循环间隔
	BatchSize   int           `mapstructure:"batch_size"`  // 每个 tick 最多领取的任务数
	Lease       time.Duration `mapstructure:"lease"`       // 租约时长
}

// Storage 是素材存储配置：driver 取 local / s3（S3 兼容，含阿里云 OSS）。
type Storage struct {
	Driver    string        `mapstructure:"driver"`
	MaxUpload int64         `mapstructure:"max_upload_bytes"` // 单个上传文件上限
	MaxResult int64         `mapstructure:"max_result_bytes"` // 单个生成产物转存上限
	SignedTTL time.Duration `mapstructure:"signed_ttl"`       // 私有桶签名 URL 有效期
	Local     LocalStorage  `mapstructure:"local"`
	S3        S3Storage     `mapstructure:"s3"`
}

// LocalStorage 本地磁盘，仅用于开发环境。
type LocalStorage struct {
	Dir     string `mapstructure:"dir"`      // 存放目录
	BaseURL string `mapstructure:"base_url"` // 对外访问前缀，为空时用相对路径 /files
}

// S3Storage S3 兼容存储；阿里云 OSS 填 endpoint 即可。
type S3Storage struct {
	Endpoint      string `mapstructure:"endpoint"`
	Region        string `mapstructure:"region"`
	Bucket        string `mapstructure:"bucket"`
	AccessKey     string `mapstructure:"access_key"`
	SecretKey     string `mapstructure:"secret_key"`
	UseSSL        bool   `mapstructure:"use_ssl"`
	PublicBaseURL string `mapstructure:"public_base_url"` // 公开桶（或 CDN）前缀；为空则用签名 URL
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
	for _, key := range []string{"ai.secret_key", "ai.webhook_secret", "storage.s3.access_key", "storage.s3.secret_key"} {
		_ = v.BindEnv(key)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	return &cfg, nil
}
