package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	. "video-canvas/internal/service"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/provider/dsl"
	"video-canvas/internal/service/aiconfigfake"
)

const aicRHKey = "rh-key-SECRET-0123456789abcdef"

// aicImportSetup 启动一个模拟 RunningHub 的 httptest 服务，并准备指向它的平台配置与凭证。
// handler 收到的请求会被断言路径、查询参数。
func aicImportSetup(t *testing.T, respond func(w http.ResponseWriter, r *http.Request)) (*AIConfigService, *aiconfigfake.HTTPFactory, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(respond))
	t.Cleanup(srv.Close)

	svc, _, _ := aicNewSvc()
	fac := &aiconfigfake.HTTPFactory{Client: srv.Client()}
	svc.SetHTTPClientFactory(fac)
	if err := svc.SetSecret(context.Background(), "runninghub_api_key", aicRHKey, 1); err != nil {
		t.Fatal(err)
	}
	body := `{"dsl":1,"key":"runninghub","name":"RH","base_url":"` + srv.URL + `","allowed_hosts":["127.0.0.1","*.runninghub.cn"],"auth":{"type":"bearer","secret":"runninghub_api_key"}}`
	if _, err := svc.SaveDraft(context.Background(), model.ConfigTargetProvider, "", true, json.RawMessage(body), "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Publish(context.Background(), model.ConfigTargetProvider, "runninghub", 1); err != nil {
		t.Fatal(err)
	}
	return svc, fac, srv
}

const aicNodesTypical = `{"code":0,"msg":"success","data":{"nodeInfoList":[
  {"nodeId":"6","fieldName":"text","fieldValue":"a cat","description":"提示词"},
  {"nodeId":"12","fieldName":"image","fieldValue":"example.png","description":"首帧图片"},
  {"nodeId":"30","fieldName":"length","fieldValue":81,"description":"帧数"},
  {"nodeId":"31","fieldName":"video","fieldValue":"a.mp4"},
  {"nodeId":"32","fieldName":"audio_file","fieldValue":"a.wav"},
  {"nodeId":"33","fieldName":"seed","fieldValue":"123456789012345678"},
  {"nodeId":"34","fieldName":"image_strength","fieldValue":0.5},
  {"nodeId":"35","fieldName":"enable","fieldValue":true}
]}}`

func aicOKHandler(t *testing.T, payload string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/webapp/apiCallDemo" || r.Method != http.MethodGet {
			t.Errorf("请求路径或方法不对：%s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("webappId") != "2093984571330498561" || r.URL.Query().Get("apiKey") != aicRHKey {
			t.Errorf("查询参数不对：%s", r.URL.RawQuery)
		}
		if r.Header.Get("Authorization") != "Bearer "+aicRHKey {
			t.Errorf("鉴权头不对：%s", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(payload))
	}
}

