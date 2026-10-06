package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	. "video-canvas/internal/service"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
	"video-canvas/internal/service/aiconfigfake"
)

// aicNewSvc 组装一个使用内存 fake 的配置服务，带主密钥；仓储同时充当渠道 / 插件的只读依赖。
func aicNewSvc() (*AIConfigService, *aiconfigfake.MemRepo, *aiconfigfake.Invalidator) {
	repo := aiconfigfake.NewMemRepo()
	svc := NewAIConfigService(repo, repo, repo, "test-master-key")
	inv := &aiconfigfake.Invalidator{}
	svc.SetInvalidator(inv)
	return svc, repo, inv
}

// aicModelBody 生成模型配置正文（video，绑定渠道 channel）。extra 是要追加的 JSON 成员（如 `"bad":true`）。
// aicWithKind 把 aicModelBody 生成的视频模型正文改成别的种类，能力一并换成该种类合法的最小配置。
func aicWithKind(body, kind string) string {
	caps := map[string]string{
		"text":  `{"prompt":{"max_length":2000},"context":{"window":128000,"output":4096}}`,
		"image": `{"ops":["t2i"],"prompt":{"max_length":2000}}`,
		"audio": `{"prompt":{"max_length":2000}}`,
		"video": `{"ops":["t2v"],"prompt":{"max_length":2000}}`,
	}[kind]
	body = strings.Replace(body, `"kind":"video"`, fmt.Sprintf(`"kind":%q`, kind), 1)
	// 定价沿用按次 5 积分：按次计费对任何种类都合法
	return strings.Replace(body, `"capabilities":{"ops":["t2v"],"prompt":{"max_length":2000}}`, `"capabilities":`+caps, 1)
}

func aicModelBody(key, channel string, extra string) json.RawMessage {
	if extra != "" {
		extra = "," + extra
	}
	return json.RawMessage(fmt.Sprintf(`{"key":%q,"kind":"video","label":"模型-%s","hint":"提示","enabled":true,"sort":10,`+
		`"channels":[{"channel":%q,"upstream_model":"kling-v2"}],"params":{"instanceType":"default"},`+
		`"capabilities":{"ops":["t2v"],"prompt":{"max_length":2000}},"pricing":{"billing":"per_call","unit":5,"cost":{"on":true,"unit":2}}%s}`, key, key, channel, extra))
}

// aicWantCode 断言错误是指定业务错误码；wantCode=0 表示期望成功。
func aicWantCode(t *testing.T, err error, wantCode int) {
	t.Helper()
	adminWantCode(t, err, wantCode)
}

// aicSeedChannel 往仓储里放一个渠道（固定 kling@1.0.0，鉴权 bearer，支持 video / text），withSecret 时同时设置渠道 Key。
func aicSeedChannel(t *testing.T, svc *AIConfigService, repo *aiconfigfake.MemRepo, key string, withSecret bool) {
	t.Helper()
	ctx := context.Background()
	if _, ok := repo.Plugins["kling"]; !ok {
		adminSeedVersion(t, repo, adminMetaJSON("kling", "1.0.0"), model.PluginSourceUploaded, true)
	}
	ver, err := repo.FindVersion(ctx, "kling", "1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	repo.Channels[key] = &model.AIChannel{
		Key: key, Name: key, PluginKey: "kling", PluginVersionID: ver.ID, BaseURL: "https://gw.example.com",
		SettingsJSON: model.JSONText(`{"tenant":"t"}`), RateLimitJSON: model.JSONText(`{}`), Enabled: true,
	}
	if withSecret {
		if err := svc.SetSecret(ctx, model.ChannelSecretName(key), "sk-"+key, 1); err != nil {
			t.Fatal(err)
		}
	}
}

// aicSave 保存模型草稿；create=true 新建（key 取正文），否则更新 key。
func aicSave(svc *AIConfigService, key string, create bool, body json.RawMessage) (*SaveDraftResult, error) {
	return svc.SaveDraft(context.Background(), ModelDraftInput{Key: key, Create: create, Body: body, Note: "备注", AdminID: 9})
}

