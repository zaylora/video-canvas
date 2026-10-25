package config

import (
	"fmt"
	"net"
	"net/url"
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
	Agent    Agent    `mapstructure:"agent"`
	Update   Update   `mapstructure:"update"`
}

// DefaultUpdateRepo 是检查新版本用的 GitHub 仓库。
const DefaultUpdateRepo = "zaylora/video-canvas"

// Update 是后台「检查新版本」的配置：读 GitHub Releases 的最新正式版本，只提示、不自动升级。
type Update struct {
	CheckEnabled bool   `mapstructure:"check_enabled"` // 内网或离线部署可关闭
	Repo         string `mapstructure:"repo"`          // owner/name；为空时用 DefaultUpdateRepo
	// 一键升级：updater 容器监听的 unix socket 与共享 token。token 为空表示没启用 updater
	UpdaterSocket string `mapstructure:"updater_socket"`
	UpdaterToken  string `mapstructure:"updater_token"`
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
	AutoMigrate     bool          `mapstructure:"auto_migrate"` // 启动时执行 backend/migrations 里未执行的迁移
	// AllowNewerSchema 为 true 时，库里有本程序不认识的迁移（回退到旧镜像）也照常启动；只在确认那些迁移向后兼容时打开
	AllowNewerSchema bool   `mapstructure:"allow_newer_schema"`
	LogLevel         string `mapstructure:"log_level"`
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

// Agent 是画布 Agent 的运行时配置。它依赖 Node（pi 的 agent 循环在独立的 Node 进程里跑），所以默认关闭，要显式开启。
// 渠道、模型、凭证和计费都在后台的 AI 配置里，不在这里。
type Agent struct {
	Enabled bool `mapstructure:"enabled"` // 是否启用；关闭时发起运行会返回「Agent 暂不可用」
	// Runtime 是 Agent 的运行方式：process（默认）每个运行片段起一个 Node 子进程；http 连独立部署的 Agent 服务（见 agent/src/serve.ts）
	Runtime string `mapstructure:"runtime"`
	// URL 是 Agent 服务的地址（http 模式），如 http://agent:8090，只写协议、主机和端口
	URL string `mapstructure:"url"`
	// BridgeURL 是 Agent 服务访问桥用的地址（http 模式），如 http://backend:47001：它要能从 Agent 容器连上，
	// 和 BridgeAddr（本进程监听在哪）不是一回事。process 模式从监听地址推出来，不用配
	BridgeURL  string `mapstructure:"bridge_url"`
	NodePath   string `mapstructure:"node_path"`   // node 可执行文件，默认 node（在 PATH 里找），要求 >= 22.19
	RuntimeDir string `mapstructure:"runtime_dir"` // 仓库根目录 agent/ 目录（含 src/main.ts（或构建产物 dist/main.js）和已安装的 node_modules），默认 ../agent
	BridgeAddr string `mapstructure:"bridge_addr"` // 桥的监听地址，默认 127.0.0.1:0（随机端口）；不是回环地址时必须配 ServiceToken
	// ServiceToken 是桥的服务令牌：配了以后桥的每个请求都要带请求头 X-Agent-Service-Token，除一次性的运行令牌外多一道校验。
	// Agent 拆成独立服务后，桥要监听内网地址，这是回环之外唯一的屏障，所以要求至少 MinServiceTokenLen 个字符。
	// 只通过环境变量 APP_AGENT_SERVICE_TOKEN 提供，不写进 YAML。
	ServiceToken string `mapstructure:"service_token"`
	WorkDir      string `mapstructure:"work_dir"` // 每个运行片段的临时目录建在这里，默认系统临时目录
	// WebEnabled 是否开放联网工具（搜索、读网页），默认开启。联网请求由 Go 发出（Agent 进程不联网），
	// 内网和本机地址一律拒绝；关闭后模型看不到这两个工具。
	WebEnabled bool `mapstructure:"web_enabled"`
	// SearXNGURL 是自己部署的 SearXNG 地址（如 http://searxng:8080），可选：配了它排在搜索源的第一位，
	// 没配就依次用免配置的 Exa、Bing、DuckDuckGo（后两个抓网页，数据中心的出口常给降级结果或要求人机验证，生产环境建议部署 SearXNG）。
	SearXNGURL string `mapstructure:"searxng_url"`
	// DisabledTools 是停用的工具名（逗号分隔的环境变量 APP_AGENT_DISABLED_TOOLS 也行）：不下发给模型。
	// Go 的工具和 Agent 自己的工具（如 codemode）都可以停用，运维想关掉某个能力时加一项。
	DisabledTools []string `mapstructure:"disabled_tools"`
}

// 取值见 Agent.Runtime
const (
	AgentRuntimeProcess = "process"
	AgentRuntimeHTTP    = "http"
)

// MinServiceTokenLen 是服务令牌的最短长度：它在非回环监听时是唯一的屏障，太短的令牌等于没配。
const MinServiceTokenLen = 32

// ValidServiceToken 报告令牌是否够长。
func ValidServiceToken(token string) bool { return len(token) >= MinServiceTokenLen }

// IsLoopbackHost 报告监听地址的主机部分是不是回环地址（127.0.0.1、::1 或 localhost）。
func IsLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// WithDefaults 补上没配置的项。
func (a Agent) WithDefaults() Agent {
	if a.NodePath == "" {
		a.NodePath = "node"
	}
	if a.RuntimeDir == "" {
		a.RuntimeDir = "../agent"
	}
	if a.BridgeAddr == "" {
		a.BridgeAddr = "127.0.0.1:0"
	}
	if a.Runtime == "" {
		a.Runtime = AgentRuntimeProcess
	}
	return a
}

// Validate 校验启用时的配置。桥的端点用一次性令牌鉴权，默认只该被本机的 Node 进程访问，
// 所以监听地址必须是回环地址：配成 0.0.0.0 或外网地址，令牌之外就没有任何屏障了，直接拒绝启动。
// 唯一的例外是配了足够长的服务令牌（Agent 拆成独立服务后从内网访问桥）：这时每个请求还要多带一道服务令牌。
func (a Agent) Validate() error {
	if !a.Enabled {
		return nil
	}
	if a.WebEnabled && a.SearXNGURL != "" {
		if u, err := url.Parse(a.SearXNGURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("agent.searxng_url 必须是 http(s)://host[:port] 形式，当前是 %q", a.SearXNGURL)
		}
	}
	switch a.WithDefaults().Runtime {
	case AgentRuntimeProcess:
	case AgentRuntimeHTTP:
		if err := a.validateHTTPRuntime(); err != nil {
			return err
		}
	default:
		return fmt.Errorf("agent.runtime 只能是 %s 或 %s，当前是 %q", AgentRuntimeProcess, AgentRuntimeHTTP, a.Runtime)
	}
	if a.ServiceToken != "" && !ValidServiceToken(a.ServiceToken) {
		return fmt.Errorf("agent.service_token 至少 %d 个字符（建议 openssl rand -hex 32）", MinServiceTokenLen)
	}
	host, _, err := net.SplitHostPort(a.WithDefaults().BridgeAddr)
	if err != nil {
		return fmt.Errorf("agent.bridge_addr 必须是 host:port 形式: %w", err)
	}
	if !IsLoopbackHost(host) && a.ServiceToken == "" {
		return fmt.Errorf("agent.bridge_addr 必须是回环地址（127.0.0.1、::1 或 localhost），当前是 %q：桥只该被本机的 Node 进程访问；要让别的容器访问，请配置 agent.service_token（APP_AGENT_SERVICE_TOKEN）", host)
	}
	return nil
}

// validateHTTPRuntime 校验 http 模式必需的三项：Agent 服务地址、桥的对外地址、两边共用的服务令牌。
func (a Agent) validateHTTPRuntime() error {
	if err := checkOrigin("agent.url", a.URL); err != nil {
		return err
	}
	if err := checkOrigin("agent.bridge_url", a.BridgeURL); err != nil {
		return err
	}
	if !ValidServiceToken(a.ServiceToken) {
		return fmt.Errorf("agent.runtime=http 需要 agent.service_token（APP_AGENT_SERVICE_TOKEN），至少 %d 个字符：它是 Go 与 Agent 服务互相认的唯一凭据", MinServiceTokenLen)
	}
	return nil
}

// checkOrigin 要求值是 http(s)://host[:port]，不带路径、查询和用户信息。
func checkOrigin(name, raw string) error {
	u, err := url.Parse(raw)
	if raw == "" || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return fmt.Errorf("%s 必须是 http(s)://host[:port] 形式，当前是 %q", name, raw)
	}
	return nil
}

