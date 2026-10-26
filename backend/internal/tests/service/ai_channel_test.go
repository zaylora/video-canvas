package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider"
	. "video-canvas/internal/service"
	"video-canvas/internal/service/aiconfigfake"
)

// achEnv 是渠道服务测试的环境：真实的凭证与模型配置服务（AES-GCM）+ 内存仓储 + 替身的上游拉取。
// 渠道不绑协议：这里的渠道只有地址、请求头、限流，协议在模型里选。
type achEnv struct {
	svc      *AIChannelService
	cfg      *AIConfigService
	repo     *aiconfigfake.MemRepo
	lister   *aiconfigfake.ModelLister
	notifier *aiconfigfake.Notifier
}

func newAchEnv(t *testing.T) *achEnv {
	t.Helper()
	repo := aiconfigfake.NewMemRepo()
	cfg := NewAIConfigService(repo, repo, repo, "test-master-key")
	lister := &aiconfigfake.ModelLister{}
	notifier := &aiconfigfake.Notifier{}
	return &achEnv{svc: NewAIChannelService(repo, cfg, cfg, lister, repo, notifier), cfg: cfg, repo: repo, lister: lister, notifier: notifier}
}

// createInput 是一份合法的新建参数，用例在它的基础上改。
func createInput() ChannelCreateInput {
	return ChannelCreateInput{Key: "main", Name: "主渠道", BaseURL: "https://gw.example.com", Enabled: true, ActorID: 5}
}

