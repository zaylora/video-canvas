package repository_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"gorm.io/gorm"

	"video-canvas/internal/model"
	"video-canvas/internal/provider/plugin"
	"video-canvas/internal/provider/pluginrunner"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
	agentsvc "video-canvas/internal/service/agent"
)

const integrationDraft = `module.exports={meta:{apiVersion:1,key:"p2-platform",name:"二期平台",auth:{type:"bearer"},endpoints:{video:{mode:"sync"}}},buildSubmitRequest:function(ctx){return {method:"POST",path:"/generate",json:{prompt:ctx.input.prompt}}},parseSubmitResponse:function(ctx,res){return {immediate:[{url:res.body.url,mediaType:"video"}]}}};`

type pluginDBEnv struct {
	db      *gorm.DB
	bridge  *agentsvc.AgentBridge
	admin   *agentsvc.AdminAssistantService
	cfg     *service.AIConfigService
	plugins *service.AIPluginService
	rc      agentsvc.RunContext
	user    *model.User
}

func newPluginDBEnv(t *testing.T) pluginDBEnv {
	t.Helper()
	db := isolatedDB(t, &model.User{}, &model.AIModel{}, &model.AIConfigRevision{}, &model.AIPlugin{}, &model.AIPluginVersion{}, &model.AIChannel{}, &model.AISecret{}, &model.AIAuditLog{}, &model.AdminAuditLog{}, &model.SystemSetting{}, &model.AgentAdminAttempt{})
	users := repository.NewUserRepository(db)
	models := repository.NewAIConfigRepository(db)
	channels := repository.NewAIChannelRepository(db)
	plugins := repository.NewAIPluginRepository(db)
	cfg := service.NewAIConfigService(models, channels, plugins, "p2-test-encryption-passphrase")
	runner := plugin.NewInProcessRunnerClient(pluginrunner.NewServer(pluginrunner.Options{}).Handler())
	pluginSvc := service.NewAIPluginService(plugins, cfg, channels, plugin.NewPrechecker(runner), nil)
	channelSvc := service.NewAIChannelService(channels, cfg, cfg, nil, channels, nil)
	settings := service.NewPricingSettingsService(repository.NewSystemSettingRepository(db), nil)
	audits := repository.NewAdminAssistantRepository(db)
	user := &model.User{Username: "phase-two-admin", Password: "test", Role: model.RoleSuperAdmin}
	if err := db.Create(user).Error; err != nil {
		t.Fatal(err)
	}
	admin := agentsvc.NewAdminAssistantService(agentsvc.AdminAssistantDeps{Models: models, Writer: cfg, Users: users, Channels: channels, Settings: settings, Audit: audits, Plugins: plugins, PluginWriter: pluginSvc, ChannelWriter: channelSvc, Checker: plugin.NewDraftChecker(runner), Attempts: audits})
	b := agentsvc.NewAgentBridge(agentsvc.BridgeDeps{Admin: admin, DramaApprovalKey: []byte("phase-two-stable-signature")})
	rc := agentsvc.RunContext{RunID: 701, SessionID: 702, UserID: user.ID, Mode: "admin"}
	return pluginDBEnv{db: db, bridge: b, admin: admin, cfg: cfg, plugins: pluginSvc, rc: rc, user: user}
}

func draftModelInput(pluginKey, channelKey string) map[string]any {
	return map[string]any{"kind": "video", "label": "新视频模型", "channels": []any{map[string]any{"channel": channelKey, "upstream_model": "video-v1", "protocol": map[string]any{"plugin": pluginKey, "version": "latest"}}}, "capabilities": map[string]any{"ops": []string{"t2v"}, "prompt": map[string]any{"max_length": 2000}}, "pricing": map[string]any{"billing": "per_call", "unit": 1}}
}

