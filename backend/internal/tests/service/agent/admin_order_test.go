package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sync"
	"testing"
	"time"

	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	. "video-canvas/internal/service/agent"
)

type adminOrderAudit struct {
	*adminFake
	mu         sync.Mutex
	orders     []string
	collisions int
	err        error
}

func (f *adminOrderAudit) ChangeAudits(_ context.Context, order string) ([]repository.AssistantAuditRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.orders = append(f.orders, order)
	if f.err != nil {
		return nil, f.err
	}
	if f.collisions > 0 {
		f.collisions--
		return []repository.AssistantAuditRecord{{ID: 1}}, nil
	}
	return nil, nil
}

func adminOrderService(f *adminOrderAudit) *AdminAssistantService {
	return NewAdminAssistantService(AdminAssistantDeps{Models: f.adminFake, Writer: f.adminFake, Users: f.adminFake, Audit: f, Settings: service.NewPricingSettingsService(f.adminFake, nil), Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }})
}

func adminOrderRequest(f *adminFake) AdminChangeRequest {
	return AdminChangeRequest{Title: "改价", Reason: "要求", Items: []AdminChangeInput{relativeInput(f, "a")}}
}

func TestAdminOrderShortAndAuditCollision(t *testing.T) {
	f := &adminOrderAudit{adminFake: newAdminFake(), collisions: 2}
	svc := adminOrderService(f)
	p, err := svc.BuildChanges(context.Background(), adminOrderRequest(f.adminFake))
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^CO-261102-[0-9A-F]{6}$`).MatchString(p.OrderNo) {
		t.Errorf("单号应为日期加六位大写十六进制：%s", p.OrderNo)
	}
	if len(f.orders) != 3 || f.orders[len(f.orders)-1] != p.OrderNo {
		t.Fatalf("应查询审计并在两次碰撞后重试：%v", f.orders)
	}
}

func TestAdminOrderAuditFailure(t *testing.T) {
	want := errors.New("审计查询失败")
	f := &adminOrderAudit{adminFake: newAdminFake(), err: want}
	_, err := adminOrderService(f).BuildChanges(context.Background(), adminOrderRequest(f.adminFake))
	if !errors.Is(err, want) {
		t.Fatalf("无法验证唯一性时应返回审计错误：%v", err)
	}
}

func TestAdminOrderCanceled(t *testing.T) {
	f := &adminOrderAudit{adminFake: newAdminFake()}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := adminOrderService(f).BuildChanges(ctx, adminOrderRequest(f.adminFake))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后应停止生成：%v", err)
	}
}

func TestAdminOrderConcurrentUnique(t *testing.T) {
	f := &adminOrderAudit{adminFake: newAdminFake()}
	svc := adminOrderService(f)
	// 每个候选连续出现两次，强制并发请求命中尚未写审计的已有提案。
	var random []byte
	for i := byte(0); i < 32; i++ {
		random = append(random, 0, 0, i, 0, 0, i)
	}
	SetAdminOrderRandomForTest(svc, bytes.NewReader(random))
	orders := make(chan string, 32)
	var wg sync.WaitGroup
	for range cap(orders) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p, err := svc.BuildChanges(context.Background(), adminOrderRequest(f.adminFake))
			if err != nil {
				t.Error(err)
				return
			}
			orders <- p.OrderNo
		}()
	}
	wg.Wait()
	close(orders)
	seen := map[string]bool{}
	for order := range orders {
		if seen[order] {
			t.Fatalf("并发生成重复单号：%s", order)
		}
		seen[order] = true
	}
	if len(seen) != 32 || len(f.orders) < 32 {
		t.Fatalf("并发生成需逐个查重：%d 单，%d 次查询", len(seen), len(f.orders))
	}
}

func TestAdminOrderLegacyRollback(t *testing.T) {
	f := newAdminFake()
	svc, _, _ := adminEnv(f)
	before, _, _, err := svc.ReadModel(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	legacy := "CO-261102-25C9F1F7D639983DD4DE611406655215"
	var after map[string]any
	_ = json.Unmarshal(f.bodies["a"], &after)
	after["label"] = "历史变更后"
	f.bodies["a"], _ = json.Marshal(after)
	after["enabled"] = false
	detail, _ := json.Marshal(map[string]any{"via": "assistant", "order_no": legacy, "before": before, "after": after, "diff": []AdminChangeDiff{{Path: "label", Before: "测试模型", After: "历史变更后"}}})
	f.audits = []repository.AssistantAuditRecord{{ID: 1, Target: "model", Key: "a", Detail: detail}}
	p, err := svc.BuildChanges(context.Background(), AdminChangeRequest{RevertOf: legacy})
	if err != nil || p.RevertOf != legacy || len(p.Items) != 1 || p.Items[0].Status != "ok" || len(p.Items[0].Diff) != 1 || p.Items[0].Diff[0].After != "测试模型" {
		t.Fatalf("历史长单号回滚失败：%+v %v", p, err)
	}
}

func TestAdminOrderPendingCollision(t *testing.T) {
	f := &adminOrderAudit{adminFake: newAdminFake()}
	svc := adminOrderService(f)
	SetAdminOrderRandomForTest(svc, bytes.NewReader([]byte{0xab, 0xcd, 0xef, 0xab, 0xcd, 0xef, 0x12, 0x34, 0x56}))
	first, err := svc.BuildChanges(context.Background(), adminOrderRequest(f.adminFake))
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.BuildChanges(context.Background(), adminOrderRequest(f.adminFake))
	if err != nil {
		t.Fatal(err)
	}
	if first.OrderNo != "CO-261102-ABCDEF" || second.OrderNo != "CO-261102-123456" {
		t.Fatalf("未应用提案碰撞未重试：%s %s", first.OrderNo, second.OrderNo)
	}
	if len(f.orders) != 2 {
		t.Fatalf("内存保留的单号不必重复查库：%v", f.orders)
	}
}

func TestAdminOrderRandomFailure(t *testing.T) {
	f := &adminOrderAudit{adminFake: newAdminFake()}
	svc := adminOrderService(f)
	SetAdminOrderRandomForTest(svc, bytes.NewReader([]byte{1}))
	if _, err := svc.BuildChanges(context.Background(), adminOrderRequest(f.adminFake)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("随机源读取失败不应生成单号：%v", err)
	}
}
