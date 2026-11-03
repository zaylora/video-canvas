package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"video-canvas/internal/config"
	"video-canvas/internal/imageproc"
	"video-canvas/internal/model"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	agentsvc "video-canvas/internal/service/agent"
)

// TestAdminManagementRealServices 用真实业务服务和 PostgreSQL 验证新增审批，而不是绕过后台原有校验。
func TestAdminManagementRealServices(t *testing.T) {
	db := isolatedDB(t, model.All()...)
	ctx := context.Background()
	actor := mkUser(t, db, "management-admin", "admin@example.com", model.RoleSuperAdmin, model.UserStatusActive)
	target := mkUser(t, db, "management-user", "user@example.com", model.RoleUser, model.UserStatusActive)
	users := repository.NewUserRepository(db)
	audits := repository.NewAdminAuditRepository(db)
	reg := service.NewSettingsService(repository.NewSystemSettingRepository(db), audits)
	userSvc := service.NewAdminUserService(users, audits, reg, service.WithAdminControls(service.AdminControls{Credits: repository.NewGenerationTaskRepository(db)}))
	models := repository.NewAIConfigRepository(db)
	channels := repository.NewAIChannelRepository(db)
	cfg := service.NewAIConfigService(models, channels, repository.NewAIPluginRepository(db), "management-test-key")
	stores := repository.NewStorageConfigRepository(db)
	storeSvc := service.NewStorageConfigService(stores, cfg, channels, nil, config.Storage{})
	processors := service.NewImageProcessorService(repository.NewImageProcessorRepository(db), stores, nil, nil)
	operations := repository.NewAdminAssistantRepository(db)
	management := &agentsvc.AdminManagementDeps{Users: userSvc, Register: reg, Storages: storeSvc, Processors: processors, Usage: service.NewAdminUsageService(repository.NewAdminUsageRepository(db)), Logs: service.NewAuditLogService(audits), Runs: agentsvc.NewObserveService(agentsvc.ObserveDeps{Repo: repository.NewAgentObserveRepository(db)}), Operations: operations}
	admin := agentsvc.NewAdminAssistantService(agentsvc.AdminAssistantDeps{Users: users, Audit: operations, Settings: service.NewPricingSettingsService(repository.NewSystemSettingRepository(db), nil), Management: management})
	bridge := agentsvc.NewAgentBridge(agentsvc.BridgeDeps{Admin: admin, DramaApprovalKey: []byte("management-signature")})
	rc := agentsvc.RunContext{RunID: 1, SessionID: 2, UserID: actor.ID, Mode: "admin"}
	call := func(name string, args any) *agentsvc.ToolResult {
		t.Helper()
		raw, _ := json.Marshal(args)
		out, err := bridge.ExecutePlatformTool(ctx, rc, "query", name, raw)
		if err != nil || out.IsError {
			t.Fatalf("%s: %+v %v", name, out, err)
		}
		return out
	}
	approval := uint64(100)
	apply := func(targetType, key string, patch map[string]any, want string) *agentsvc.ApprovalExecution {
		t.Helper()
		sha := ""
		if patch["action"] != "create" {
			out := call("admin_get_resource", map[string]any{"target": targetType, "key": key})
			var v struct {
				SHA string `json:"body_sha256"`
			}
			_ = json.Unmarshal([]byte(out.Content), &v)
			sha = v.SHA
		}
		raw, _ := json.Marshal(agentsvc.AdminChangeRequest{Title: "管理操作", Reason: "验证用户要求", Items: []agentsvc.AdminChangeInput{{Target: targetType, Key: key, BaseSHA256: sha, Patch: patch}}})
		p, err := bridge.ProposeApproval(ctx, rc, "admin_change", raw)
		if err != nil || p.IsError {
			t.Fatalf("提案失败：%+v %v", p, err)
		}
		o, err := bridge.ResolveApproval(ctx, rc, "admin_change", p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
		if err != nil {
			t.Fatal(err)
		}
		approval++
		res, err := bridge.ExecuteApproval(ctx, rc, "admin_change", approval, "call", *o)
		if err != nil || res.Status != want {
			t.Fatalf("执行 %s/%v：%+v %v", targetType, patch, res, err)
		}
		// 每项重放都不得再次改变业务；真实审计必须恢复首次执行结果。
		again, err := bridge.ExecuteApproval(ctx, rc, "admin_change", approval, "call", *o)
		if err != nil || again.Status != want {
			t.Fatalf("重放：%+v %v", again, err)
		}
		return res
	}
	key := strconv.FormatUint(target.ID, 10)
	apply("user", key, map[string]any{"action": "credits", "mode": "add", "amount": 10, "note": "测试发放"}, "executed")
	var account model.UserCredit
	if err := db.First(&account, "user_id = ?", target.ID).Error; err != nil || account.Balance != 60 {
		t.Fatalf("积分未按初始 50 + 10 且仅加一次：%+v %v", account, err)
	}
	apply("user", key, map[string]any{"action": "limit", "max_active_tasks": 8}, "executed")
	apply("user", key, map[string]any{"action": "limit", "max_active_tasks": nil}, "executed")
	apply("user", key, map[string]any{"action": "status", "status": "disabled"}, "executed")
	apply("user", key, map[string]any{"action": "status", "status": "active"}, "executed")
	apply("user", key, map[string]any{"action": "role", "role": "admin"}, "executed")
	apply("user", strconv.FormatUint(actor.ID, 10), map[string]any{"action": "status", "status": "disabled"}, "failed")
	apply("register_settings", "register", map[string]any{"initial_credits": 75}, "executed")

	// 真实存储更新复用定位锁和删除约束；图片服务复用厂商匹配、草稿版本与发布校验。
	store := &model.StorageConfig{Name: "测试 OSS", Provider: "aliyun_oss", Region: "oss-cn-hangzhou", Bucket: "management-test", UseSSL: true, Addressing: "virtual", SignedTTLSec: 3600, Version: 1}
	if err := db.Create(store).Error; err != nil {
		t.Fatal(err)
	}
	storeKey := strconv.FormatUint(store.ID, 10)
	apply("storage", storeKey, map[string]any{"name": "素材存储"}, "executed")
	apply("storage", storeKey, map[string]any{"action": "default"}, "failed")
	apply("image_processor", "test-processor", map[string]any{"action": "create", "name": "缩略图", "vendor": imageproc.VendorAliyunOSSImg, "storage_id": store.ID, "config": map[string]any{"domain": "assets.example.com", "width": 512, "format": "webp"}}, "executed")
	var processor model.ImageProcessor
	if err := db.First(&processor).Error; err != nil {
		t.Fatal(err)
	}
	processorKey := strconv.FormatUint(processor.ID, 10)
	apply("image_processor", processorKey, map[string]any{"name": "缩略图服务", "config": map[string]any{"width": 640}}, "executed")
	apply("image_processor", processorKey, map[string]any{"action": "publish"}, "failed")

	for _, resource := range []string{"users", "usage", "usage_summary", "audits", "runs", "storages", "image_processors", "storage_presets", "image_processor_presets"} {
		out := call("admin_query", map[string]any{"resource": resource, "page_size": 5})
		if !json.Valid([]byte(out.Content)) {
			t.Fatalf("%s 不是 JSON：%s", resource, out.Content)
		}
	}
	apply("image_processor", processorKey, map[string]any{"action": "delete"}, "executed")
	apply("storage", storeKey, map[string]any{"action": "delete"}, "executed")
	// 普通管理员仍不能对其他管理员发积分；查询工具不能扩大用户管理的业务权限。
	if err := users.Update(ctx, actor.ID, map[string]any{"role": model.RoleAdmin}); err != nil {
		t.Fatal(err)
	}
	apply("user", key, map[string]any{"action": "credits", "mode": "add", "amount": 10, "note": "不应允许"}, "failed")
}

func TestAdminManagementOperationReservation(t *testing.T) {
	for _, scenario := range []string{"并发重放", "失败不重试", "中断不重试"} {
		t.Run(scenario, func(t *testing.T) {
			db := isolatedDB(t, &model.AIAuditLog{}, &model.AdminAuditLog{})
			repo := repository.NewAdminAssistantRepository(db)
			ctx := context.Background()
			makeAudit := func() *model.AIAuditLog {
				return &model.AIAuditLog{Action: "assistant.management", TargetType: "user", TargetKey: "2", DetailJSON: model.JSONText(`{"via":"assistant","approval_id":"123","order_no":"CO-test","index":0}`)}
			}
			var calls atomic.Int32
			if scenario == "中断不重试" {
				audit := makeAudit()
				audit.DetailJSON = model.JSONText(`{"via":"assistant","approval_id":"123","order_no":"CO-test","index":0,"state":"running"}`)
				if err := db.Create(audit).Error; err != nil {
					t.Fatal(err)
				}
				_, err := repo.ExecuteAdminOperation(ctx, makeAudit(), func() error { calls.Add(1); return nil })
				if !errors.Is(err, repository.ErrAdminOperationUncertain) || calls.Load() != 0 {
					t.Fatalf("%v calls=%d", err, calls.Load())
				}
				return
			}
			if scenario == "失败不重试" {
				_, err := repo.ExecuteAdminOperation(ctx, makeAudit(), func() error { calls.Add(1); return fmt.Errorf("业务失败") })
				if err == nil {
					t.Fatal("丢失业务错误")
				}
				_, err = repo.ExecuteAdminOperation(ctx, makeAudit(), func() error { calls.Add(1); return nil })
				if !errors.Is(err, repository.ErrAdminOperationFailed) || calls.Load() != 1 {
					t.Fatalf("%v calls=%d", err, calls.Load())
				}
				return
			}
			started, release := make(chan struct{}), make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := repo.ExecuteAdminOperation(ctx, makeAudit(), func() error { calls.Add(1); close(started); <-release; return nil })
				if err != nil {
					t.Error(err)
				}
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("未开始执行")
			}
			_, err := repo.ExecuteAdminOperation(ctx, makeAudit(), func() error { calls.Add(1); return nil })
			close(release)
			wg.Wait()
			if !errors.Is(err, repository.ErrAdminOperationUncertain) {
				t.Fatalf("并发执行应返回未确认：%v", err)
			}
			_, err = repo.ExecuteAdminOperation(ctx, makeAudit(), func() error { calls.Add(1); return nil })
			if err != nil || calls.Load() != 1 {
				t.Fatalf("%v calls=%d", err, calls.Load())
			}
			rows, err := repo.ChangeAudits(ctx, "CO-test")
			if err != nil || len(rows) != 2 || !strings.Contains(string(rows[0].Detail), `"state":"running"`) || !strings.Contains(string(rows[1].Detail), `"state":"applied"`) {
				t.Fatalf("审计：%+v %v", rows, err)
			}
		})
	}
}
