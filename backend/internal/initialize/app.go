package initialize

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"video-canvas/internal/cache"
	"video-canvas/internal/config"
	"video-canvas/internal/handler"
	agenthandler "video-canvas/internal/handler/agent"
	"video-canvas/internal/mailer"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/internal/provider/queue"
	"video-canvas/internal/release"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
	agentsvc "video-canvas/internal/service/agent"
	"video-canvas/internal/storage"
	"video-canvas/internal/updater"
	"video-canvas/pkg/version"
)

type App struct {
	cfg    *config.Config
	db     *gorm.DB
	rdb    *redis.Client
	server *http.Server

	hub        *ws.Hub                  // WebSocket 连接管理，退出时需要显式关闭（Shutdown 不会等已劫持的连接）
	taskQueue  *queue.Runtime           // 生成任务队列（River）：本实例是否执行任务由 ai.worker.enabled 决定，但入队和后台操作总是可用
	queueConn  *pgxpool.Pool            // 队列用来 LISTEN 新任务通知的专用连接池，退出时关闭
	skills     *agentsvc.SkillService   // Agent 技能库；后台周期清理过期暂存和待删除的版本
	agent      *agentRuntime            // 画布 Agent 的桥和运行进程；agent.enabled=false 时为 nil
	runnerStop func()                   // 停止 plugin-runner 子进程的监督并结束进程；非 spawn 模式为 nil
	assets     *service.AssetService    // 素材服务；后台定期用它清理过期未登记的直传对象
	loginLogs  *service.LoginLogCleaner // 登录记录清理器；后台每天清理超过保留天数的记录
	observe    *agentsvc.ObserveService // Agent 运行观测；后台每天清理超过 30 天的工具参数和结果
	payment    *service.PaymentService  // 积分充值；后台每 30 秒补单、处理超时订单
}

