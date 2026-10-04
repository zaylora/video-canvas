package initialize

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"video-canvas/internal/cache"
	"video-canvas/internal/config"
	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/ws"
	"video-canvas/internal/provider"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/internal/provider/worker"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

type App struct {
	cfg    *config.Config
	db     *gorm.DB
	rdb    *redis.Client
	server *http.Server

	hub        *ws.Hub               // WebSocket 连接管理，退出时需要显式关闭（Shutdown 不会等已劫持的连接）
	taskWorker *worker.Worker        // 生成任务调度；配置里关闭时为 nil
	runnerStop func()                // 停止 plugin-runner 子进程的监督并结束进程；非 spawn 模式为 nil
	assets     *service.AssetService // 素材服务；后台定期用它清理过期未登记的直传对象
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
	userSvc := service.NewUserService(userRepo, userCache, cfg.JWT.Secret, cfg.JWT.Issuer, cfg.JWT.ExpireHours)

	// 对外 ID 编码：画布 ID 等以十六进制串暴露，库里主键不变
	idKey := cfg.Server.IDKey
	if idKey == "" {
		idKey = cfg.JWT.Secret
	}
	idcodec.Init(idKey)

	// 画布
	canvasProjectRepo := repository.NewCanvasProjectRepository(db)
	canvasProjectSvc := service.NewCanvasProjectService(canvasProjectRepo)

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
	// 图片处理服务：给素材所在存储配置的云厂商缩略图 / 视频封面；素材服务通过它解析 /files?v=thumb|poster
	processorSvc := service.NewImageProcessorService(repository.NewImageProcessorRepository(db), storageRepo, storeRegistry, service.NewHTTPProcessorFetcher())
	assetSvc.SetVariantResolver(processorSvc)

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
		Runner:  runnerClient,
		Codes:   pluginCodeStore{repo: aiPluginRepo},
		Secrets: aiCfgSvc,
		Assets:  assetSvc,
		Saver:   assetSvc,
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
	})

	// 配置服务反向依赖插件宿主与任务服务，所以组装完后再注入。
	aiCfgSvc.SetDryRunner(executor)
	aiCfgSvc.SetTestTaskCreator(taskSvc)

	// 插件与渠道管理：aiChannelRepo 同时负责审计日志；变更后经 aiCfgSvc 刷新 Registry
	aiPluginSvc := service.NewAIPluginService(aiPluginRepo, aiChannelRepo, aiChannelRepo, plugin.NewPrechecker(runnerClient), aiCfgSvc)
	aiChannelSvc := service.NewAIChannelService(aiChannelRepo, aiPluginRepo, aiCfgSvc, aiChannelRepo, executor, aiCfgSvc)
	// 内置插件按 key + version 登记（在 runner 就绪之后）；失败只记日志，不阻止启动
	regCtx, regCancel := context.WithTimeout(context.Background(), time.Minute)
	registerBuiltinPlugins(regCtx, aiPluginSvc)
	regCancel()

	var taskWorker *worker.Worker
	if cfg.AI.Worker.Enabled {
		taskWorker = worker.New(taskSvc, executor, provider.AssetSaver(assetSvc), taskSvc.KickChan(), worker.Options{
			Concurrency:      cfg.AI.Worker.Concurrency,
			Tick:             cfg.AI.Worker.Tick,
			BatchSize:        cfg.AI.Worker.BatchSize,
			Lease:            cfg.AI.Worker.Lease,
			MaxDownloadBytes: cfg.Storage.MaxResult,
			ShutdownTimeout:  cfg.Server.ShutdownTimeout,
		})
	}

	// 管理接口的角色查询：缓存 30 秒，提升角色后最多 30 秒生效
	roleLookup := middleware.NewCachedRoleLookup(func(ctx context.Context, id uint64) (string, error) {
		u, err := userRepo.GetByID(ctx, id)
		if errors.Is(err, repository.ErrNotFound) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return u.Role, nil
	}, 30*time.Second)

	engineHTTP := router.New(cfg.Server.Mode, cfg.JWT.Secret, router.Handlers{
		Health:         handler.NewHealthHandler(db, rdb),
		User:           handler.NewUserHandler(userSvc),
		CanvasProject:  handler.NewCanvasProjectHandler(canvasProjectSvc),
		GenerationTask: handler.NewGenerationTaskHandler(taskSvc),
		WS:             handler.NewWSHandler(ticketStore, hub, cfg.Server.AllowedOrigins),
		Asset:          handler.NewAssetHandler(assetSvc),
		Files:          handler.NewFilesHandler(assetSvc),
		AIModel:        handler.NewAIModelHandler(aiCfgSvc),
		AdminAI:        handler.NewAdminAIHandler(aiCfgSvc),
		AdminPlugin:    handler.NewAdminPluginHandler(aiPluginSvc),
		AdminChannel:   handler.NewAdminChannelHandler(aiChannelSvc),
		AdminStorage:   handler.NewAdminStorageHandler(storageSvc),
		AdminImageProc: handler.NewAdminImageProcessorHandler(processorSvc),
		AdminMe:        handler.NewAdminMeHandler(roleLookup),
		AdminRole:      roleLookup,
	})

	return &App{
		cfg:        cfg,
		db:         db,
		rdb:        rdb,
		hub:        hub,
		taskWorker: taskWorker,
		runnerStop: runnerStop,
		assets:     assetSvc,
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

	// 生成任务的调度循环：状态全在 DB 里，重启后自然接着处理；退出时先停 HTTP 再等在途任务处理完，最后才关库
	var workerDone chan struct{}
	workerCancel := func() {}
	if a.taskWorker != nil {
		var wctx context.Context
		wctx, workerCancel = context.WithCancel(context.Background())
		workerDone = make(chan struct{})
		go func() {
			defer close(workerDone)
			if err := a.taskWorker.Run(wctx); err != nil {
				logger.Error("生成任务 worker 异常退出", zap.Error(err))
			}
		}()
	}
	defer func() {
		workerCancel()
		if workerDone != nil {
			<-workerDone
		}
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

func (a *App) close() {
	if a.runnerStop != nil {
		a.runnerStop()
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
