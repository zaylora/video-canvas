// Package fx 定期拉取美元兑人民币汇率，失败保留最近成功快照。
package fx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/provider/netguard"
)

const endpoint = "https://api.frankfurter.dev/v1/latest?from=USD&to=CNY"
const interval = 6 * time.Hour
const timeout = 10 * time.Second
const maxBody = 64 << 10

// Store 只需写入成功快照，不读取或覆盖人工汇率。
type Store interface {
	SetMany(ctx context.Context, values map[string]string, actorID uint64) error
}

// Service 使用现有 netguard 出站策略，无环境代理，重定向仍校验 IP。
type Service struct {
	store  Store
	client *http.Client
	now    func() time.Time
}

// New 创建汇率定时服务。
func New(store Store, ipAllowed func(net.IP) bool) *Service {
	cfg := &netguard.Config{IPAllowed: ipAllowed}
	cfg.ApplyDefaults()
	return &Service{store: store, client: netguard.NewClient(netguard.NewTransport(cfg), nil, cfg.MaxRedirects), now: time.Now}
}

// Refresh 成功时一次性写入 last/date/fetched_at，任何失败不修改已有值。
func (s *Service) Refresh(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("拉取汇率失败：%w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("汇率源返回状态 %d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return err
	}
	if len(raw) > maxBody {
		return errors.New("汇率响应过大")
	}
	var result struct {
		Base  string             `json:"base"`
		Date  string             `json:"date"`
		Rates map[string]float64 `json:"rates"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return fmt.Errorf("解析汇率失败：%w", err)
	}
	rate := result.Rates["CNY"]
	if result.Base != "USD" || rate <= 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
		return errors.New("汇率响应没有合法的 USD/CNY")
	}
	if _, err := time.Parse("2006-01-02", result.Date); err != nil {
		return errors.New("汇率日期不合法")
	}
	return s.store.SetMany(ctx, map[string]string{model.SettingPricingFXLast: strconv.FormatFloat(rate, 'f', -1, 64), model.SettingPricingFXDate: result.Date, model.SettingPricingFXFetchedAt: s.now().UTC().Format(time.RFC3339)}, 0)
}

// Run 启动时拉取一次，之后每六小时重试；取消时结束，失败仅在此后台边界记日志。
func (s *Service) Run(ctx context.Context) {
	timer := time.NewTicker(interval)
	defer timer.Stop()
	for {
		if err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
			logger.Warn("汇率刷新失败，沿用最近成功值", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
	}
}