// NewApp 初始化基础设施并手动组装依赖：repository -> service -> handler -> router。
func NewApp(cfg *config.Config) (*App, error) {
	db, err := NewDB(cfg.Database)
	if err != nil {
		return nil, err
	}
	rdb, err := NewRedis(cfg.Redis)
	if err != nil {
		closeDB(db)
		return nil, err
	}

	// 用户
	userRepo := repository.NewUserRepository(db)
	userCache := cache.NewUserCache(rdb)
	// 注册 / 系统设置 / 邮件：设置库值优先、config 兜底；SMTP 密码复用 ai_secrets 的加密（APP_AI_SECRET_KEY），
	// 发信与主机校验都经 netguard，拒绝内网 / 回环 / 链路本地
	auditRepo := repository.NewAdminAuditRepository(db)
	settingsSvc := service.NewSettingsService(repository.NewSystemSettingRepository(db), auditRepo)
	smtpSvc := service.NewSMTPService(repository.NewSMTPSettingRepository(db), mailer.NewSMTPSender(nil), auditRepo, cfg.AI.SecretKey, service.NewNetguardHostChecker())
	userSvc := service.NewUserService(service.UserDeps{
		Repo: userRepo, Cache: userCache, Codes: cache.NewRegisterCodeStore(rdb), Policy: settingsSvc, Mail: smtpSvc,
		JWTSecret: cfg.JWT.Secret, JWTIssuer: cfg.JWT.Issuer, JWTExpireHours: cfg.JWT.ExpireHours,
		Debug: cfg.Server.Mode == "debug", // 仅 debug 模式把验证码打到日志
	})

	// 对外 ID 编码：画布 ID 等以十六进制串暴露，库里主键不变
	idKey := cfg.Server.IDKey
	if idKey == "" {
		idKey = cfg.JWT.Secret
	}
	idcodec.Init(idKey)

	// 画布
	canvasProjectRepo := repository.NewCanvasProjectRepository(db)

	// AI 配置（平台协议 / 模型 / 凭证）：service 同时是 provider.Registry 与 provider.SecretResolver
	aiConfigRepo := repository.NewAIConfigRepository(db)
	aiChannelRepo := repository.NewAIChannelRepository(db)
	aiPluginRepo := repository.NewAIPluginRepository(db)
	aiCfgSvc := service.NewAIConfigService(aiConfigRepo, aiChannelRepo, aiPluginRepo, cfg.AI.SecretKey)
	if cfg.AI.SecretKey == "" {
		logger.Warn("未配置 ai.secret_key（环境变量 APP_AI_SECRET_KEY），管理端无法设置或读取平台凭证，生成任务将无法提交")
	}

	// 素材存储：存储配置在数据库里（后台“存储配置”管理），密钥复用 AI 配置的加密存储（ai_secrets），
	// 所以 AI 配置服务要先建。Registry 按 id 解析存储并缓存客户端；写入用默认存储，读取按素材记录的 storage_id。
	storageRepo := repository.NewStorageConfigRepository(db)
	storageSvc := service.NewStorageConfigService(storageRepo, aiCfgSvc, aiChannelRepo, nil, cfg.Storage)
	if err := bootstrapStorage(db, storageSvc, cfg.Storage); err != nil {
		closeRedis(rdb)
		closeDB(db)
		return nil, err
	}
	storeRegistry := storage.NewRegistry(storageSvc, 30*time.Second)
	storageSvc.SetInvalidator(storeRegistry)
	assetRepo := repository.NewAssetRepository(db)
	assetSvc := service.NewAssetService(assetRepo, storeRegistry, cfg.Storage) // 同时是 provider.AssetStore 和 provider.AssetSaver
	assetSvc.SetUploadIntents(repository.NewUploadIntentRepository(db))        // 开启浏览器直传
	// 画布列表的封面：素材 key 换成稳定的 /files 地址并带 v=thumb 取缩略图（没有处理服务时服务端回退到原图）
	canvasProjectSvc := service.NewCanvasProjectService(canvasProjectRepo,
		service.WithCoverURL(func(key string) string { return assetSvc.FileURL(key) + "?v=thumb" }))
	// 图片处理服务：给素材所在存储配置的云厂商缩略图 / 视频封面；素材服务通过它解析 /files?v=thumb|poster
	processorSvc := service.NewImageProcessorService(repository.NewImageProcessorRepository(db), storageRepo, storeRegistry, service.NewHTTPProcessorFetcher())
	assetSvc.SetVariantResolver(processorSvc)
	assetSvc.SetAvatarLocator(userRepo) // /files/avatars/... 从用户表反查头像所在的存储

	// 登录页展示：条目引用素材表里的视频 / 封面，地址复用素材服务的稳定地址（/files/<key>），设置走 system_settings 键值表；
	// 后台素材库列表另需生成任务（带出提示词 / 模型名）和用户名（显示作者）
	showcaseSvc := service.NewShowcaseService(service.ShowcaseDeps{
		Items: repository.NewShowcaseRepository(db), Assets: assetRepo, Views: assetSvc,
		Tasks: repository.NewGenerationTaskRepository(db), Users: userRepo,
		Settings: repository.NewSystemSettingRepository(db), Audit: auditRepo,
	})

	// 实时推送：Redis 开启时 ticket 存 Redis，关闭时降级为进程内存
	hub := ws.NewHub()
	ticketStore := ws.NewTicketStore(rdb)

	runnerClient, runnerStop, err := newPluginRunnerClient(cfg)
	if err != nil {
		closeRedis(rdb)
		closeDB(db)
		return nil, err
	}
	executor := plugin.New(plugin.Options{
		Runner:    runnerClient,
		Codes:     pluginCodeStore{repo: aiPluginRepo},
		Secrets:   aiCfgSvc,
		Assets:    assetSvc,
		Saver:     assetSvc,
		IPAllowed: outboundIPPolicy(cfg),
	})

	// 生成任务：提交 / 对账 / 取消 / 积分事务
	taskRepo := repository.NewGenerationTaskRepository(db)
	taskSvc := service.NewGenerationTaskService(service.GenerationTaskDeps{
		Repo:        taskRepo,
		Registry:    aiCfgSvc,
		Executor:    executor,
		Assets:      assetSvc,
		Broadcaster: hub,
		Config:      cfg.AI,
		Limits:      service.NewTaskLimitService(settingsSvc, userRepo), // 初始积分 / 并发上限：单用户覆盖 > 系统设置 > config
		Covers:      canvasProjectRepo,                                  // 任务成功时把第一张图写成画布封面
	})

	// 配置服务反向依赖插件宿主与任务服务，所以组装完后再注入。
	aiCfgSvc.SetDryRunner(executor)
	aiCfgSvc.SetTestTaskCreator(taskSvc)

	// 画布 Agent：模型清单来自后台已发布的 agent 类型模型；运行时按 agent.enabled 装配，没启用时是占位（发起运行返回 60005）
	agentRepo := repository.NewAgentRepository(db)
	// Agent 技能库：整包存在素材所用的对象存储里（key 前缀 agent-skills/，不会被 /files 公开路由读到：那里只认有素材记录的 key）
	skillSvc := agentsvc.NewSkillService(repository.NewAgentSkillRepository(db), storeRegistry, auditRepo, agentsvc.SkillOptions{})
	agentCanvasSvc := agentsvc.NewAgentCanvasService(agentRepo, hub)
	// Agent 运行观测：工具调用逐条留存、运行摘要、用户反馈、后台只读导出。Harness 版本在创建运行时记下，改了提示词或工具后才能对比效果
	observeRepo := repository.NewAgentObserveRepository(db)
	buildSHA := version.GitCommit
	if buildSHA == "none" {
		buildSHA = "" // 没有注入构建号（本地 go run）：不记，免得把占位文字当成真实的 commit
	}
	agentSvc := agentsvc.NewAgentService(agentsvc.AgentDeps{
		Harness: agentsvc.CurrentHarness(buildSHA),
		Repo:    agentRepo, Canvas: agentCanvasSvc,
		Models: agentsvc.NewRegistryAgentModels(aiCfgSvc), Runtime: agentsvc.NoAgentRuntime{}, Broadcaster: hub, Registry: aiCfgSvc, Skills: skillSvc,
		Generator: agentsvc.NewAgentGenerator(taskSvc, agentCanvasSvc, aiCfgSvc), // 批准生成后创建任务并绑定到节点
	})
	// 上次服务退出时还停在 queued / running 的运行：它们的进程随旧服务一起没了，不会再有人推进，却占着画布。
	// 不管有没有启用运行时都要做，否则关掉功能后画布仍被占着
	if n, err := agentRepo.InterruptActiveRuns(context.Background()); err != nil {
		stopRunner(runnerStop)
		closeRedis(rdb)
		closeDB(db)
		return nil, fmt.Errorf("恢复被中断的 Agent 运行失败：%w", err)
	} else if n > 0 {
		logger.Warn("上次退出时有 Agent 运行没有结束，已标为中断，用户可以点「继续」", zap.Int64("runs", n))
	}
	agentRT, err := newAgentRuntime(cfg, agentDeps{tasks: taskSvc, assets: assetSvc, frames: newVideoFrames(), frameStore: assetFrameStore{assetSvc}, repo: agentRepo, agent: agentSvc, canvas: agentCanvasSvc, registry: aiCfgSvc, secrets: aiCfgSvc, skills: skillSvc, toolCalls: observeRepo, modelRequests: observeRepo, ipAllowed: outboundIPPolicy(cfg), broadcaster: hub})
	if err != nil {
		stopRunner(runnerStop)
		closeRedis(rdb)
		closeDB(db)
		return nil, err
	}

	// 插件与渠道管理：aiChannelRepo 同时负责审计日志；变更后经 aiCfgSvc 刷新 Registry
	aiPluginSvc := service.NewAIPluginService(aiPluginRepo, aiCfgSvc, aiChannelRepo, plugin.NewPrechecker(runnerClient), aiCfgSvc)
	aiChannelSvc := service.NewAIChannelService(aiChannelRepo, aiCfgSvc, aiCfgSvc, executor, aiChannelRepo, aiCfgSvc)
	// 内置插件按 key + version 登记（在 runner 就绪之后）；失败只记日志，不阻止启动
	regCtx, regCancel := context.WithTimeout(context.Background(), time.Minute)
	registerBuiltinPlugins(regCtx, aiPluginSvc)
	regCancel()

	// 生成任务队列（River）：任务调度、并发、重试之外的实例心跳与救回都交给它；业务处理仍是 worker。
	// 入队和后台操作总是可用，是否在本实例执行任务由 ai.worker.enabled 决定（API-only 实例关闭它）。
	taskQueue, queueConn, err := newTaskQueue(cfg, db, taskSvc, executor, assetSvc, settingsSvc)
	if err != nil {
		stopRunner(runnerStop)
		closeRedis(rdb)
		closeDB(db)
		return nil, err
	}
	taskRepo.SetEnqueuer(taskQueue) // 创建任务的事务里入队：任务、积分冻结、job 一起提交
	riverUI, err := taskQueue.UIHandler(riverUIPrefix)
	if err != nil {
		queueConn.Close()
		stopRunner(runnerStop)
		closeRedis(rdb)
		closeDB(db)
		return nil, err
	}

	// 个人中心：头像写默认存储；改密码复用 token_version 失效机制，并断开全部 WS、给当前设备续签
	meSvc := service.NewMeService(service.MeDeps{
		Repo: userRepo, Canvases: canvasProjectRepo, Stores: storeRegistry,
		Limiter: cache.NewPasswordFailLimiter(rdb), // Redis 未启用时降级为进程内计数（单实例部署可接受）
		Users:   userSvc, Conns: hub, Tokens: userSvc,
		FileBaseURL: cfg.Storage.Local.BaseURL,
	})

	// 积分充值与支付：渠道密钥复用 AI 配置的加密存储，入账走积分账户的同一套行锁与流水；回调、补单、后台管理都在这里装配
	pay := wirePayment(db, hub, aiCfgSvc, settingsSvc, auditRepo, outboundIPPolicy(cfg))

	// 鉴权用的用户状态查询：RequireActive 与 RequireAdmin / RequireSuperAdmin 共用同一套（Redis 缓存，无 Redis 直接查库）。
	// 封禁、改角色、重置密码时由 UserService.InvalidateUser 主动删缓存，所以不再需要进程内的角色缓存
	stateLookup := func(ctx context.Context, id uint64) (*middleware.UserState, error) {
		u, err := userSvc.State(ctx, id)
		if err != nil || u == nil {
			return nil, err
		}
		return &middleware.UserState{Role: u.Role, Status: u.Status, TokenVersion: u.TokenVersion}, nil
	}
	roleLookup := func(ctx context.Context, id uint64) (string, error) {
		st, err := stateLookup(ctx, id)
		if err != nil || st == nil {
			return "", err
		}
		return st.Role, nil
	}

	// 观测服务依赖角色查询（分析令牌的签发人必须仍是管理员），所以在 roleLookup 之后装配，再绑定给 AgentService 做摘要刷新
	observeSvc := agentsvc.NewObserveService(agentsvc.ObserveDeps{
		Repo: observeRepo, Runs: agentRepo, Events: agentSvc, Audit: auditRepo, Roles: roleLookup, Remote: AgentRemoteRunsFor(cfg.Agent),
	})
	agentSvc.SetObserver(observeSvc)
	if agentRT != nil && agentRT.rt != nil {
		agentRT.rt.SetTailSink(observeSvc) // 运行进程退出时把 stderr 末尾留进运行摘要
	}

	agentProxy, err := AgentProxyFor(cfg.Agent)
	if err != nil {
		stopRunner(runnerStop)
		closeRedis(rdb)
		closeDB(db)
		return nil, err
	}
	engineHTTP := router.New(cfg.Server.Mode, cfg.JWT.Secret, router.Handlers{
		AgentProxy:    agentProxy,
		Health:        handler.NewHealthHandler(db, rdb),
		User:          handler.NewUserHandler(userSvc),
		Me:            handler.NewMeHandler(meSvc),
		CanvasProject: handler.NewCanvasProjectHandler(canvasProjectSvc),
		Conversation:  handler.NewConversationHandler(service.NewConversationService(repository.NewConversationRepository(db), taskSvc)),
		Agent:         agenthandler.NewAgentHandler(agentSvc),
		AgentSkill:    agenthandler.NewSkillHandler(skillSvc),
		AgentFeedback: agenthandler.NewAgentFeedbackHandler(observeSvc),
		AgentObserve:  agenthandler.NewAgentObserveHandler(observeSvc),
		AnalysisToken: func(ctx context.Context, plain string) (uint64, uint64, error) {
			who, err := observeSvc.AuthenticateToken(ctx, plain)
			if err != nil {
				return 0, 0, err
			}
			return who.TokenID, who.UserID, nil
		},
		GenerationTask: handler.NewGenerationTaskHandler(taskSvc),
		WS:             handler.NewWSHandler(ticketStore, hub, cfg.Server.AllowedOrigins),
		Asset:          handler.NewAssetHandler(assetSvc),
		AssetLibrary: handler.NewAssetLibraryHandler(service.NewAssetLibraryService(service.AssetLibraryDeps{
			Folders: repository.NewAssetFolderRepository(db), Items: repository.NewAssetLibraryRepository(db),
			Assets: assetRepo, Canvases: canvasProjectRepo, Views: assetSvc,
		})),
		CharacterCard: handler.NewCharacterCardHandler(service.NewCharacterCardService(service.CharacterCardDeps{
			Cards: repository.NewCharacterCardRepository(db), Assets: assetRepo, Views: assetSvc,
		})),
		Files:        handler.NewFilesHandler(assetSvc),
		AIModel:      handler.NewAIModelHandler(aiCfgSvc),
		AdminAI:      handler.NewAdminAIHandler(aiCfgSvc),
		AdminCatalog: handler.NewAdminCatalogHandler(service.NewAICatalogService(aiChannelSvc, aiPluginSvc, aiCfgSvc)),
		AdminPlugin:  handler.NewAdminPluginHandler(aiPluginSvc),
		AdminChannel: handler.NewAdminChannelHandler(aiChannelSvc, aiCfgSvc),
		AdminStats:   handler.NewAdminStatsHandler(service.NewAIStatsService(repository.NewAIStatsRepository(db), aiCfgSvc)),
		AdminQueue: handler.NewAdminQueueHandler(service.NewQueueAdminService(
			repository.NewQueueRepository(db), aiChannelRepo, settingsSvc, taskQueue, auditRepo)),
		// River UI 能重试和删除 job，只许仍然启用的 super_admin 打开：停用的账号即使 cookie 还没过期也立即失效
		RiverUI: handler.NewRiverUIHandler(riverUI, ticketStore, func(ctx context.Context, id uint64) (string, error) {
			st, err := stateLookup(ctx, id)
			if err != nil || st == nil || st.Status != model.UserStatusActive {
				return "", err
			}
			return st.Role, nil
		}, cfg.JWT.Secret, riverUIPrefix),
		AdminStorage:   handler.NewAdminStorageHandler(storageSvc),
		AdminImageProc: handler.NewAdminImageProcessorHandler(processorSvc),
		AdminMe:        handler.NewAdminMeHandler(roleLookup),
		AdminUser: handler.NewAdminUserHandler(service.NewAdminUserService(userRepo, auditRepo, settingsSvc, service.WithAdminControls(service.AdminControls{
			Credits: taskRepo, // 积分调整与任务冻结 / 结算共用 LockCredit 行锁
			Users:   userSvc,  // 封禁 / 启用 / 改并发后清用户状态缓存
			Tasks:   taskSvc,  // 封禁并取消任务：复用任务服务的取消与退款流程
			Conns:   hub,      // 封禁后断开该用户全部 WebSocket
		}))),
		AdminUsage:    handler.NewAdminUsageHandler(service.NewAdminUsageService(repository.NewAdminUsageRepository(db))),
		AdminLogs:     handler.NewAdminLogHandler(service.NewAuditLogService(auditRepo)),
		AdminSettings: handler.NewAdminSettingsHandler(settingsSvc, smtpSvc),
		AdminSystem:   newAdminSystemHandler(cfg, db, taskRepo, auditRepo),
		Showcase:      handler.NewShowcaseHandler(showcaseSvc),
		Payment:       pay.user,
		AdminPayment:  pay.admin,
		AdminRole:     roleLookup,
		UserState:     stateLookup,
	})

	return &App{
		cfg:        cfg,
		db:         db,
		rdb:        rdb,
		hub:        hub,
		agent:      agentRT,
		skills:     skillSvc,
		taskQueue:  taskQueue,
		queueConn:  queueConn,
		runnerStop: runnerStop,
		assets:     assetSvc,
		loginLogs:  service.NewLoginLogCleaner(userRepo),
		observe:    observeSvc,
		payment:    pay.svc,
		server: &http.Server{
			Addr:         cfg.Server.Addr(),
			Handler:      engineHTTP,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
		},
	}, nil
}