func (e pluginDBEnv) propose(t *testing.T, withPlugin bool) (*agentsvc.ApprovalProposal, *agentsvc.ApprovalOutcome) {
	t.Helper()
	ctx := context.Background()
	if withPlugin {
		args, _ := json.Marshal(map[string]any{"code": integrationDraft})
		res, err := e.bridge.ExecutePlatformTool(ctx, e.rc, "check", "admin_plugin_check", args)
		if err != nil || res.IsError || !strings.Contains(res.Content, `"ok":true`) {
			t.Fatalf("静态验证：%+v %v", res, err)
		}
		for _, table := range []string{"ai_plugins", "ai_plugin_versions"} {
			var count int64
			if err := e.db.Table(table).Count(&count).Error; err != nil || count != 0 {
				t.Fatalf("静态检查写入 %s：%d %v", table, count, err)
			}
		}
	}
	items := []agentsvc.AdminChangeInput{{Target: "model_create", Key: "p2-model", Patch: draftModelInput("p2-platform", "p2-channel"), DependsOn: []int{1}}, {Target: "channel", Key: "p2-channel", Patch: map[string]any{"name": "二期渠道", "base_url": "https://example.invalid", "headers": map[string]any{"X-Product": "test"}}}}
	if withPlugin {
		items[0].DependsOn = append(items[0].DependsOn, 2)
		items[1].DependsOn = []int{2}
		items = append(items, agentsvc.AdminChangeInput{Target: "plugin", Key: "p2-platform", Patch: map[string]any{"code": integrationDraft}})
	}
	raw, _ := json.Marshal(agentsvc.AdminChangeRequest{Title: "接入平台", Reason: "用户要求接入", Items: items})
	p, err := e.bridge.ProposeApproval(ctx, e.rc, "admin_change", raw)
	if err != nil || p.IsError {
		t.Fatalf("创建提案：%+v %v", p, err)
	}
	o, err := e.bridge.ResolveApproval(ctx, e.rc, "admin_change", p.Payload, model.DecideAgentApprovalReq{Decision: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	return p, o
}

func TestAdminPluginFullChainRealDB(t *testing.T) {
	for _, scenario := range []string{"新插件完整链路", "复用内置插件", "admin权限跳过", "上传失败依赖跳过", "取消依赖条目", "审计失败回滚", "并发重放"} {
		t.Run(scenario, func(t *testing.T) {
			e := newPluginDBEnv(t)
			ctx := context.Background()
			withPlugin := scenario != "复用内置插件"
			if !withPlugin {
				if _, err := e.plugins.RegisterBuiltin(ctx, [][]byte{[]byte(strings.Replace(integrationDraft, `name:"二期平台"`, `version:"1.0.0",name:"二期平台"`, 1))}); err != nil {
					t.Fatal(err)
				}
			}
			p, o := e.propose(t, withPlugin)
			switch scenario {
			case "admin权限跳过":
				if err := e.db.Model(e.user).Update("role", model.RoleAdmin).Error; err != nil {
					t.Fatal(err)
				}
			case "上传失败依赖跳过":
				if _, err := e.plugins.RegisterBuiltin(ctx, [][]byte{[]byte(strings.Replace(integrationDraft, `name:"二期平台"`, `version:"1.0.0",name:"二期平台"`, 1))}); err != nil {
					t.Fatal(err)
				}
			case "取消依赖条目":
				var err error
				o, err = e.bridge.ResolveApproval(ctx, e.rc, "admin_change", p.Payload, model.DecideAgentApprovalReq{Decision: "approve", Items: []model.ApprovalItemDecision{{Index: 0, Approve: true}, {Index: 1, Approve: true}, {Index: 2, Approve: false}}})
				if err != nil {
					t.Fatal(err)
				}
			case "审计失败回滚":
				if err := e.db.Exec(`CREATE FUNCTION reject_assistant_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.detail_json->>'via' = 'assistant' THEN RAISE EXCEPTION 'reject audit'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_assistant_audit BEFORE INSERT ON ai_audit_logs FOR EACH ROW EXECUTE FUNCTION reject_assistant_audit()`).Error; err != nil {
					t.Fatal(err)
				}
			}
			var result *agentsvc.ApprovalExecution
			run := func() (*agentsvc.ApprovalExecution, error) {
				return e.bridge.ExecuteApproval(ctx, e.rc, "admin_change", 703, "change", *o)
			}
			if scenario == "并发重放" {
				var wg sync.WaitGroup
				var mu sync.Mutex
				for i := 0; i < 6; i++ {
					wg.Add(1)
					go func() {
						defer wg.Done()
						res, err := run()
						if err != nil {
							t.Error(err)
							return
						}
						if res.Status != agentsvc.ExecExecuted {
							t.Errorf("重放未恢复：%s", res.Result)
						}
						mu.Lock()
						result = res
						mu.Unlock()
					}()
				}
				wg.Wait()
			} else {
				var err error
				result, err = run()
				if err != nil {
					t.Fatal(err)
				}
			}
			if result == nil {
				t.Fatal("没有执行结果")
			}
			var body agentsvc.AdminChangeResult
			if err := json.Unmarshal(result.Result, &body); err != nil {
				t.Fatal(err)
			}
			success := scenario == "新插件完整链路" || scenario == "复用内置插件" || scenario == "并发重放"
			if !success {
				if body.Items[0].Status != "skipped" || !strings.Contains(body.Items[0].Message, "依赖的") {
					t.Fatalf("模型依赖没有跳过：%s", result.Result)
				}
				if scenario == "审计失败回滚" {
					for _, table := range []string{"ai_plugins", "ai_plugin_versions", "ai_channels", "ai_models", "ai_audit_logs"} {
						var count int64
						if err := e.db.Table(table).Count(&count).Error; err != nil || count != 0 {
							t.Fatalf("审计失败未回滚 %s：%d %v", table, count, err)
						}
					}
				}
				return
			}
			for _, item := range body.Items {
				if item.Status != "applied" || item.Created == nil {
					t.Fatalf("条目未创建：%s", result.Result)
				}
			}
			var m model.AIModel
			if err := e.db.Where("key = ?", "p2-model").First(&m).Error; err != nil || m.Enabled {
				t.Fatalf("模型必须默认停用：%+v %v", m, err)
			}
			var rev model.AIConfigRevision
			if err := e.db.Where("target_key = ?", "p2-model").First(&rev).Error; err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(rev.BodyJSON), "latest") {
				t.Fatalf("版本未固定：%s", rev.BodyJSON)
			}
			var count int64
			e.db.Model(&model.AIAuditLog{}).Where("detail_json->>'via' = 'assistant'").Count(&count)
			if count != int64(len(body.Items)) {
				t.Fatalf("审计应每项恰好一条：%d", count)
			}
			var payload agentsvc.AdminChangePayload
			_ = json.Unmarshal(p.Payload, &payload)
			reverse, err := e.admin.BuildChanges(ctx, agentsvc.AdminChangeRequest{RevertOf: payload.OrderNo})
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range reverse.Items {
				if item.Status != "invalid" || !strings.Contains(strings.Join(item.Issues, ""), "手动删除") {
					t.Fatalf("新建条目不能回滚：%+v", item)
				}
			}
		})
	}
}

func TestAdminAttemptsDurableAtomicLimit(t *testing.T) {
	db := isolatedDB(t, &model.AgentAdminAttempt{})
	ctx := context.Background()
	repo := repository.NewAdminAssistantRepository(db)
	var wg sync.WaitGroup
	var mu sync.Mutex
	attempts := []int{}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			n, err := repo.ClaimAdminAttempt(ctx, 1, "plugin_check", "key", fmt.Sprint(i))
			if err != nil {
				t.Error(err)
				return
			}
			if n > 0 {
				mu.Lock()
				attempts = append(attempts, n)
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if len(attempts) != 3 {
		t.Fatalf("并发分配超出3次：%v", attempts)
	}
	if n, err := repository.NewAdminAssistantRepository(db).ClaimAdminAttempt(ctx, 1, "plugin_check", "key", "fourth"); err != nil || n != 0 {
		t.Fatalf("重建仓储绕过上限：%d %v", n, err)
	}
	if n, err := repo.ClaimAdminAttempt(ctx, 2, "plugin_check", "key", "new"); err != nil || n != 1 {
		t.Fatalf("新运行计数错误：%d %v", n, err)
	}
}

func TestAdminSecretEncryptedSaveNeverEntersApproval(t *testing.T) {
	e := newPluginDBEnv(t)
	ctx := context.Background()
	if err := e.db.Create(&model.AIChannel{Key: "main", Name: "主渠道", BaseURL: "https://example.invalid", HeadersJSON: model.JSONText(`{}`), RateLimitJSON: model.JSONText(`{}`)}).Error; err != nil {
		t.Fatal(err)
	}
	p, err := e.bridge.ProposeApproval(ctx, e.rc, "admin_secret", json.RawMessage(`{"channel_key":"main","reason":"平台鉴权"}`))
	if err != nil || p.IsError {
		t.Fatalf("%+v %v", p, err)
	}
	sentinel := "P2-DB-ZERO-LEAK-4db7e590"
	if err := e.cfg.SetSecret(ctx, model.ChannelSecretName("main"), sentinel, e.user.ID); err != nil {
		t.Fatal(err)
	}
	o, err := e.bridge.ResolveApproval(ctx, e.rc, "admin_secret", p.Payload, model.DecideAgentApprovalReq{Decision: "approve", Answer: "configured"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := e.bridge.ExecuteApproval(ctx, e.rc, "admin_secret", 900, "secret", *o)
	if err != nil || string(result.Result) != `{"configured":true}` {
		t.Fatalf("%+v %v", result, err)
	}
	for _, v := range []any{p, o, result} {
		raw, _ := json.Marshal(v)
		if strings.Contains(string(raw), sentinel) {
			t.Fatal("审批边界泄露密钥")
		}
	}
	for _, table := range []string{"ai_secrets", "ai_audit_logs", "ai_channels", "agent_admin_attempts"} {
		var rows []struct{ Body string }
		if err := e.db.Raw("SELECT row_to_json(t)::text AS body FROM " + table + " t").Scan(&rows).Error; err != nil {
			t.Fatal(err)
		}
		for _, row := range rows {
			if strings.Contains(row.Body, sentinel) {
				t.Fatalf("%s 泄露密钥", table)
			}
		}
	}
}

func TestAdminModelCreateCannotOverwriteConcurrentSave(t *testing.T) {
	db := isolatedDB(t, &model.AIModel{}, &model.AIConfigRevision{})
	r := repository.NewAIConfigRepository(db)
	ctx := context.Background()
	in := repository.SaveModelInput{CreateOnly: true, Pointer: repository.ConfigPointer{Target: "model", Key: "create-once", Kind: "video"}, Body: []byte(`{"label":"第一次创建"}`)}
	if _, err := r.SaveModel(ctx, in); err != nil {
		t.Fatal(err)
	}
	in.Body = []byte(`{"label":"不应覆盖"}`)
	if _, err := r.SaveModel(ctx, in); err == nil {
		t.Fatal("并发新建不能覆盖既有配置")
	}
	current, err := r.GetModelConfig(ctx, "create-once")
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		t.Fatal(err)
	}
	if current == nil || !strings.Contains(string(current.BodyJSON), "第一次创建") {
		t.Fatalf("原配置被覆盖：%+v", current)
	}
}