func TestAIChannelService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("成功：只有连接信息，写审计并刷新 Registry", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.RateLimit = provider.RateLimit{RPS: 5, MaxConcurrency: 20}
		in.Headers = map[string]string{"X-Biz": "canvas", "User-Agent": "Canvas/1.0"}
		view, err := e.svc.Create(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if view.Key != "main" || view.Name != "主渠道" || view.BaseURL != "https://gw.example.com" || !view.Enabled ||
			view.SecretSet || view.SecretKeySet || view.UpdatedBy != 5 {
			t.Fatalf("视图不符合预期：%+v", view)
		}
		if view.Headers["X-Biz"] != "canvas" || view.Headers["User-Agent"] != "Canvas/1.0" {
			t.Fatalf("自定义请求头应原样保存：%v", view.Headers)
		}
		if view.RateLimit.RPS != 5 || view.RateLimit.MaxConcurrency != 20 {
			t.Fatalf("限流不符合预期：%+v", view.RateLimit)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditChannelCreate {
			t.Fatalf("审计不符合预期：%v", got)
		}
		if len(e.notifier.Reasons) != 1 {
			t.Fatalf("应刷新一次 Registry：%v", e.notifier.Reasons)
		}
		if e.lister.Calls != 0 {
			t.Fatal("保存只做字段校验，不能向上游发请求")
		}
	})

	t.Run("请求头缺省时视图里是空对象，不是 null", func(t *testing.T) {
		e := newAchEnv(t)
		view, err := e.svc.Create(ctx, createInput())
		if err != nil || view.Headers == nil || len(view.Headers) != 0 {
			t.Fatalf("headers 应是空 map：%#v %v", view.Headers, err)
		}
	})

	t.Run("新渠道排在现有渠道之后，间隔 10", func(t *testing.T) {
		e := newAchEnv(t)
		a, _ := e.svc.Create(ctx, createInput())
		in := createInput()
		in.Key = "second"
		b, err := e.svc.Create(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		if a.Sort != 10 || b.Sort != 20 {
			t.Fatalf("sort 不符合预期：a=%d b=%d", a.Sort, b.Sort)
		}
	})

	t.Run("不传 key：后端生成 16 位十六进制标识，格式合法、互不重复，Key 凭证按它命名", func(t *testing.T) {
		e := newAchEnv(t)
		seen := map[string]bool{}
		for i := 0; i < 20; i++ {
			in := createInput()
			in.Key = ""
			view, err := e.svc.Create(ctx, in)
			if err != nil {
				t.Fatal(err)
			}
			if len(view.Key) != 16 || !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(view.Key) || seen[view.Key] {
				t.Fatalf("生成的 key 不合法或重复：%q", view.Key)
			}
			seen[view.Key] = true
		}
		in := createInput()
		in.Key = ""
		view, _ := e.svc.Create(ctx, in)
		if err := e.svc.SetSecret(ctx, 1, view.Key, "sk-1"); err != nil {
			t.Fatalf("生成的 key 应能用来设置 Key：%v", err)
		}
	})

	t.Run("生成的 key 与已有渠道撞上时自动重试", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.Key = ""
		e.svc.SetKeyGenForTest(func() string {
			if e.repo.Channels["aaaaaaaaaaaaaaaa"] == nil {
				return "aaaaaaaaaaaaaaaa"
			}
			return "bbbbbbbbbbbbbbbb"
		})
		e.repo.Channels["aaaaaaaaaaaaaaaa"] = &model.AIChannel{Key: "aaaaaaaaaaaaaaaa", Name: "x", BaseURL: "https://x.example.com", Enabled: true}
		view, err := e.svc.Create(ctx, in)
		if err != nil || view.Key != "bbbbbbbbbbbbbbbb" {
			t.Fatalf("撞 key 应重新生成：%+v %v", view, err)
		}
	})

	t.Run("显式传了 key 仍按它创建，格式不对仍报错", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.Key = "my-gw"
		if v, err := e.svc.Create(ctx, in); err != nil || v.Key != "my-gw" {
			t.Fatalf("%+v %v", v, err)
		}
	})

	t.Run("key 已存在 409", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput())
		_, err := e.svc.Create(ctx, createInput())
		adminWantCode(t, err, errcode.ErrChannelExists.Code)
	})

	tooMany := map[string]string{}
	for i := 0; i < 33; i++ {
		tooMany["X-H"+strings.Repeat("a", i+1)] = "v"
	}
	tests := []struct {
		name    string
		mutate  func(in *ChannelCreateInput)
		wantMsg string
	}{
		{"key 含大写", func(in *ChannelCreateInput) { in.Key = "Main" }, "key"},
		{"key 以连字符开头", func(in *ChannelCreateInput) { in.Key = "-main" }, "key"},
		{"key 超过 64 字符", func(in *ChannelCreateInput) { in.Key = strings.Repeat("a", 65) }, "key"},
		{"名称为空白", func(in *ChannelCreateInput) { in.Name = "  " }, "name"},
		{"地址为空", func(in *ChannelCreateInput) { in.BaseURL = "" }, "base_url"},
		{"地址不是 http", func(in *ChannelCreateInput) { in.BaseURL = "ftp://x.example.com" }, "base_url"},
		{"地址带用户名密码", func(in *ChannelCreateInput) { in.BaseURL = "https://u:p@x.example.com" }, "base_url"},
		{"地址带查询参数", func(in *ChannelCreateInput) { in.BaseURL = "https://x.example.com/?a=1" }, "base_url"},
		{"限流为负", func(in *ChannelCreateInput) { in.RateLimit = provider.RateLimit{RPS: -1} }, "rate_limit"},
		{"请求头：Authorization 不能自定义", func(in *ChannelCreateInput) { in.Headers = map[string]string{"Authorization": "x"} }, "Authorization"},
		{"请求头：Host 不能自定义", func(in *ChannelCreateInput) { in.Headers = map[string]string{"host": "x"} }, "host"},
		{"请求头：Cookie 不能自定义", func(in *ChannelCreateInput) { in.Headers = map[string]string{"Cookie": "x"} }, "Cookie"},
		{"请求头：名称含空格", func(in *ChannelCreateInput) { in.Headers = map[string]string{"X Bad": "x"} }, "不合法"},
		{"请求头：名称为空", func(in *ChannelCreateInput) { in.Headers = map[string]string{"": "x"} }, "不合法"},
		{"请求头：同名（不区分大小写）重复", func(in *ChannelCreateInput) { in.Headers = map[string]string{"X-A": "1", "x-a": "2"} }, "重复"},
		{"请求头：值含换行（防注入）", func(in *ChannelCreateInput) { in.Headers = map[string]string{"X-A": "a\r\nX-Evil: 1"} }, "换行"},
		{"请求头：值过长", func(in *ChannelCreateInput) { in.Headers = map[string]string{"X-A": strings.Repeat("a", 1025)} }, "1024"},
		{"请求头：超过 32 条", func(in *ChannelCreateInput) { in.Headers = tooMany }, "32"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newAchEnv(t)
			in := createInput()
			tt.mutate(&in)
			_, err := e.svc.Create(ctx, in)
			ce := adminWantCodeErr(t, err, errcode.ErrChannelInvalid.Code)
			if !strings.Contains(ce.Msg, tt.wantMsg) {
				t.Fatalf("错误信息应包含 %q：%s", tt.wantMsg, ce.Msg)
			}
			if len(e.repo.Channels) != 0 || len(e.repo.Audits) != 0 {
				t.Fatal("校验失败不能落库也不能写审计")
			}
		})
	}

	t.Run("多个问题一次报出", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.Name, in.BaseURL = "", ""
		_, err := e.svc.Create(ctx, in)
		ce := adminWantCodeErr(t, err, errcode.ErrChannelInvalid.Code)
		if !strings.Contains(ce.Msg, "name") || !strings.Contains(ce.Msg, "base_url") {
			t.Fatalf("应同时报 name 与 base_url：%s", ce.Msg)
		}
	})
}