type pluginCodeStore struct {
	repo *repository.AIPluginRepository
}

func (s pluginCodeStore) Code(ctx context.Context, versionID uint64, _ string) (string, error) {
	version, err := s.repo.GetVersion(ctx, versionID)
	if err != nil {
		return "", err
	}
	return version.Code, nil
}

// newPluginRunnerClient 按配置创建 runner 客户端。spawn 模式下还会拉起子进程，并返回停止函数：
// 子进程崩溃（如内存超限被杀）后由监督循环按指数退避自动重启，否则任务会一直 pending。
func newPluginRunnerClient(cfg *config.Config) (plugin.RunnerClient, func(), error) {
	r := cfg.AI.PluginRunner
	switch r.Mode {
	case "inprocess":
		return plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{
			PoolSize:       r.PoolSize,
			DefaultTimeout: r.HookTimeout,
		}).Handler()), nil, nil
	case "spawn":
		executable, err := os.Executable()
		if err != nil {
			return nil, nil, fmt.Errorf("定位 plugin-runner 可执行文件失败: %w", err)
		}
		spawn := func() (pluginrunner.Process, error) {
			cmd := exec.Command(executable, "plugin-runner",
				"--network", r.Network, "--address", r.Address,
				"--pool-size", fmt.Sprint(r.PoolSize), "--hook-timeout", r.HookTimeout.String())
			if r.MemoryLimit > 0 {
				cmd.Env = append(os.Environ(), fmt.Sprintf("GOMEMLIMIT=%dMiB", r.MemoryLimit))
			}
			if err := cmd.Start(); err != nil {
				return nil, err
			}
			return cmdProcess{cmd}, nil
		}
		first, err := spawn()
		if err != nil {
			return nil, nil, fmt.Errorf("启动 plugin-runner 失败: %w", err)
		}
		client := plugin.NewHTTPRunnerClient(r.Network, r.Address)
		wait := r.StartupWait
		if wait <= 0 {
			wait = 15 * time.Second
		}
		ctx, cancel := context.WithTimeout(context.Background(), wait)
		defer cancel()
		for {
			if err := client.Ready(ctx); err == nil {
				break
			}
			select {
			case <-ctx.Done():
				_ = first.Kill()
				_ = first.Wait()
				return nil, nil, fmt.Errorf("等待 plugin-runner 就绪超时: %w", ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
		// 就绪之后交给监督循环：进程退出就重启；停止函数取消监督、杀掉当前进程并等循环结束
		superCtx, superCancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			pluginrunner.Supervise(superCtx, first, pluginrunner.SupervisorOptions{
				Start: spawn,
				OnExit: func(err error, lived, delay time.Duration) {
					logger.Error("plugin-runner 进程退出，将自动重启",
						zap.Error(err), zap.Duration("lived", lived), zap.Duration("restart_in", delay))
				},
				OnStartError: func(err error, delay time.Duration) {
					logger.Error("重启 plugin-runner 失败，稍后再试", zap.Error(err), zap.Duration("retry_in", delay))
				},
			})
		}()
		return client, func() { superCancel(); <-done }, nil
	default:
		return plugin.NewHTTPRunnerClient(r.Network, r.Address), nil, nil
	}
}