func TestAIConfigService_ImportRunningHub_Success(t *testing.T) {
	svc, fac, _ := aicImportSetup(t, aicOKHandler(t, aicNodesTypical))
	res, err := svc.ImportRunningHub(context.Background(), "2093984571330498561", "", "")
	aicWantCode(t, err, 0)

	t.Run("受 SSRF 防护的客户端拿到平台的白名单和超时", func(t *testing.T) {
		if len(fac.AllowedHosts) != 2 || fac.AllowedHosts[1] != "*.runninghub.cn" || fac.Timeout != 30*time.Second {
			t.Fatalf("工厂参数不对：%v %v", fac.AllowedHosts, fac.Timeout)
		}
	})

	var draft dsl.ModelConfig
	if err := json.Unmarshal(res.Draft, &draft); err != nil {
		t.Fatalf("草稿应是合法的 Model JSON：%v\n%s", err, res.Draft)
	}

	t.Run("草稿基本字段", func(t *testing.T) {
		if draft.Key != "rh-2093984571330498561" || draft.Kind != "video" || draft.Provider != "runninghub" ||
			draft.Enabled || draft.Credits != 10 || draft.Deadline.D() != 30*time.Minute {
			t.Fatalf("草稿基本字段不符合预期：%+v", draft)
		}
		if draft.Params["webappId"] != "2093984571330498561" || draft.Params["instanceType"] != "default" {
			t.Fatalf("params 不符合预期：%v", draft.Params)
		}
		if !strings.Contains(draft.Output.Select, "mp4") || draft.Output.Media != "video" {
			t.Fatalf("output 不符合预期：%+v", draft.Output)
		}
	})

	t.Run("类型推断、输入名、端口", func(t *testing.T) {
		want := []struct {
			name, typ string
		}{
			{"prompt", "text"}, {"image", "image"}, {"length", "number"}, {"video", "video"},
			{"audio_file", "audio"}, {"seed", "text"}, {"image_strength", "number"}, {"enable", "boolean"},
		}
		if len(draft.InputSchema) != len(want) {
			t.Fatalf("字段数期望 %d，实际 %d", len(want), len(draft.InputSchema))
		}
		for i, w := range want {
			e := draft.InputSchema[i]
			if e.Name != w.name || e.Type != w.typ {
				t.Fatalf("第 %d 个字段期望 %s/%s，实际 %s/%s", i, w.name, w.typ, e.Name, e.Type)
			}
		}
		p, _ := draft.InputSchema.Get("prompt")
		if !p.Required || p.Port != "text" || p.Label != "提示词" || p.Default != "a cat" || p.MaxLength != 2000 {
			t.Fatalf("prompt 字段不符合预期：%+v", p)
		}
		img, _ := draft.InputSchema.Get("image")
		if !img.Required || img.Port != "image" || img.Label != "首帧图片" {
			t.Fatalf("image 字段不符合预期：%+v", img)
		}
		ln, _ := draft.InputSchema.Get("length")
		if ln.Default != float64(81) || !ln.Advanced || ln.Required {
			t.Fatalf("length 字段不符合预期：%+v", ln)
		}
		seed, _ := draft.InputSchema.Get("seed")
		if seed.Default != "123456789012345678" {
			t.Fatalf("大整数文本值应原样保留：%+v", seed)
		}
	})

	t.Run("mapping 预填表达式，媒体字段用 files", func(t *testing.T) {
		m := draft.Mapping.(map[string]any)["nodeInfoList"].([]any)
		if len(m) != 8 {
			t.Fatalf("mapping 数量不对：%d", len(m))
		}
		first := m[0].(map[string]any)
		if first["nodeId"] != "6" || first["fieldName"] != "text" || first["fieldValue"] != "${ input.prompt }" {
			t.Fatalf("第一条 mapping 不对：%v", first)
		}
		if m[1].(map[string]any)["fieldValue"] != "${ files.image }" ||
			m[3].(map[string]any)["fieldValue"] != "${ files.video }" ||
			m[4].(map[string]any)["fieldValue"] != "${ files.audio_file }" {
			t.Fatalf("媒体字段应使用 files：%v", m)
		}
		if m[2].(map[string]any)["fieldValue"] != "${ input.length }" {
			t.Fatalf("数字字段应使用 input：%v", m[2])
		}
	})

	t.Run("返回节点原始信息供勾选参考", func(t *testing.T) {
		if len(res.Nodes) != 8 || res.Nodes[0].NodeID != "6" || res.Nodes[0].InputName != "prompt" || res.Nodes[0].InputType != "text" ||
			res.Nodes[0].Description != "提示词" || res.Nodes[0].FieldValue != "a cat" {
			t.Fatalf("节点信息不符合预期：%+v", res.Nodes[0])
		}
		if res.Nodes[2].FieldValue != int64(81) {
			t.Fatalf("数字原值应保留：%#v", res.Nodes[2].FieldValue)
		}
		if len(res.Warnings) == 0 {
			t.Fatal("应给出提示")
		}
	})

	t.Run("响应里不含 API Key，也不落库", func(t *testing.T) {
		if strings.Contains(aicJSON(res), aicRHKey) {
			t.Fatal("导入结果不得包含 API Key")
		}
		if _, err := svc.Repo().GetModelPointer(context.Background(), "rh-2093984571330498561"); err == nil {
			t.Fatal("导入只返回建议，不应落库")
		}
	})
}