func TestAIChannelService_Update(t *testing.T) {
	ctx := context.Background()
	str := func(s string) *string { return &s }
	flag := func(b bool) *bool { return &b }
	seed := func(t *testing.T) *achEnv {
		t.Helper()
		e := newAchEnv(t)
		in := createInput()
		in.Headers = map[string]string{"X-Biz": "canvas"}
		if _, err := e.svc.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
		e.repo.Audits, e.notifier.Reasons = nil, nil
		return e
	}

	t.Run("只改传了的字段，其余保持；写一条 channel.update 审计并刷新 Registry", func(t *testing.T) {
		e := seed(t)
		view, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Name: str("新名称"), ActorID: 8})
		if err != nil {
			t.Fatal(err)
		}
		if view.Name != "新名称" || view.BaseURL != "https://gw.example.com" || view.Headers["X-Biz"] != "canvas" || view.UpdatedBy != 8 {
			t.Fatalf("视图不符合预期：%+v", view)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditChannelUpdate {
			t.Fatalf("审计不符合预期：%v", got)
		}
		var d struct {
			Fields []string `json:"fields"`
		}
		_ = json.Unmarshal(e.repo.Audits[0].DetailJSON, &d)
		if len(d.Fields) != 1 || d.Fields[0] != "name" {
			t.Fatalf("审计应只记被改的字段名：%s", e.repo.Audits[0].DetailJSON)
		}
		if len(e.notifier.Reasons) != 1 {
			t.Fatalf("应刷新一次 Registry：%v", e.notifier.Reasons)
		}
	})

	t.Run("headers 传了就整体替换（含空对象），不传保持原样；审计不记请求头的值", func(t *testing.T) {
		e := seed(t)
		view, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Headers: map[string]string{"X-New": "secret-ish-value"}})
		if err != nil || len(view.Headers) != 1 || view.Headers["X-New"] != "secret-ish-value" {
			t.Fatalf("应整体替换：%+v %v", view, err)
		}
		if strings.Contains(string(e.repo.Audits[len(e.repo.Audits)-1].DetailJSON), "secret-ish-value") {
			t.Fatal("审计不能记请求头的值")
		}
		view, err = e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Headers: map[string]string{}})
		if err != nil || len(view.Headers) != 0 {
			t.Fatalf("空对象应清空请求头：%+v %v", view, err)
		}
	})

	t.Run("启停另记一条带前后值的审计", func(t *testing.T) {
		e := seed(t)
		if _, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Enabled: flag(false)}); err != nil {
			t.Fatal(err)
		}
		got := e.repo.AuditActions()
		if len(got) != 1 || got[0] != model.AuditChannelEnable {
			t.Fatalf("只改启停应只有一条 channel.enable：%v", got)
		}
	})

	t.Run("没有变化不写审计", func(t *testing.T) {
		e := seed(t)
		if _, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Name: str("主渠道")}); err != nil {
			t.Fatal(err)
		}
		if len(e.repo.Audits) != 0 {
			t.Fatalf("没有变化不该写审计：%v", e.repo.AuditActions())
		}
	})

	t.Run("sort 不能通过更新修改", func(t *testing.T) {
		e := seed(t)
		before, _ := e.svc.Get(ctx, "main")
		view, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "main", Name: str("x")})
		if err != nil || view.Sort != before.Sort {
			t.Fatalf("sort 不应变化：%+v %v", view, err)
		}
	})

	t.Run("校验失败 400，不落库不写审计", func(t *testing.T) {
		for name, in := range map[string]ChannelUpdateInput{
			"名称为空":               {Key: "main", Name: str(" ")},
			"地址不合法":              {Key: "main", BaseURL: str("not-a-url")},
			"限流为负":               {Key: "main", RateLimit: &provider.RateLimit{MaxRunning: -1}},
			"请求头含 Authorization": {Key: "main", Headers: map[string]string{"Authorization": "x"}},
		} {
			e := seed(t)
			_, err := e.svc.Update(ctx, in)
			adminWantCode(t, err, errcode.ErrChannelInvalid.Code)
			if len(e.repo.Audits) != 0 {
				t.Fatalf("%s：失败不该写审计", name)
			}
			if got, _ := e.svc.Get(ctx, "main"); got.Name != "主渠道" || got.BaseURL != "https://gw.example.com" {
				t.Fatalf("%s：渠道不应被改动：%+v", name, got)
			}
		}
	})

	t.Run("渠道不存在 404", func(t *testing.T) {
		e := newAchEnv(t)
		_, err := e.svc.Update(ctx, ChannelUpdateInput{Key: "nope", Name: str("x")})
		adminWantCode(t, err, errcode.ErrChannelNotFound.Code)
	})
}