// cmdProcess 把 *exec.Cmd 适配成可监督的进程。
type cmdProcess struct{ cmd *exec.Cmd }

func (p cmdProcess) Wait() error { return p.cmd.Wait() }

func (p cmdProcess) Kill() error {
	if p.cmd.Process == nil {
		return nil
	}
	return p.cmd.Process.Kill()
}

// Run 启动 HTTP 服务，ctx 取消（收到退出信号）后优雅关闭并释放资源。
func (a *App) Run(ctx context.Context) error {
	defer a.close()

	// 生成任务队列：状态全在 DB 里，重启后自然接着处理（对账会把没有 job 的任务补上）；退出时先停 HTTP 再等在途任务处理完，最后才关库
	queueCtx, queueCancel := context.WithCancel(context.Background())
	queueDone := make(chan struct{})
	go func() {
		defer close(queueDone)
		if err := a.taskQueue.Run(queueCtx); err != nil {
			logger.Error("生成任务队列异常退出", zap.Error(err))
		}
	}()
	defer func() {
		queueCancel()
		<-queueDone
	}()

	// 过期未登记的直传对象清理：和 worker 一样先于关库停止
	cleanCtx, cleanCancel := context.WithCancel(context.Background())
	cleanDone := make(chan struct{})
	go func() {
		defer close(cleanDone)
		a.cleanupUploads(cleanCtx)
	}()
	defer func() {
		cleanCancel()
		<-cleanDone
	}()

	// 登录记录清理：同样先于关库停止
	loginCtx, loginCancel := context.WithCancel(context.Background())
	loginDone := make(chan struct{})
	go func() {
		defer close(loginDone)
		a.cleanupLoginLogs(loginCtx)
	}()
	defer func() {
		loginCancel()
		<-loginDone
	}()

	// Agent 观测大字段清理：先于关库停止
	observeCtx, observeCancel := context.WithCancel(context.Background())
	observeDone := make(chan struct{})
	go func() {
		defer close(observeDone)
		a.cleanupObserve(observeCtx)
	}()
	defer func() {
		observeCancel()
		<-observeDone
	}()

	// durable 模式：把 Agent 服务里变过的运行汇总进摘要表（没有配置 Agent 服务时立即返回）：先于关库停止
	syncCtx, syncCancel := context.WithCancel(context.Background())
	syncDone := make(chan struct{})
	go func() {
		defer close(syncDone)
		a.observe.RunRemoteSync(syncCtx, observeSyncInterval)
	}()
	defer func() {
		syncCancel()
		<-syncDone
	}()

	// 技能库清理：先于关库停止
	skillCtx, skillCancel := context.WithCancel(context.Background())
	skillDone := make(chan struct{})
	go func() {
		defer close(skillDone)
		a.skills.RunSweeper(skillCtx, skillSweepInterval)
	}()
	defer func() {
		skillCancel()
		<-skillDone
	}()

	// 支付补单：先于关库停止，和其它后台任务一样
	payCtx, payCancel := context.WithCancel(context.Background())
	payDone := make(chan struct{})
	go func() {
		defer close(payDone)
		a.runPayment(payCtx)
	}()
	defer func() {
		payCancel()
		<-payDone
	}()

	if a.agent != nil {
		go a.agent.serve()
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http 服务已启动", zap.String("name", a.cfg.Server.Name), zap.String("addr", a.server.Addr))
		if err := a.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	case <-ctx.Done():
	}

	logger.Info("正在关闭 http 服务")
	// http.Server.Shutdown 不会等已劫持的 WebSocket 连接，必须显式关闭 Hub，否则会一直等到超时
	a.hub.Close()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	logger.Info("http 服务已停止")
	return nil
}