func TestAIConfigService_ImportRunningHub_TolerantParsing(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    []string // 期望的输入名
	}{
		{"顶层 nodeInfoList", `{"nodeInfoList":[{"nodeId":1,"fieldName":"text","fieldValue":"x"}]}`, []string{"prompt"}},
		{"字段名大小写与下划线差异", `{"code":"0","data":{"NodeInfoList":[{"NodeID":"7","Field_Name":"Text","Field_Value":"x","FieldType":"STRING"}]}}`, []string{"prompt"}},
		{"data 直接是数组", `{"code":0,"data":[{"nodeId":"1","fieldName":"image","fieldValue":""}]}`, []string{"image"}},
		{"顶层直接是数组", `[{"nodeId":"1","fieldName":"prompt","fieldValue":"x"}]`, []string{"prompt"}},
		{"code 为 200 也算成功", `{"code":200,"data":{"nodeInfoList":[{"nodeId":"1","fieldName":"text","fieldValue":"x"}]}}`, []string{"prompt"}},
		{"同名字段自动去重", `{"data":{"nodeInfoList":[{"nodeId":"1","fieldName":"image","fieldValue":""},{"nodeId":"2","fieldName":"image","fieldValue":""},{"nodeId":"3","fieldName":"image","fieldValue":""}]}}`, []string{"image", "image_2", "image_3"}},
		{"没有字段名的节点被跳过，非法输入名被规整", `{"data":{"nodeInfoList":[{"nodeId":"1"},{"nodeId":"2","fieldName":"9 Width-px","fieldValue":512}]}}`, []string{"f_9_width_px"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := aicImportSetup(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tt.payload)) })
			res, err := svc.ImportRunningHub(context.Background(), "2093984571330498561", "", "")
			aicWantCode(t, err, 0)
			var draft dsl.ModelConfig
			if err := json.Unmarshal(res.Draft, &draft); err != nil {
				t.Fatal(err)
			}
			if len(draft.InputSchema) != len(tt.want) {
				t.Fatalf("字段数期望 %v，实际 %+v", tt.want, draft.InputSchema)
			}
			for i, n := range tt.want {
				if draft.InputSchema[i].Name != n {
					t.Fatalf("第 %d 个输入名期望 %s，实际 %s", i, n, draft.InputSchema[i].Name)
				}
			}
		})
	}
}

func TestAIConfigService_ImportRunningHub_KindOverride(t *testing.T) {
	svc, _, _ := aicImportSetup(t, aicOKHandler(t, aicNodesTypical))
	res, err := svc.ImportRunningHub(context.Background(), "2093984571330498561", "image", "")
	aicWantCode(t, err, 0)
	var draft dsl.ModelConfig
	_ = json.Unmarshal(res.Draft, &draft)
	if draft.Kind != "image" || draft.Output.Media != "image" || !strings.Contains(draft.Output.Select, "png") {
		t.Fatalf("kind 应可指定：%+v", draft)
	}
}