func TestAIChannelService_GetAndList(t *testing.T) {
	ctx := context.Background()

	t.Run("没有渠道时列表是空数组", func(t *testing.T) {
		e := newAchEnv(t)
		list, err := e.svc.List(ctx)
		if err != nil || list == nil || len(list) != 0 {
			t.Fatalf("结果不符合预期：%#v %v", list, err)
		}
	})

	t.Run("列表按渠道顺序（sort）排，secret_set 与 secret_key_set 正确；详情一致；响应不含任何 Key", func(t *testing.T) {
		e := newAchEnv(t)
		in := createInput()
		in.Key = "b-chan"
		_, _ = e.svc.Create(ctx, in)
		in.Key = "a-chan"
		_, _ = e.svc.Create(ctx, in)
		if err := e.svc.SetSecret(ctx, 1, "b-chan", "sk-secret"); err != nil {
			t.Fatal(err)
		}
		if err := e.svc.SetSecretKey(ctx, 1, "b-chan", "sk-second"); err != nil {
			t.Fatal(err)
		}
		list, err := e.svc.List(ctx)
		// b-chan 先创建，sort 更小，排在前面（不是按 key 排）
		if err != nil || len(list) != 2 || list[0].Key != "b-chan" || list[1].Key != "a-chan" {
			t.Fatalf("列表不符合预期：%+v %v", list, err)
		}
		if !list[0].SecretSet || !list[0].SecretKeySet || list[1].SecretSet || list[1].SecretKeySet {
			t.Fatalf("secret_set / secret_key_set 不符合预期：%+v", list)
		}
		one, err := e.svc.Get(ctx, "b-chan")
		if err != nil || !one.SecretSet || !one.SecretKeySet {
			t.Fatalf("详情不符合预期：%+v %v", one, err)
		}
		b, _ := json.Marshal(list)
		if strings.Contains(string(b), "sk-secret") || strings.Contains(string(b), "sk-second") {
			t.Fatalf("响应里不能出现 Key：%s", b)
		}
	})

	t.Run("详情：渠道不存在 404", func(t *testing.T) {
		e := newAchEnv(t)
		_, err := e.svc.Get(ctx, "nope")
		adminWantCode(t, err, errcode.ErrChannelNotFound.Code)
	})
}

func TestAIChannelService_SetSecret(t *testing.T) {
	ctx := context.Background()

	t.Run("成功：加密存入 channel:<key>，审计不含值，视图 secret_set=true", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput())
		e.repo.Audits = nil
		if err := e.svc.SetSecret(ctx, 6, "main", "  sk-abc-123  "); err != nil {
			t.Fatal(err)
		}
		row := e.repo.Secrets["channel:main"]
		if row == nil || strings.Contains(string(row.Ciphertext), "sk-abc-123") || row.UpdatedBy != 6 {
			t.Fatalf("凭证行不符合预期：%+v", row)
		}
		// 能读回去（首尾空白被去掉）
		if v, err := e.cfg.Get(ctx, "channel:main"); err != nil || v != "sk-abc-123" {
			t.Fatalf("解密结果不符合预期：%q %v", v, err)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditChannelSecret {
			t.Fatalf("审计不符合预期：%v", got)
		}
		a := e.repo.Audits[0]
		b, _ := json.Marshal(a)
		if a.ActorID != 6 || a.TargetKey != "main" || strings.Contains(string(b), "sk-abc") || len(a.DetailJSON) != 0 {
			t.Fatalf("审计不应带任何凭证内容：%s", b)
		}
		if view, _ := e.svc.Get(ctx, "main"); !view.SecretSet {
			t.Fatal("secret_set 应为 true")
		}
	})

	t.Run("渠道不存在 404，且不会写出无主凭证", func(t *testing.T) {
		e := newAchEnv(t)
		adminWantCode(t, e.svc.SetSecret(ctx, 1, "nope", "sk-1"), errcode.ErrChannelNotFound.Code)
		if len(e.repo.Secrets) != 0 {
			t.Fatal("不该写入凭证")
		}
	})

	t.Run("空值 400；没有主密钥 500；都不写审计", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput())
		e.repo.Audits = nil
		adminWantCode(t, e.svc.SetSecret(ctx, 1, "main", "   "), errcode.ErrInvalidParams.Code)

		noKey := NewAIConfigService(e.repo, e.repo, e.repo, "")
		svc := NewAIChannelService(e.repo, noKey, noKey, e.lister, e.repo, e.notifier)
		adminWantCode(t, svc.SetSecret(ctx, 1, "main", "sk-1"), errcode.ErrInternal.Code)
		if len(e.repo.Audits) != 0 {
			t.Fatalf("失败不该写审计：%v", e.repo.AuditActions())
		}
	})
}