// aicPublishModel 保存并发布一个模型（渠道 c1 必须已就绪）。
func aicPublishModel(t *testing.T, svc *AIConfigService, key, channel string) {
	t.Helper()
	ctx := context.Background()
	if _, err := svc.SaveDraft(ctx, ModelDraftInput{Create: true, Body: aicModelBody(key, channel, ""), AdminID: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(ctx, key, 1); err != nil {
		t.Fatal(err)
	}
}

func TestAIConfigService_SaveDraft(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		create   bool
		body     json.RawMessage
		setup    func(svc *AIConfigService)
		wantCode int
		wantIss  int
	}{
		{"新建模型草稿成功", "", true, aicModelBody("m1", "c1", ""), nil, 0, 0},
		{"key 已存在不能再新建", "", true, aicModelBody("m1", "c1", ""), func(s *AIConfigService) {
			_, _ = aicSave(s, "", true, aicModelBody("m1", "c1", ""))
		}, errcode.ErrConfigInvalid.Code, 0},
		{"更新不存在的配置返回 404", "nope", false, aicModelBody("nope", "c1", ""), nil, errcode.ErrConfigNotFound.Code, 0},
		{"路径 key 与正文不一致", "m2", false, aicModelBody("m1", "c1", ""), nil, errcode.ErrInvalidParams.Code, 0},
		{"正文不是 JSON 对象", "", true, json.RawMessage(`[1,2]`), nil, errcode.ErrInvalidParams.Code, 0},
		{"正文缺少 key", "", true, json.RawMessage(`{"label":"x"}`), nil, errcode.ErrInvalidParams.Code, 0},
		{"key 含非法字符", "", true, json.RawMessage(`{"key":"a/b"}`), nil, errcode.ErrInvalidParams.Code, 0},
		{"key 超过 128 字符", "", true, aicModelBody(strings.Repeat("a", 129), "c1", ""), nil, errcode.ErrInvalidParams.Code, 0},
		{"正文里有未知字段：仍保存并返回 Issue", "", true, aicModelBody("m1", "c1", `"bad":true`), nil, 0, 1},
		{"绑定的渠道不存在：仍保存，Issue 挂在 channels[0].channel", "", true, aicModelBody("m1", "ghost", ""), nil, 0, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := aicNewSvc()
			aicSeedChannel(t, svc, repo, "c1", true)
			if tt.setup != nil {
				tt.setup(svc)
			}
			res, err := aicSave(svc, tt.key, tt.create, tt.body)
			aicWantCode(t, err, tt.wantCode)
			if tt.wantCode != 0 {
				return
			}
			if len(res.Issues) != tt.wantIss || res.Issues == nil {
				t.Fatalf("Issue 数量期望 %d（且非 nil），实际 %v", tt.wantIss, res.Issues)
			}
			if res.Revision.Status != model.RevisionDraft || res.Revision.CreatedBy != 9 || res.Revision.Note != "备注" {
				t.Fatalf("revision 不符合预期：%+v", res.Revision)
			}
			if tt.wantIss > 0 {
				// 有问题也已经落库
				if _, err := repo.GetDraft(context.Background(), model.ConfigTargetModel, res.Revision.TargetKey); err != nil {
					t.Fatalf("草稿应已保存：%v", err)
				}
			}
		})
	}
}

func TestAIConfigService_SaveDraft_CrossObjectIssues(t *testing.T) {
	// 渠道停用、插件停用、插件不支持这个 kind：都是跨对象问题，保存时以 Issue 报出而不是拒绝
	tests := []struct {
		name    string
		mutate  func(repo *aiconfigfake.MemRepo)
		wantMsg string
	}{
		{"渠道已停用", func(r *aiconfigfake.MemRepo) { r.Channels["c1"].Enabled = false }, "已停用"},
		{"插件已停用", func(r *aiconfigfake.MemRepo) { r.Plugins["kling"].Enabled = false }, "已停用"},
		{"插件不支持该种类", func(r *aiconfigfake.MemRepo) {
			for _, v := range r.Versions {
				v.MetaJSON = model.JSONText(adminMetaLoose("kling", "1.0.0")) // 只支持 video
			}
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := aicNewSvc()
			aicSeedChannel(t, svc, repo, "c1", true)
			tt.mutate(repo)
			body := aicModelBody("m1", "c1", "")
			if tt.wantMsg == "" { // 用 text 种类去撞只支持 video 的插件
				body = json.RawMessage(aicWithKind(string(body), "text"))
			}
			res, err := aicSave(svc, "", true, body)
			aicWantCode(t, err, 0)
			if len(res.Issues) != 1 || res.Issues[0].Path != "channels[0].channel" || !strings.Contains(res.Issues[0].Message, tt.wantMsg) {
				t.Fatalf("Issue 不符合预期：%+v", res.Issues)
			}
		})
	}
}