func TestAIConfigService_ImportRunningHub_Errors(t *testing.T) {
	ctx := context.Background()
	okHandler := func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(aicNodesTypical)) }

	tests := []struct {
		name     string
		respond  func(w http.ResponseWriter, r *http.Request)
		webappID string
		kind     string
		provider string
		mutate   func(svc *AIConfigService, repo *aiconfigfake.MemRepo)
		wantCode int
	}{
		{"webappId 含非数字字符", okHandler, "123/../x", "", "", nil, errcode.ErrInvalidParams.Code},
		{"webappId 为空", okHandler, "", "", "", nil, errcode.ErrInvalidParams.Code},
		{"kind 非法", okHandler, "1", "text", "", nil, errcode.ErrInvalidParams.Code},
		{"平台不存在", okHandler, "1", "", "ghost", nil, errcode.ErrConfigNotFound.Code},
		{"凭证未设置", okHandler, "1", "", "", func(svc *AIConfigService, repo *aiconfigfake.MemRepo) { delete(repo.Secrets, "runninghub_api_key") }, errcode.ErrSecretNotSet.Code},
		{"平台没有配置 auth.secret", okHandler, "1", "", "", func(svc *AIConfigService, repo *aiconfigfake.MemRepo) {
			body := `{"dsl":1,"key":"runninghub","name":"RH","base_url":"http://x","auth":{"type":"none"}}`
			_, _ = svc.SaveDraft(ctx, model.ConfigTargetProvider, "runninghub", false, json.RawMessage(body), "", 1)
			_, _ = svc.Publish(ctx, model.ConfigTargetProvider, "runninghub", 1)
		}, errcode.ErrConfigInvalid.Code},
		{"平台 base_url 不合法", okHandler, "1", "", "", func(svc *AIConfigService, repo *aiconfigfake.MemRepo) {
			body := `{"dsl":1,"key":"runninghub","name":"RH","base_url":"ftp://x","auth":{"type":"bearer","secret":"runninghub_api_key"}}`
			_, _ = svc.SaveDraft(ctx, model.ConfigTargetProvider, "runninghub", false, json.RawMessage(body), "", 1)
			_, _ = svc.Publish(ctx, model.ConfigTargetProvider, "runninghub", 1)
		}, errcode.ErrConfigInvalid.Code},
		{"上游返回业务错误码", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":401,"msg":"invalid apikey"}`))
		}, "1", "", "", nil, errcode.ErrConfigInvalid.Code},
		{"上游 HTTP 500", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(500)
			_, _ = w.Write([]byte("boom"))
		}, "1", "", "", nil, errcode.ErrInternal.Code},
		{"上游返回的不是 JSON", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("<html>")) }, "1", "", "", nil, errcode.ErrConfigInvalid.Code},
		{"没有任何节点", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"code":0,"data":{"nodeInfoList":[]}}`))
		}, "1", "", "", nil, errcode.ErrConfigInvalid.Code},
		{"响应里没有节点列表字段", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"code":0,"data":{}}`)) }, "1", "", "", nil, errcode.ErrConfigInvalid.Code},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := aicImportSetup(t, tt.respond)
			if tt.mutate != nil {
				tt.mutate(svc, svc.Repo().(*aiconfigfake.MemRepo))
			}
			_, err := svc.ImportRunningHub(ctx, tt.webappID, tt.kind, tt.provider)
			aicWantCode(t, err, tt.wantCode)
			if err != nil && strings.Contains(err.Error(), aicRHKey) {
				t.Fatalf("错误信息不得泄露 API Key：%v", err)
			}
		})
	}

	t.Run("网络错误信息里的 API Key 被脱敏", func(t *testing.T) {
		svc, _, srv := aicImportSetup(t, okHandler)
		srv.Close() // 关掉服务，client.Do 的错误会带上完整 URL（含 apiKey）
		_, err := svc.ImportRunningHub(ctx, "1", "", "")
		aicWantCode(t, err, errcode.ErrInternal.Code)
		var e *errcode.Error
		_ = errors.As(err, &e)
		if e == nil || strings.Contains(e.Msg, aicRHKey) || !strings.Contains(e.Msg, "***") {
			t.Fatalf("应脱敏 API Key：%v", err)
		}
	})

	t.Run("未注入 HTTP 工厂", func(t *testing.T) {
		svc, _, _ := aicNewSvc()
		_, err := svc.ImportRunningHub(ctx, "1", "", "")
		aicWantCode(t, err, errcode.ErrInternal.Code)
	})
}

func TestAIInferType(t *testing.T) {
	tests := []struct {
		field, ftype string
		value        any
		want         string
	}{
		{"text", "", "x", "text"},
		{"prompt", "", "", "text"},
		{"positive_prompt", "", "", "text"},
		{"image_prompt", "", "", "text"},
		{"image", "", "a.png", "image"},
		{"first_frame_img", "", "", "image"},
		{"mask", "", "", "image"},
		{"首帧图片", "", "", "image"},
		{"video", "", "", "video"},
		{"audio", "", "", "audio"},
		{"voice", "", "", "audio"},
		{"length", "", int64(81), "number"},
		{"image_strength", "", 0.5, "number"},
		{"enable", "", true, "boolean"},
		{"whatever", "IMAGE", "", "image"},
		{"whatever", "VIDEO", "", "video"},
		{"whatever", "AUDIO", "", "audio"},
		{"whatever", "INT", "", "number"},
		{"whatever", "FLOAT", "", "number"},
		{"whatever", "SWITCH", "", "boolean"},
		{"image", "STRING", "", "text"},
		{"sampler_name", "", "euler", "text"},
	}
	for _, tt := range tests {
		t.Run(tt.field+"/"+tt.ftype, func(t *testing.T) {
			if got := AIInferType(tt.field, tt.ftype, tt.value); got != tt.want {
				t.Fatalf("期望 %s，实际 %s", tt.want, got)
			}
		})
	}
}