func TestAIChannelService_SetSecretKey(t *testing.T) {
	ctx := context.Background()

	t.Run("设置：存入 channel:<key>:secret，与 API Key 互不影响，审计不含值", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput())
		_ = e.svc.SetSecret(ctx, 1, "main", "api-key-1")
		e.repo.Audits = nil
		if err := e.svc.SetSecretKey(ctx, 6, "main", "  secret-key-2 "); err != nil {
			t.Fatal(err)
		}
		if v, _ := e.cfg.Get(ctx, "channel:main:secret"); v != "secret-key-2" {
			t.Fatalf("Secret Key 应能读回：%q", v)
		}
		if v, _ := e.cfg.Get(ctx, "channel:main"); v != "api-key-1" {
			t.Fatalf("API Key 不应被覆盖：%q", v)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditChannelSecKey || len(e.repo.Audits[0].DetailJSON) != 0 {
			t.Fatalf("审计不符合预期：%v %s", got, e.repo.Audits[0].DetailJSON)
		}
		if view, _ := e.svc.Get(ctx, "main"); !view.SecretKeySet || !view.SecretSet {
			t.Fatalf("视图不符合预期：%+v", view)
		}
	})

	t.Run("空串清除：删除凭证行，视图 secret_key_set=false，API Key 保持", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput())
		_ = e.svc.SetSecret(ctx, 1, "main", "api-key-1")
		_ = e.svc.SetSecretKey(ctx, 1, "main", "secret-key-2")
		if err := e.svc.SetSecretKey(ctx, 1, "main", "  "); err != nil {
			t.Fatal(err)
		}
		if _, ok := e.repo.Secrets["channel:main:secret"]; ok {
			t.Fatal("Secret Key 应已删除")
		}
		view, _ := e.svc.Get(ctx, "main")
		if view.SecretKeySet || !view.SecretSet {
			t.Fatalf("视图不符合预期：%+v", view)
		}
		// 清除时本实例的明文缓存也要一并清掉
		if _, err := e.cfg.Get(ctx, "channel:main:secret"); err == nil {
			t.Fatal("清除后不应还能读到明文")
		}
	})

	t.Run("清除一个没设置过的 Secret Key 也算成功（幂等）", func(t *testing.T) {
		e := newAchEnv(t)
		_, _ = e.svc.Create(ctx, createInput())
		if err := e.svc.SetSecretKey(ctx, 1, "main", ""); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("渠道不存在 404", func(t *testing.T) {
		e := newAchEnv(t)
		adminWantCode(t, e.svc.SetSecretKey(ctx, 1, "nope", "x"), errcode.ErrChannelNotFound.Code)
	})
}

func TestAIChannelService_SetOrder(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T) *achEnv {
		t.Helper()
		e := newAchEnv(t)
		for _, k := range []string{"a", "b", "c"} {
			in := createInput()
			in.Key = k
			if _, err := e.svc.Create(ctx, in); err != nil {
				t.Fatal(err)
			}
		}
		e.repo.Audits, e.notifier.Reasons = nil, nil
		return e
	}
	order := func(e *achEnv) []string {
		list, _ := e.svc.List(ctx)
		out := make([]string, 0, len(list))
		for _, c := range list {
			out = append(out, c.Key)
		}
		return out
	}

	t.Run("成功：按新顺序重写 sort，写审计并刷新 Registry", func(t *testing.T) {
		e := seed(t)
		if err := e.svc.SetOrder(ctx, 7, []string{"c", "a", "b"}); err != nil {
			t.Fatal(err)
		}
		if got := order(e); strings.Join(got, ",") != "c,a,b" {
			t.Fatalf("顺序不对：%v", got)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditChannelSort {
			t.Fatalf("审计不符合预期：%v", got)
		}
		if len(e.notifier.Reasons) != 1 {
			t.Fatalf("渠道顺序决定用户看到的模型顺序，应刷新 Registry：%v", e.notifier.Reasons)
		}
	})

	t.Run("不是全集（漏、重复、不存在）400，顺序不变", func(t *testing.T) {
		for name, keys := range map[string][]string{
			"漏掉渠道": {"a", "b"},
			"重复":   {"a", "a", "b"},
			"不存在的": {"a", "b", "ghost"},
			"空列表":  {},
			"多出一个": {"a", "b", "c", "a"},
		} {
			e := seed(t)
			adminWantCode(t, e.svc.SetOrder(ctx, 1, keys), errcode.ErrInvalidParams.Code)
			if got := order(e); strings.Join(got, ",") != "a,b,c" {
				t.Fatalf("%s：顺序不应改变：%v", name, got)
			}
			if len(e.repo.Audits) != 0 {
				t.Fatalf("%s：失败不该写审计", name)
			}
		}
	})
}

func TestAIChannelService_PullModels(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T, withKey bool) *achEnv {
		t.Helper()
		e := newAchEnv(t)
		in := createInput()
		in.Headers = map[string]string{"X-Biz": "canvas"}
		if _, err := e.svc.Create(ctx, in); err != nil {
			t.Fatal(err)
		}
		if withKey {
			if err := e.svc.SetSecret(ctx, 1, "main", "sk-live-123"); err != nil {
				t.Fatal(err)
			}
		}
		e.lister.IDs = []string{"gpt-image-2", "sd2.0-c1", "nano-banana2"}
		return e
	}

	t.Run("成功：用渠道的地址、请求头与 API Key 请求上游，标记已存在的模型，不落库", func(t *testing.T) {
		e := seed(t, true)
		// 渠道下已有 sd2.0-c1；别的渠道下的 gpt-image-2 不算
		e.repo.Channels["other"] = &model.AIChannel{Key: "other", Name: "other", BaseURL: "https://o.example.com", Enabled: true}
		if _, err := e.cfg.ImportChannelModels(ctx, 1, "main", []string{"sd2.0-c1"}); err != nil {
			t.Fatal(err)
		}
		if _, err := e.cfg.ImportChannelModels(ctx, 1, "other", []string{"gpt-image-2"}); err != nil {
			t.Fatal(err)
		}
		before := len(e.repo.Models)

		res, err := e.svc.PullModels(ctx, "main")
		if err != nil {
			t.Fatal(err)
		}
		if res.Total != 3 || len(res.Models) != 3 {
			t.Fatalf("结果不符合预期：%+v", res)
		}
		exists := map[string]bool{}
		for _, m := range res.Models {
			exists[m.ID] = m.Exists
		}
		if exists["sd2.0-c1"] != true || exists["gpt-image-2"] != false || exists["nano-banana2"] != false {
			t.Fatalf("exists 标记不对：%v", exists)
		}
		if e.lister.APIKey != "sk-live-123" || e.lister.Channel.BaseURL != "https://gw.example.com" || e.lister.Channel.Headers["X-Biz"] != "canvas" {
			t.Fatalf("请求上游用的渠道信息不对：%+v key=%q", e.lister.Channel, e.lister.APIKey)
		}
		if len(e.repo.Models) != before {
			t.Fatal("拉取不落库")
		}
	})

	t.Run("API Key 没设置 409，不发请求", func(t *testing.T) {
		e := seed(t, false)
		_, err := e.svc.PullModels(ctx, "main")
		adminWantCode(t, err, errcode.ErrChannelSecretUnset.Code)
		if e.lister.Calls != 0 {
			t.Fatal("没有 Key 时不该向上游发请求")
		}
	})

	t.Run("渠道不存在 404", func(t *testing.T) {
		e := seed(t, true)
		_, err := e.svc.PullModels(ctx, "nope")
		adminWantCode(t, err, errcode.ErrChannelNotFound.Code)
	})

	t.Run("上游失败：翻译成拉取失败 502，消息来自宿主，Key 明文被抹掉", func(t *testing.T) {
		for name, perr := range map[string]error{
			"401":  &provider.PullError{Reason: provider.PullUnauthorized, Status: 401, Message: "上游拒绝了请求（HTTP 401），key=sk-live-123"},
			"不可达":  &provider.PullError{Reason: provider.PullUnreachable, Message: "连接 gw.example.com 超时"},
			"格式":   &provider.PullError{Reason: provider.PullFormat, Message: "不是 OpenAI 形状"},
			"其他错误": errors.New("意外：sk-live-123"),
		} {
			e := seed(t, true)
			e.lister.Err = perr
			_, err := e.svc.PullModels(ctx, "main")
			ce := adminWantCodeErr(t, err, errcode.ErrChannelPullFailed.Code)
			if strings.Contains(ce.Msg, "sk-live-123") || ce.Msg == "" {
				t.Fatalf("%s：消息不能为空，也不能含 Key 明文：%q", name, ce.Msg)
			}
		}
	})

	t.Run("没有注入拉取器时返回内部错误，而不是 panic", func(t *testing.T) {
		e := seed(t, true)
		svc := NewAIChannelService(e.repo, e.cfg, e.cfg, nil, e.repo, e.notifier)
		_, err := svc.PullModels(ctx, "main")
		adminWantCode(t, err, errcode.ErrInternal.Code)
	})
}

