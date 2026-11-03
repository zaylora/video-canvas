package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/configbody"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
	"video-canvas/internal/service/pricingfake"
)

func TestPricingSettings_ValidationDefaultsAndAudit(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	f := pricingfake.New()
	s := NewPricingSettingsService(f, func() time.Time { return now })
	view, err := s.Get(ctx)
	if err != nil || view.CreditsPerYuan != nil || view.DefaultMarkupPct != 30 || view.USDCNY.Source != "none" {
		t.Fatalf("%+v %v", view, err)
	}
	fx := 6.6529
	for _, req := range []model.PricingSettingsReq{{CreditsPerYuan: 0}, {CreditsPerYuan: -1}, {CreditsPerYuan: 1.001}, {CreditsPerYuan: 1, DefaultMarkupPct: -1}, {CreditsPerYuan: 1, DefaultMarkupPct: 1001}, {CreditsPerYuan: 1, USDCNYOverride: floatPointer(0)}, {CreditsPerYuan: 1, USDCNYOverride: floatPointer(6.12345)}} {
		_, _, err := s.Update(ctx, 1, req, "", nil)
		var e *errcode.Error
		if !errors.As(err, &e) || e.Code != 10001 {
			t.Fatalf("%+v %v", req, err)
		}
	}
	req := model.PricingSettingsReq{CreditsPerYuan: 7.5, DefaultMarkupPct: 35, USDCNYOverride: &fx}
	view, id, err := s.Update(ctx, 7, req, "", nil)
	if err != nil || id != 1 || *view.CreditsPerYuan != 7.5 || view.DefaultMarkupPct != 35 || view.USDCNY.Source != "override" || view.USDCNY.Date != nil {
		t.Fatalf("%+v %d %v", view, id, err)
	}
	if len(f.Logs) != 1 || f.Logs[0].ActorID != 7 || f.Logs[0].Action != "settings.pricing" || !strings.Contains(string(f.Logs[0].DetailJSON), `"before"`) {
		t.Fatal(f.Logs)
	}
	// 审计失败会回滚设置，不产生仅配置成功的半次变更。
	f.AuditErr = errors.New("audit down")
	req.DefaultMarkupPct = 40
	if _, _, err := s.Update(ctx, 7, req, "", nil); err == nil {
		t.Fatal("审计失败应失败")
	}
	if f.Values[model.SettingPricingMarkup] != "35" {
		t.Fatal(f.Values)
	}
	f.AuditErr = nil
	if _, _, err := s.Update(ctx, 7, req, "old", nil); !errors.Is(err, repository.ErrRevisionConflict) {
		t.Fatalf("%v", err)
	}
	base := configbody.SHA256(PricingSettingsBody(view))
	detail := map[string]any{"via": "assistant", "approval_id": "99", "index": 0}
	_, id, err = s.Update(ctx, 7, req, base, detail)
	if err != nil {
		t.Fatal(err)
	}
	_, second, err := s.Update(ctx, 7, req, base, detail)
	if err != nil || second != id || len(f.Logs) != 2 {
		t.Fatalf("%d %v %+v", second, err, f.Logs)
	}
}

func floatPointer(v float64) *float64 { return &v }

func TestPricingSettingsRestoreUnsetSnapshot(t *testing.T) {
	f := pricingfake.New()
	s := NewPricingSettingsService(f, nil)
	ctx := context.Background()
	view, _, err := s.Update(ctx, 1, model.PricingSettingsReq{CreditsPerYuan: 7.5, DefaultMarkupPct: 35}, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(`{"credits_per_yuan":null,"default_markup_pct":30,"usd_cny_override":null}`)
	restored, _, err := s.Restore(ctx, 1, raw, configbody.SHA256(PricingSettingsBody(view)), map[string]any{"via": "assistant", "approval_id": "2", "index": 0})
	if err != nil || restored.CreditsPerYuan != nil || restored.DefaultMarkupPct != 30 {
		t.Fatalf("%+v %v", restored, err)
	}
}

func TestPricingSettingsFXSourceAndStale(t *testing.T) {
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	f := pricingfake.New()
	f.Values = map[string]string{model.SettingPricingFXLast: "7.1", model.SettingPricingFXDate: "2026-10-20", model.SettingPricingFXFetchedAt: now.Add(-8 * 24 * time.Hour).Format(time.RFC3339)}
	s := NewPricingSettingsService(f, func() time.Time { return now })
	view, err := s.Get(context.Background())
	if err != nil || view.USDCNY.Source != "live" || !view.USDCNY.Stale || *view.USDCNY.Rate != 7.1 {
		t.Fatalf("%+v %v", view, err)
	}
	f.Values[model.SettingPricingFXOverride] = "6.8"
	view, _ = s.Get(context.Background())
	if view.USDCNY.Source != "override" || view.USDCNY.Stale || view.USDCNY.Date != nil || view.USDCNY.FetchedAt == nil {
		t.Fatalf("%+v", view)
	}
	f.ReadErr = errors.New("read failed")
	if _, err := s.Get(context.Background()); !errors.Is(err, f.ReadErr) {
		t.Fatal(err)
	}
}