// 用真实的 dsl 校验器走一遍“种子平台 → 导入草稿 → 校验 → 发布 → Registry 可见”，
// 确保导入生成的草稿与 dsl 的校验规则契合（补上 label / credits 后应能直接发布）。
func TestAIConfigService_ImportDraft_PassesRealDSL(t *testing.T) {
	ctx := context.Background()
	repo := aiconfigfake.NewMemRepo()
	svc := NewAIConfigService(repo, NewDSLValidator(), "k")
	if err := svc.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if list, _ := svc.ListModels(ctx, ""); len(list) != 0 {
		t.Fatal("种子只有平台，没有模型")
	}
	nodes, err := AIParseNodeInfoList([]byte(aicNodesTypical))
	if err != nil {
		t.Fatal(err)
	}
	res, err := AIBuildImportDraft("2093984571330498561", "video", "runninghub", nodes)
	if err != nil {
		t.Fatal(err)
	}

	saved, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "", true, res.Draft, "导入", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Issues) != 0 {
		t.Fatalf("导入的草稿应通过 dsl 校验：%+v", saved.Issues)
	}
	// 凭证在此处并不需要（模型发布不检查凭证），发布后手工上架
	if _, err := svc.Publish(ctx, model.ConfigTargetModel, "rh-2093984571330498561", 1); err != nil {
		t.Fatalf("发布失败：%v", err)
	}
	if err := svc.SetModelEnabled(ctx, "rh-2093984571330498561", true, 1); err != nil {
		t.Fatal(err)
	}
	list, err := svc.ListModels(ctx, "video")
	if err != nil || len(list) != 1 || list[0].Key != "rh-2093984571330498561" {
		t.Fatalf("上架后应出现在清单里：%v %+v", err, list)
	}
	snap, err := svc.Snapshot(ctx, "rh-2093984571330498561")
	if err != nil || snap.Provider.Key != "runninghub" {
		t.Fatalf("快照应可用：%v", err)
	}
}

// RunningHub 常把数字 / 开关当前值以字符串返回，超长的默认提示词也常见：导入草稿仍应通过校验。
func TestAIConfigService_ImportDraft_StringDefaultsAndLongPrompt(t *testing.T) {
	ctx := context.Background()
	svc := NewAIConfigService(aiconfigfake.NewMemRepo(), NewDSLValidator(), "k")
	if err := svc.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	long := strings.Repeat("长提示词", 800)
	raw := fmt.Sprintf(`[
	  {"nodeId":"1","fieldName":"text","fieldValue":%q,"fieldType":"STRING","description":"提示词"},
	  {"nodeId":"2","fieldName":"megapixels","fieldValue":"1.5","fieldType":"FLOAT"},
	  {"nodeId":"3","fieldName":"value","fieldValue":"1024","fieldType":"INT"},
	  {"nodeId":"4","fieldName":"steps","fieldValue":"","fieldType":"INT"},
	  {"nodeId":"5","fieldName":"flag","fieldValue":"true","fieldType":"BOOLEAN"}
	]`, long)
	nodes, err := AIParseNodeInfoList([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	res, err := AIBuildImportDraft("1", "video", "runninghub", nodes)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := svc.SaveDraft(ctx, model.ConfigTargetModel, "", true, res.Draft, "导入", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.Issues) != 0 {
		t.Fatalf("字符串数字与超长提示词不应导致校验失败：%+v", saved.Issues)
	}
}