func TestAIChannelService_ImportModels(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T) *achEnv {
		t.Helper()
		e := newAchEnv(t)
		if _, err := e.svc.Create(ctx, createInput()); err != nil {
			t.Fatal(err)
		}
		e.repo.Audits = nil
		return e
	}
	bodyOf := func(t *testing.T, e *achEnv, key string) map[string]any {
		t.Helper()
		rev, err := e.repo.GetModelConfig(ctx, key)
		if err != nil {
			t.Fatalf("模型 %s 应已创建：%v", key, err)
		}
		var m map[string]any
		if err := json.Unmarshal(rev.BodyJSON, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}

	t.Run("创建“待配置”模型：未启用、kind 为空、没有协议，label 取上游 id，排在渠道内现有模型之后", func(t *testing.T) {
		e := seed(t)
		res, err := e.svc.ImportModels(ctx, 9, "main", []string{"gpt-image-2", "sd2.0-c1"})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Created) != 2 || len(res.Skipped) != 0 || res.Created[0].Key != "gpt-image-2" || res.Created[1].Key != "sd2.0-c1" {
			t.Fatalf("结果不符合预期：%+v", res)
		}
		body := bodyOf(t, e, "gpt-image-2")
		ch := body["channels"].([]any)[0].(map[string]any)
		if body["kind"] != "" || body["label"] != "gpt-image-2" || body["enabled"] != false ||
			ch["channel"] != "main" || ch["upstream_model"] != "gpt-image-2" || ch["protocol"] != nil {
			t.Fatalf("正文不符合预期：%v", body)
		}
		p1, p2 := e.repo.Models["gpt-image-2"], e.repo.Models["sd2.0-c1"]
		if p1.Enabled || p1.Kind != "" || p1.Sort != 10 || p2.Sort != 20 {
			t.Fatalf("指针行不符合预期：%+v %+v", p1, p2)
		}
		// 待配置的模型绝不能出现在用户可见的清单里
		list, _ := e.cfg.ListModels(ctx, "")
		if len(list) != 0 {
			t.Fatalf("待配置的模型不应对用户可见：%+v", list)
		}
		if got := e.repo.AuditActions(); len(got) != 1 || got[0] != model.AuditModelImport {
			t.Fatalf("审计不符合预期：%v", got)
		}
	})

	t.Run("渠道下已有的同名上游模型跳过，不重复创建", func(t *testing.T) {
		e := seed(t)
		_, _ = e.svc.ImportModels(ctx, 9, "main", []string{"a"})
		res, err := e.svc.ImportModels(ctx, 9, "main", []string{"a", "b"})
		if err != nil || len(res.Created) != 1 || res.Created[0].UpstreamModel != "b" || len(res.Skipped) != 1 || res.Skipped[0] != "a" {
			t.Fatalf("结果不符合预期：%+v %v", res, err)
		}
		if len(e.repo.Models) != 2 {
			t.Fatalf("应只有 2 个模型：%d", len(e.repo.Models))
		}
		// 新导入的排在已有模型之后
		if e.repo.Models["b"].Sort <= e.repo.Models["a"].Sort {
			t.Fatalf("新模型应排在已有模型之后：a=%d b=%d", e.repo.Models["a"].Sort, e.repo.Models["b"].Sort)
		}
	})

	t.Run("key 规则：不合法字符换成 -；与其他渠道的模型 key 冲突时加渠道前缀，仍冲突再加序号", func(t *testing.T) {
		e := seed(t)
		e.repo.Channels["other"] = &model.AIChannel{Key: "other", Name: "other", BaseURL: "https://o.example.com", Enabled: true}
		if _, err := e.svc.ImportModels(ctx, 1, "other", []string{"sd2.0-c1"}); err != nil {
			t.Fatal(err)
		}
		res, err := e.svc.ImportModels(ctx, 1, "main", []string{"sd2.0-c1", "deepseek-ai/DeepSeek-V3", "中文", "模型 B"})
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for _, c := range res.Created {
			got[c.UpstreamModel] = c.Key
		}
		if got["sd2.0-c1"] != "main-sd2.0-c1" {
			t.Fatalf("冲突时应加渠道前缀：%v", got)
		}
		if got["deepseek-ai/DeepSeek-V3"] != "deepseek-ai-DeepSeek-V3" {
			t.Fatalf("斜杠应换成 -：%v", got)
		}
		for up, key := range got {
			if key == "" || !aiKeyOK(key) {
				t.Fatalf("%q 生成的 key %q 不合法", up, key)
			}
		}
		// 两个全是非法字符的 id 不能生成同一个 key
		if got["中文"] == got["模型 B"] {
			t.Fatalf("不同的上游模型不能生成同一个 key：%v", got)
		}
	})

	t.Run("参数：空列表、全是空白 400；超过 500 个提示分批", func(t *testing.T) {
		e := seed(t)
		_, err := e.svc.ImportModels(ctx, 1, "main", nil)
		adminWantCode(t, err, errcode.ErrInvalidParams.Code)
		_, err = e.svc.ImportModels(ctx, 1, "main", []string{"  ", ""})
		adminWantCode(t, err, errcode.ErrInvalidParams.Code)
		many := make([]string, 501)
		for i := range many {
			many[i] = "m" + strings.Repeat("x", i%7) + string(rune('a'+i%26)) + strings.Repeat("y", i/26)
		}
		_, err = e.svc.ImportModels(ctx, 1, "main", many)
		ce := adminWantCodeErr(t, err, errcode.ErrInvalidParams.Code)
		if !strings.Contains(ce.Msg, "500") {
			t.Fatalf("应提示单次上限：%s", ce.Msg)
		}
		if len(e.repo.Models) != 0 {
			t.Fatal("参数不合法时不能创建任何模型")
		}
	})

	t.Run("ids 里的重复与首尾空白被规整", func(t *testing.T) {
		e := seed(t)
		res, err := e.svc.ImportModels(ctx, 1, "main", []string{" a ", "a", "b"})
		if err != nil || len(res.Created) != 2 {
			t.Fatalf("应只创建 a、b 两个：%+v %v", res, err)
		}
	})

	t.Run("渠道不存在 404", func(t *testing.T) {
		e := seed(t)
		_, err := e.svc.ImportModels(ctx, 1, "nope", []string{"a"})
		adminWantCode(t, err, errcode.ErrChannelNotFound.Code)
	})
}