func TestAIConfigService_SaveDraft_Revisions(t *testing.T) {
	svc, repo, _ := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true)
	aicSeedChannel(t, svc, repo, "c2", true)
	ctx := context.Background()
	if _, err := aicSave(svc, "", true, aicModelBody("m1", "c1", "")); err != nil {
		t.Fatal(err)
	}
	// 上下架状态由接口独占：先改 enabled，再保存草稿不应被正文覆盖
	repo.Models["m1"].Enabled = false
	repo.Models["m1"].Sort = 77
	res, err := aicSave(svc, "m1", false, aicModelBody("m1", "c2", ""))
	if err != nil {
		t.Fatal(err)
	}
	if res.Revision.RevisionNo != 2 {
		t.Fatalf("revision_no 期望 2，实际 %d", res.Revision.RevisionNo)
	}
	if repo.Models["m1"].Enabled || repo.Models["m1"].Sort != 77 {
		t.Fatalf("已存在的指针行不应被正文覆盖 enabled/sort：%+v", repo.Models["m1"])
	}
	drafts := 0
	for _, r := range repo.Revs {
		if r.Status == model.RevisionDraft {
			drafts++
		}
	}
	if drafts != 1 {
		t.Fatalf("同一目标只应保留 1 个草稿，实际 %d", drafts)
	}
	if d, _ := repo.GetDraft(ctx, model.ConfigTargetModel, "m1"); !strings.Contains(string(d.BodyJSON), `"c2"`) {
		t.Fatalf("最新草稿应是第二版：%s", d.BodyJSON)
	}
}

func TestAIConfigService_NewModelInitialValues(t *testing.T) {
	svc, repo, _ := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true)
	if _, err := aicSave(svc, "", true, aicModelBody("m1", "c1", "")); err != nil {
		t.Fatal(err)
	}
	m := repo.Models["m1"]
	if !m.Enabled || m.Sort != 10 || m.Kind != "video" {
		t.Fatalf("新建模型的指针行应取正文初始值：%+v", m)
	}
}

func TestAIConfigService_Validate(t *testing.T) {
	ctx := context.Background()
	svc, repo, _ := aicNewSvc()
	aicSeedChannel(t, svc, repo, "c1", true)
	if _, err := aicSave(svc, "", true, aicModelBody("m1", "c1", `"bad":true`)); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		key       string
		body      json.RawMessage
		wantCode  int
		wantValid bool
	}{
		{"校验已保存的草稿：有问题", "m1", nil, 0, false},
		{"校验传入的正文：通过", "m1", aicModelBody("m1", "c1", ""), 0, true},
		{"传入正文的 key 与目标不一致", "m1", aicModelBody("other", "c1", ""), 0, false},
		{"传入正文不是对象", "m1", json.RawMessage(`"x"`), 0, false},
		{"传入正文的渠道不存在", "m1", aicModelBody("m1", "ghost", ""), 0, false},
		{"没有草稿", "none", nil, errcode.ErrConfigNoDraft.Code, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := svc.Validate(ctx, tt.key, tt.body)
			aicWantCode(t, err, tt.wantCode)
			if tt.wantCode != 0 {
				return
			}
			if res.Valid != tt.wantValid || res.Issues == nil || (!res.Valid && len(res.Issues) == 0) {
				t.Fatalf("校验结果不符合预期：%+v", res)
			}
		})
	}
}