// skillSweepInterval 是清理技能库垃圾（过期暂存、待删除版本）的间隔。
const skillSweepInterval = 5 * time.Minute

// uploadCleanupInterval 是清理过期直传对象的间隔；意图有效期 15 分钟，每 5 分钟扫一次足够及时。
const uploadCleanupInterval = 5 * time.Minute

// cleanupUploads 周期性删除“申请了直传却一直没登记”的对象与意图，直到 ctx 取消。
// 多实例同时跑也安全：删除不存在的对象与意图都不算错误。
func (a *App) cleanupUploads(ctx context.Context) {
	ticker := time.NewTicker(uploadCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := a.assets.CleanupUploads(ctx, 200); err != nil {
				logger.Error("清理过期直传失败", zap.Error(err))
			} else if n > 0 {
				logger.Info("已清理过期未登记的直传对象", zap.Int("count", n))
			}
		}
	}
}

// loginLogCleanupInterval 是清理过期登录记录的间隔：记录按天保留，每天扫一次足够。
const loginLogCleanupInterval = 24 * time.Hour

// loginLogCleanupMaxDelay 是启动后首次清理前的随机延迟上限：错开启动高峰，多实例同时启动时也不会同一秒一起删。
const loginLogCleanupMaxDelay = 5 * time.Minute