// aiKeyOK 判断模型 key 是否符合 aiConfigKeyRe 的字符集与长度。
func aiKeyOK(key string) bool {
	if key == "" || len(key) > 128 {
		return false
	}
	for i, r := range key {
		alnum := r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
		if i == 0 && !alnum || !alnum && r != '_' && r != '.' && r != '-' {
			return false
		}
	}
	return true
}

func TestAIChannelService_UpstreamLimits(t *testing.T) {
	ctx := context.Background()
	seed := func(t *testing.T, withKey bool) *achEnv {
		t.Helper()
		e := newAchEnv(t)
		if _, err := e.svc.Create(ctx, createInput()); err != nil {
			t.Fatal(err)
		}
		if withKey {
			if err := e.svc.SetSecret(ctx, 1, "main", "sk-live-123"); err != nil {
				t.Fatal(err)
			}
		}
		e.lister.Limits = map[string]provider.UpstreamLimits{"claude-x": {Window: 200000, Output: 64000}}
		return e
	}

	t.Run("上游给了规格：返回它，并用渠道的 Key 请求", func(t *testing.T) {
		e := seed(t, true)
		got, ok, err := e.svc.UpstreamLimits(ctx, "main", "claude-x")
		if err != nil || !ok || got.Window != 200000 || got.Output != 64000 {
			t.Fatalf("实际 %+v ok=%v err=%v", got, ok, err)
		}
		if e.lister.APIKey != "sk-live-123" {
			t.Fatalf("应带渠道的 Key：%q", e.lister.APIKey)
		}
	})
	t.Run("上游没给这个模型的规格：ok=false，不是错误", func(t *testing.T) {
		e := seed(t, true)
		if _, ok, err := e.svc.UpstreamLimits(ctx, "main", "other"); ok || err != nil {
			t.Fatalf("ok=%v err=%v", ok, err)
		}
	})
	t.Run("Key 没设置：409，不发请求", func(t *testing.T) {
		e := seed(t, false)
		_, _, err := e.svc.UpstreamLimits(ctx, "main", "claude-x")
		adminWantCode(t, err, errcode.ErrChannelSecretUnset.Code)
		if e.lister.Calls != 0 {
			t.Fatal("没有 Key 不该请求上游")
		}
	})
	t.Run("渠道不存在", func(t *testing.T) {
		e := seed(t, true)
		_, _, err := e.svc.UpstreamLimits(ctx, "nope", "claude-x")
		adminWantCode(t, err, errcode.ErrChannelNotFound.Code)
	})
	t.Run("上游失败：返回拉取失败的业务错误", func(t *testing.T) {
		e := seed(t, true)
		e.lister.Err = &provider.PullError{Reason: provider.PullStatus, Status: 502, Message: "上游返回 HTTP 502"}
		_, _, err := e.svc.UpstreamLimits(ctx, "main", "claude-x")
		adminWantCode(t, err, errcode.ErrChannelPullFailed.Code)
	})
}