func TestAIConfigService_Publish(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name     string
		setup    func(t *testing.T, svc *AIConfigService, repo *aiconfigfake.MemRepo)
		key      string
		wantCode int
	}{
		{"没有草稿", nil, "m1", errcode.ErrConfigNoDraft.Code},
		{"草稿有校验问题不能发布", func(t *testing.T, s *AIConfigService, r *aiconfigfake.MemRepo) {
			aicSeedChannel(t, s, r, "c1", true)
			_, _ = aicSave(s, "", true, aicModelBody("m1", "c1", `"bad":true`))
		}, "m1", errcode.ErrConfigInvalid.Code},
		{"绑定的渠道不存在（50011）", func(t *testing.T, s *AIConfigService, r *aiconfigfake.MemRepo) {
			_, _ = aicSave(s, "", true, aicModelBody("m1", "ghost", ""))
		}, "m1", errcode.ErrChannelNotFound.Code},
		{"渠道已停用（50014）", func(t *testing.T, s *AIConfigService, r *aiconfigfake.MemRepo) {
			aicSeedChannel(t, s, r, "c1", true)
			_, _ = aicSave(s, "", true, aicModelBody("m1", "c1", ""))
			r.Channels["c1"].Enabled = false
		}, "m1", errcode.ErrChannelDisabled.Code},
		{"渠道用的插件已停用（50004）", func(t *testing.T, s *AIConfigService, r *aiconfigfake.MemRepo) {
			aicSeedChannel(t, s, r, "c1", true)
			_, _ = aicSave(s, "", true, aicModelBody("m1", "c1", ""))
			r.Plugins["kling"].Enabled = false
		}, "m1", errcode.ErrPluginDisabled.Code},
		{"渠道固定的插件版本已不存在（50013）", func(t *testing.T, s *AIConfigService, r *aiconfigfake.MemRepo) {
			aicSeedChannel(t, s, r, "c1", true)
			_, _ = aicSave(s, "", true, aicModelBody("m1", "c1", ""))
			for id := range r.Versions {
				delete(r.Versions, id)
			}
		}, "m1", errcode.ErrChannelInvalid.Code},
		{"渠道的插件不支持模型的种类", func(t *testing.T, s *AIConfigService, r *aiconfigfake.MemRepo) {
			aicSeedChannel(t, s, r, "c1", true)
			body := aicWithKind(string(aicModelBody("m1", "c1", "")), "image")
			_, _ = aicSave(s, "", true, json.RawMessage(body))
		}, "m1", errcode.ErrConfigInvalid.Code},
		{"需要鉴权而渠道 Key 未设置（50015）", func(t *testing.T, s *AIConfigService, r *aiconfigfake.MemRepo) {
			aicSeedChannel(t, s, r, "c1", false)
			_, _ = aicSave(s, "", true, aicModelBody("m1", "c1", ""))
		}, "m1", errcode.ErrChannelSecretUnset.Code},
		{"渠道 Key 已设置，发布成功", func(t *testing.T, s *AIConfigService, r *aiconfigfake.MemRepo) {
			aicSeedChannel(t, s, r, "c1", true)
			_, _ = aicSave(s, "", true, aicModelBody("m1", "c1", ""))
		}, "m1", 0},
		{"auth.type=none 的插件不需要 Key", func(t *testing.T, s *AIConfigService, r *aiconfigfake.MemRepo) {
			id := adminSeedVersion(t, r, adminMetaLoose("loose", "1.0.0"), model.PluginSourceUploaded, true)
			r.Channels["c1"] = &model.AIChannel{Key: "c1", PluginKey: "loose", PluginVersionID: id, BaseURL: "https://x.com", Enabled: true}
			_, _ = aicSave(s, "", true, aicModelBody("m1", "c1", ""))
		}, "m1", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, inv := aicNewSvc()
			if tt.setup != nil {
				tt.setup(t, svc, repo)
			}
			before := len(inv.Reasons)
			rev, err := svc.Publish(ctx, tt.key, 1)
			aicWantCode(t, err, tt.wantCode)
			if tt.wantCode != 0 {
				if len(inv.Reasons) != before {
					t.Fatal("发布失败不应触发失效广播")
				}
				return
			}
			if rev.Status != model.RevisionPublished {
				t.Fatalf("状态应为 published：%+v", rev)
			}
			pub, err := repo.GetPublishedRevision(ctx, model.ConfigTargetModel, tt.key)
			if err != nil || pub.ID != rev.ID {
				t.Fatalf("指针未指向新发布的版本：%v", err)
			}
			if len(inv.Reasons) != before+1 {
				t.Fatalf("发布成功应广播一次失效，实际 %v", inv.Reasons)
			}
		})
	}
}

// aicRacyRepo 在发布前偷偷保存一个新草稿，模拟“校验之后草稿被别人改掉”。
type aicRacyRepo struct{ *aiconfigfake.MemRepo }

func (r aicRacyRepo) PublishDraft(ctx context.Context, ptr repository.ConfigPointer, id uint64) (*model.AIConfigRevision, error) {
	_, _ = r.SaveDraft(ctx, repository.SaveDraftInput{Pointer: ptr, Body: aicModelBody(ptr.Key, "c1", `"x":1`)})
	return r.MemRepo.PublishDraft(ctx, ptr, id)
}

func TestAIConfigService_Publish_DraftChangedConcurrently(t *testing.T) {
	ctx := context.Background()
	mem := aiconfigfake.NewMemRepo()
	svc := NewAIConfigService(aicRacyRepo{mem}, mem, mem, "k")
	aicSeedChannel(t, svc, mem, "c1", true)
	if _, err := aicSave(svc, "", true, aicModelBody("m1", "c1", "")); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Publish(ctx, "m1", 1)
	aicWantCode(t, err, errcode.ErrConfigNoDraft.Code)
	if _, err := mem.GetPublishedRevision(ctx, model.ConfigTargetModel, "m1"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal("冲突时不应发布任何版本")
	}
}