// cleanupLoginLogs 启动后延迟随机一小段时间清理一次，之后每天一次，直到 ctx 取消。
// 失败只记日志，下一轮会继续清；不影响服务。
func (a *App) cleanupLoginLogs(ctx context.Context) {
	delay := time.Minute + time.Duration(rand.Int64N(int64(loginLogCleanupMaxDelay-time.Minute))) //nolint:gosec // 只是错峰用的随机延迟，不需要密码学随机
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			a.cleanupLoginLogsOnce(ctx)
			timer.Reset(loginLogCleanupInterval)
		}
	}
}

// cleanupLoginLogsOnce 执行一轮清理并记录结果。
func (a *App) cleanupLoginLogsOnce(ctx context.Context) {
	n, err := a.loginLogs.Cleanup(ctx)
	if err != nil {
		logger.Error("清理过期登录记录失败", zap.Error(err), zap.Int64("deleted", n))
		return
	}
	if n > 0 {
		logger.Info("已清理过期登录记录", zap.Int64("count", n))
	}
}

// observeSyncInterval 是汇总 Agent 服务里变过的运行的间隔：后台运行记录比运行结束晚几秒出现，可以接受。
const observeSyncInterval = 10 * time.Second

// observeCleanupInterval 是清理过期观测大字段的间隔：按天保留，每天扫一次足够。
const observeCleanupInterval = 24 * time.Hour

