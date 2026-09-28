package initialize

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"

	"video-canvas/internal/cache"
	"video-canvas/internal/config"
	"video-canvas/internal/handler"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
)

type App struct {
	cfg    *config.Config
	db     *gorm.DB
	rdb    *redis.Client
	server *http.Server
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

	engine := router.New(cfg.Server.Mode, cfg.JWT.Secret, router.Handlers{
		Health:        handler.NewHealthHandler(db, rdb),
		User:          handler.NewUserHandler(userSvc),
		CanvasProject: handler.NewCanvasProjectHandler(canvasProjectSvc),
	})

	return &App{
		cfg: cfg,
		db:  db,
		rdb: rdb,
		server: &http.Server{
			Addr:         cfg.Server.Addr(),
			Handler:      engine,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
		},
	}, nil
}

// Run 启动 HTTP 服务，ctx 取消（收到退出信号）后优雅关闭并释放资源。
func (a *App) Run(ctx context.Context) error {
	defer a.close()

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
	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := a.server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	logger.Info("http 服务已停止")
	return nil
}

func (a *App) close() {
	if a.rdb != nil {
		if err := a.rdb.Close(); err != nil {
			logger.Warn("关闭 redis 失败", zap.Error(err))
		}
	}
	closeDB(a.db)
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