// AI 生成任务相关配置。协议插件、渠道、模型、凭证不在这里，存在数据库里（见协议插件设计）。
type AI struct {
	SecretKey string `mapstructure:"secret_key"` // 加解密 ai_secrets 的主密钥，只通过环境变量 APP_AI_SECRET_KEY 提供
	// AllowFakeIP 放行 198.18.0.0/15（代理工具 Fake-IP 模式给域名分配的假地址），其余内网照拦。
	// 只给开发机用：本机开了 Fake-IP 的代理时，所有域名都解析到这个网段，不放行连不上任何外部接口。默认关，线上不要开。
	AllowFakeIP  bool         `mapstructure:"allow_fake_ip"`
	Worker       Worker       `mapstructure:"worker"`
	Queue        Queue        `mapstructure:"queue"`
	PluginRunner PluginRunner `mapstructure:"plugin_runner"`
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

// Worker 控制本实例是否执行生成任务。任务由 River 队列调度（见 provider/queue），关闭后本实例只能创建任务（入队），不执行。
type Worker struct {
	Enabled bool `mapstructure:"enabled"`
}

// Queue 是任务队列并发的硬上限（每个实例）。后台「任务队列」页能调的范围是 1..硬上限，所以这里决定了后台最多能把并发调到多少；
// 实际并发在后台设置（默认 16）。改硬上限要重启。
type Queue struct {
	MaxConcurrency int `mapstructure:"max_concurrency"` // 同时处理的任务数的硬上限
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
	v.SetDefault("agent.web_enabled", true) // 联网工具默认开启
	v.SetDefault("ai.queue.max_concurrency", 64)
	v.SetDefault("update.check_enabled", true)
	v.SetDefault("update.repo", DefaultUpdateRepo)
	v.SetDefault("update.updater_socket", "/run/updater/updater.sock")

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	// 没写进 YAML 的项也要能被环境变量覆盖（viper 只会为已知的 key 读取环境变量）
	// 其中 storage.driver 与 storage.s3.* 是旧版对象存储的环境变量，YAML 里已经没有这些项，仍需要绑定才能读到，
	// 否则升级时无法把它们导入为后台存储
	for _, key := range []string{
		"server.id_key", "database.allow_newer_schema", "update.updater_token", "ai.secret_key", "ai.allow_fake_ip", "storage.driver",
		"agent.enabled", "agent.runtime", "agent.url", "agent.bridge_url", "agent.node_path", "agent.runtime_dir", "agent.bridge_addr", "agent.service_token", "agent.work_dir", "agent.web_enabled", "agent.searxng_url", "agent.disabled_tools",
		"storage.s3.endpoint", "storage.s3.region", "storage.s3.bucket", "storage.s3.access_key", "storage.s3.secret_key",
		"storage.s3.use_ssl", "storage.s3.public_base_url", "storage.s3.path_prefix",
	} {
		_ = v.BindEnv(key)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	cfg.Agent = cfg.Agent.WithDefaults()
	if err := cfg.Agent.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}