// cleanupObserve 启动后延迟随机一小段时间清理一次，之后每天一次，直到 ctx 取消。
// 工具调用的参数和结果、运行摘要里的 runtime_tail 保留 30 天，模型请求体只保留 7 天；失败只记日志，下一轮会继续，不影响服务。
func (a *App) cleanupObserve(ctx context.Context) {
	delay := time.Minute + time.Duration(rand.Int64N(int64(loginLogCleanupMaxDelay-time.Minute))) //nolint:gosec // 只是错峰用的随机延迟，不需要密码学随机
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			tools, digests, err := a.observe.PurgeExpired(ctx)
			if err != nil {
				logger.Error("清理过期的 Agent 观测数据失败", zap.Error(err))
			} else if tools > 0 || digests > 0 {
				logger.Info("已清理过期的 Agent 观测数据", zap.Int64("tool_calls", tools), zap.Int64("digests", digests))
			}
			bodies, err := a.observe.PurgeExpiredRequestBodies(ctx)
			if err != nil {
				logger.Error("清理过期的 Agent 模型请求体失败", zap.Error(err))
			} else if bodies > 0 {
				logger.Info("已清理过期的 Agent 模型请求体", zap.Int64("bodies", bodies))
			}
			timer.Reset(observeCleanupInterval)
		}
	}
}

