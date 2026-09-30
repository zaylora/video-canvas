package initialize

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"video-canvas/internal/cache"
	"video-canvas/internal/config"
	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
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

	hub        *ws.Hub        // WebSocket 连接管理，退出时需要显式关闭（Shutdown 不会等已劫持的连接）
	taskWorker *worker.Worker // 生成任务调度；配置里关闭时为 nil
	runnerCmd  *exec.Cmd
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

	// 画布
	canvasProjectRepo := repository.NewCanvasProjectRepository(db)
	canvasProjectSvc := service.NewCanvasProjectService(canvasProjectRepo)

	// 素材存储：本地磁盘（仅开发）或 S3 兼容（含阿里云 OSS）
	store, err := storage.New(cfg.Storage)
	if err != nil {
		closeRedis(rdb)
		closeDB(db)
		return nil, err
	}
	assetRepo := repository.NewAssetRepository(db)
	assetSvc := service.NewAssetService(assetRepo, store, cfg.Storage) // 同时是 provider.AssetStore 和 provider.AssetSaver
	var localFiles gin.HandlerFunc
	if ls, ok := store.(*storage.LocalStorage); ok {
		localFiles = storage.FileServer(ls)
	}

	// 实时推送：Redis 开启时 ticket 存 Redis，关闭时降级为进程内存
	hub := ws.NewHub()
	ticketStore := ws.NewTicketStore(rdb)

	// AI 配置（平台协议 / 模型 / 凭证）：service 同时是 provider.Registry 与 provider.SecretResolver
	aiConfigRepo := repository.NewAIConfigRepository(db)
	aiChannelRepo := repository.NewAIChannelRepository(db)
	aiPluginRepo := repository.NewAIPluginRepository(db)
	aiCfgSvc := service.NewAIConfigService(aiConfigRepo, aiChannelRepo, aiPluginRepo, cfg.AI.SecretKey)
	if cfg.AI.SecretKey == "" {
		logger.Warn("未配置 ai.secret_key（环境变量 APP_AI_SECRET_KEY），管理端无法设置或读取平台凭证，生成任务将无法提交")
	}
	runnerClient, runnerCmd, err := newPluginRunnerClient(cfg)
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
		LocalFiles:     localFiles,
		AIModel:        handler.NewAIModelHandler(aiCfgSvc),
		AdminAI:        handler.NewAdminAIHandler(aiCfgSvc),
		AdminRole:      roleLookup,
	})

	return &App{
		cfg:        cfg,
		db:         db,
		rdb:        rdb,
		hub:        hub,
		taskWorker: taskWorker,
		runnerCmd:  runnerCmd,
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

func newPluginRunnerClient(cfg *config.Config) (plugin.RunnerClient, *exec.Cmd, error) {
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
		cmd := exec.Command(executable, "plugin-runner",
			"--network", r.Network, "--address", r.Address,
			"--pool-size", fmt.Sprint(r.PoolSize), "--hook-timeout", r.HookTimeout.String())
		if r.MemoryLimit > 0 {
			cmd.Env = append(os.Environ(), fmt.Sprintf("GOMEMLIMIT=%dMiB", r.MemoryLimit))
		}
		if err := cmd.Start(); err != nil {
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
				return client, cmd, nil
			}
			select {
			case <-ctx.Done():
				_ = cmd.Process.Kill()
				return nil, nil, fmt.Errorf("等待 plugin-runner 就绪超时: %w", ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
	default:
		return plugin.NewHTTPRunnerClient(r.Network, r.Address), nil, nil
	}
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

func (a *App) close() {
	if a.runnerCmd != nil && a.runnerCmd.Process != nil {
		_ = a.runnerCmd.Process.Kill()
		_, _ = a.runnerCmd.Process.Wait()
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