func (a *App) close() {
	// 画布 Agent 的进程要先于关库停：它们退出后的状态修正（标为中断）还要写库
	if a.agent != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		a.agent.stop(ctx)
		cancel()
	}
	if a.runnerStop != nil {
		a.runnerStop()
	}
	if a.queueConn != nil {
		a.queueConn.Close()
	}
	if a.rdb != nil {
		if err := a.rdb.Close(); err != nil {
			logger.Warn("关闭 redis 失败", zap.Error(err))
		}
	}
	closeDB(a.db)
}

func closeRedis(rdb *redis.Client) {
	if rdb != nil {
		_ = rdb.Close()
	}
}

func closeDB(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		return
	}
	if err := sqlDB.Close(); err != nil {
		logger.Warn("数据库关闭失败", zap.Error(err))
	}
}

// newAdminSystemHandler 组装版本面板：检查新版本、迁移状态、一键升级。
// 没配置 updater token 时不创建 updater 客户端，升级面板显示「未启用」和手动升级步骤。
func newAdminSystemHandler(cfg *config.Config, db *gorm.DB, tasks *repository.GenerationTaskRepository, audit *repository.AdminAuditRepository) *handler.AdminSystemHandler {
	migrations := repository.NewMigrationStatusRepository(db)
	updates := service.NewSystemUpdateService(service.SystemUpdateOptions{
		CurrentVersion: version.Version,
		CheckEnabled:   cfg.Update.CheckEnabled,
		Releases:       release.NewGitHubClient(cfg.Update.Repo, release.Options{UserAgent: "video-canvas/" + version.Version}),
		Migrations:     migrations,
	})
	opts := service.SystemUpgradeOptions{
		CurrentVersion: version.Version,
		Tasks:          tasks,
		Checker:        updates,
		Migrations:     migrations,
		Audit:          audit,
	}
	// 不能把 nil 的 *updater.Client 赋给接口：那样接口不为 nil，service 会以为启用了
	if cfg.Update.UpdaterToken != "" {
		opts.Updater = updater.NewClient("unix", cfg.Update.UpdaterSocket, cfg.Update.UpdaterToken)
	}
	return handler.NewAdminSystemHandler(updates, service.NewSystemUpgradeService(opts))
}

// bootstrapStorage 在启动时准备存储配置，必须在任何素材读写之前完成：
//  1. 升级前用环境变量配置了 S3 的部署，先把它导入为后台存储，旧素材才能继续从对象存储读取；
//  2. 确保内置的本地磁盘存储存在（全新部署的默认存储），并把还没记录存储的旧素材回填给它。
//
// 失败会让启动中止：没有默认存储，所有上传都会失败，不如启动时就报清楚。
func bootstrapStorage(db *gorm.DB, svc *service.StorageConfigService, cfg config.Storage) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if cfg.Driver == "s3" {
		if _, err := svc.ImportLegacyS3(ctx, cfg); err != nil {
			return fmt.Errorf("导入旧版 S3 存储配置失败: %w", err)
		}
	}
	if _, err := repository.EnsureBuiltinStorage(ctx, db); err != nil {
		return err
	}
	return nil
}

// stopRunner 停掉 plugin-runner 的监督；非 spawn 模式没有它。
func stopRunner(stop func()) {
	if stop != nil {
		stop()
	}
}
