package agent_test

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	. "video-canvas/internal/handler/agent"
	"video-canvas/internal/middleware"
	"video-canvas/internal/pkg/errcode"
	agentsvc "video-canvas/internal/service/agent"
)

// fakeSkillAPI 记录 handler 传给业务层的参数，并按 err 返回业务错误。
type fakeSkillAPI struct {
	err       error
	calls     []string
	gotImport agentsvc.SkillImportInput
	gotName   string
	gotVer    int
	gotOn     bool
	gotActor  uint64
}

func (f *fakeSkillAPI) rec(n string) error { f.calls = append(f.calls, n); return f.err }
func (f *fakeSkillAPI) List(context.Context, string, string) ([]agentsvc.SkillItem, error) {
	return []agentsvc.SkillItem{{Name: "demo"}}, f.rec("list")
}
func (f *fakeSkillAPI) Get(_ context.Context, n string) (*agentsvc.SkillDetail, error) {
	f.gotName = n
	return &agentsvc.SkillDetail{}, f.rec("get")
}
func (f *fakeSkillAPI) Version(_ context.Context, n string, v int) (*agentsvc.SkillVersionView, error) {
	f.gotName, f.gotVer = n, v
	return &agentsvc.SkillVersionView{}, f.rec("version")
}
func (f *fakeSkillAPI) File(_ context.Context, n string, v int, _ string) (*agentsvc.SkillFileContent, error) {
	f.gotName, f.gotVer = n, v
	return &agentsvc.SkillFileContent{}, f.rec("file")
}
func (f *fakeSkillAPI) Download(_ context.Context, n string, v int) ([]byte, string, error) {
	f.gotName, f.gotVer = n, v
	return []byte("PK"), "demo-v1.zip", f.rec("download")
}
func (f *fakeSkillAPI) Import(_ context.Context, a uint64, in agentsvc.SkillImportInput) (*agentsvc.SkillImportView, error) {
	f.gotActor, f.gotImport = a, in
	return &agentsvc.SkillImportView{ID: "x"}, f.rec("import")
}
func (f *fakeSkillAPI) ImportFile(context.Context, uint64, string, string) (*agentsvc.SkillFileContent, error) {
	return &agentsvc.SkillFileContent{}, f.rec("importfile")
}
func (f *fakeSkillAPI) DiscardImport(context.Context, uint64, string) error { return f.rec("discard") }
func (f *fakeSkillAPI) ConfirmImport(context.Context, uint64, string) (*agentsvc.SkillItem, error) {
	return &agentsvc.SkillItem{}, f.rec("confirm")
}
func (f *fakeSkillAPI) SetEnabled(_ context.Context, a uint64, n string, on bool) (*agentsvc.SkillItem, error) {
	f.gotActor, f.gotName, f.gotOn = a, n, on
	return &agentsvc.SkillItem{}, f.rec("enabled")
}
func (f *fakeSkillAPI) SetActiveVersion(_ context.Context, _ uint64, n string, v int) (*agentsvc.SkillItem, error) {
	f.gotName, f.gotVer = n, v
	return &agentsvc.SkillItem{}, f.rec("active")
}
func (f *fakeSkillAPI) Rename(context.Context, uint64, string, string) (*agentsvc.SkillItem, error) {
	return &agentsvc.SkillItem{}, f.rec("rename")
}
func (f *fakeSkillAPI) DeleteCheck(context.Context, string) (*agentsvc.SkillDeleteCheck, error) {
	return &agentsvc.SkillDeleteCheck{}, f.rec("check")
}
func (f *fakeSkillAPI) DeleteVersion(_ context.Context, _ uint64, _ string, v int) error {
	f.gotVer = v
	return f.rec("delver")
}
func (f *fakeSkillAPI) DeleteSkill(context.Context, uint64, string) error { return f.rec("delete") }
func (f *fakeSkillAPI) Enabled(context.Context) ([]agentsvc.SkillBrief, error) {
	return []agentsvc.SkillBrief{{Name: "demo"}}, f.rec("user")
}

func newSkillRouter(api SkillAPI) *gin.Engine {
	h := NewSkillHandler(api)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(5)); c.Next() })
	v1 := r.Group("/api/v1")
	v1.GET("/agent/skills", h.UserList)
	a := v1.Group("/admin/agent/skills")
	a.GET("", h.List)
	a.POST("/imports", h.Import)
	a.GET("/imports/:id/files", h.ImportFile)
	a.DELETE("/imports/:id", h.DiscardImport)
	a.POST("/imports/:id/confirm", h.ConfirmImport)
	a.GET("/:name", h.Get)
	a.PUT("/:name", h.Rename)
	a.PUT("/:name/enabled", h.SetEnabled)
	a.PUT("/:name/active-version", h.SetActiveVersion)
	a.GET("/:name/delete-check", h.DeleteCheck)
	a.GET("/:name/versions/:v", h.Version)
	a.GET("/:name/versions/:v/files", h.File)
	a.GET("/:name/versions/:v/download", h.Download)
	a.DELETE("/:name/versions/:v", h.DeleteVersion)
	a.DELETE("/:name", h.Delete)
	return r
}

func TestSkillHandler_Endpoints(t *testing.T) {
	base := "/api/v1/admin/agent/skills"
	cases := []struct {
		name, method, path, body string
		status                   int
		call                     string
	}{
		{"用户目录", "GET", "/api/v1/agent/skills", "", 200, "user"},
		{"列表", "GET", base + "?q=a&status=enabled", "", 200, "list"},
		{"详情", "GET", base + "/demo", "", 200, "get"},
		{"版本详情", "GET", base + "/demo/versions/2", "", 200, "version"},
		{"读文件", "GET", base + "/demo/versions/2/files?path=a.md", "", 200, "file"},
		{"预览暂存文件", "GET", base + "/imports/x/files?path=a.md", "", 200, "importfile"},
		{"放弃暂存", "DELETE", base + "/imports/x", "", 200, "discard"},
		{"确认导入", "POST", base + "/imports/x/confirm", "", 200, "confirm"},
		{"启停", "PUT", base + "/demo/enabled", `{"enabled":false}`, 200, "enabled"},
		{"设为生效", "PUT", base + "/demo/active-version", `{"version":2}`, 200, "active"},
		{"改名", "PUT", base + "/demo", `{"title":"演示"}`, 200, "rename"},
		{"删除预检", "GET", base + "/demo/delete-check", "", 200, "check"},
		{"删除版本", "DELETE", base + "/demo/versions/3", "", 200, "delver"},
		{"删除技能", "DELETE", base + "/demo", "", 200, "delete"},
	}
	for _, tc := range cases {
		t.Run(tc.name+"：成功", func(t *testing.T) {
			api := &fakeSkillAPI{}
			status, resp := agentCall(t, newSkillRouter(api), tc.method, tc.path, tc.body)
			if status != tc.status || codeOf(resp) != 0 || len(api.calls) != 1 || api.calls[0] != tc.call {
				t.Errorf("status=%d resp=%v calls=%v", status, resp, api.calls)
			}
		})
		t.Run(tc.name+"：业务错误原样透传", func(t *testing.T) {
			api := &fakeSkillAPI{err: errcode.ErrSkillNotFound}
			status, resp := agentCall(t, newSkillRouter(api), tc.method, tc.path, tc.body)
			if status != http.StatusNotFound || codeOf(resp) != 61004 {
				t.Errorf("status=%d resp=%v", status, resp)
			}
		})
	}

	t.Run("参数校验：版本号、启停、生效版本、状态过滤", func(t *testing.T) {
		for _, p := range []struct{ method, path, body string }{
			{"GET", base + "/demo/versions/abc", ""},
			{"GET", base + "/demo/versions/0", ""},
			{"DELETE", base + "/demo/versions/-1", ""},
			{"PUT", base + "/demo/enabled", `{}`},
			{"PUT", base + "/demo/active-version", `{"version":0}`},
			{"PUT", base + "/demo", `{"title":""}`},
			{"GET", base + "?status=bad", ""},
			{"GET", base + "/demo/versions/1/files", ""},
		} {
			status, resp := agentCall(t, newSkillRouter(&fakeSkillAPI{}), p.method, p.path, p.body)
			if status != 400 || codeOf(resp) != 10001 {
				t.Errorf("%s %s: status=%d resp=%v", p.method, p.path, status, resp)
			}
		}
	})

	t.Run("下载返回 zip 与文件名", func(t *testing.T) {
		w := httptest.NewRecorder()
		newSkillRouter(&fakeSkillAPI{}).ServeHTTP(w, httptest.NewRequest("GET", base+"/demo/versions/1/download", nil))
		if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" || !strings.Contains(w.Header().Get("Content-Disposition"), "demo-v1.zip") {
			t.Errorf("%d %v", w.Code, w.Header())
		}
	})
}

func upload(t *testing.T, api *fakeSkillAPI, build func(*multipart.Writer)) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	build(mw)
	_ = mw.Close()
	req := httptest.NewRequest("POST", "/api/v1/admin/agent/skills/imports", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	newSkillRouter(api).ServeHTTP(w, req)
	var resp map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	return w.Code, resp
}

func TestSkillHandler_Import(t *testing.T) {
	t.Run("zip", func(t *testing.T) {
		api := &fakeSkillAPI{}
		status, resp := upload(t, api, func(mw *multipart.Writer) {
			w, _ := mw.CreateFormFile("file", "a.zip")
			_, _ = w.Write([]byte("PKzip"))
		})
		if status != 200 || codeOf(resp) != 0 || string(api.gotImport.Zip) != "PKzip" || api.gotActor != 5 {
			t.Errorf("%d %v %+v", status, resp, api.gotImport)
		}
	})
	t.Run("文件夹：files 与 paths 成对", func(t *testing.T) {
		api := &fakeSkillAPI{}
		status, _ := upload(t, api, func(mw *multipart.Writer) {
			_ = mw.WriteField("paths", "demo/SKILL.md")
			w, _ := mw.CreateFormFile("files", "SKILL.md")
			_, _ = w.Write([]byte("x"))
		})
		if status != 200 || len(api.gotImport.Files) != 1 || api.gotImport.Files[0].Path != "demo/SKILL.md" || string(api.gotImport.Files[0].Data) != "x" {
			t.Errorf("%d %+v", status, api.gotImport)
		}
	})
	t.Run("格式不对：缺内容、不成对、zip 与文件夹混传、不是 multipart", func(t *testing.T) {
		for name, build := range map[string]func(*multipart.Writer){
			"缺内容": func(mw *multipart.Writer) { _ = mw.WriteField("x", "y") },
			"不成对": func(mw *multipart.Writer) {
				w, _ := mw.CreateFormFile("files", "a")
				_, _ = w.Write([]byte("x"))
			},
			"混传": func(mw *multipart.Writer) {
				w, _ := mw.CreateFormFile("file", "a.zip")
				_, _ = w.Write([]byte("x"))
				_ = mw.WriteField("paths", "a")
				w2, _ := mw.CreateFormFile("files", "a")
				_, _ = w2.Write([]byte("x"))
			},
		} {
			status, resp := upload(t, &fakeSkillAPI{}, build)
			if status != 400 || codeOf(resp) != 10001 {
				t.Errorf("%s: %d %v", name, status, resp)
			}
		}
		status, resp := agentCall(t, newSkillRouter(&fakeSkillAPI{}), "POST", "/api/v1/admin/agent/skills/imports", `{}`)
		if status != 400 || codeOf(resp) != 10001 {
			t.Errorf("非 multipart: %d %v", status, resp)
		}
	})
	t.Run("zip 超过 50MB：413 + 61011", func(t *testing.T) {
		status, resp := upload(t, &fakeSkillAPI{}, func(mw *multipart.Writer) {
			w, _ := mw.CreateFormFile("file", "a.zip")
			_, _ = w.Write(make([]byte, 50<<20+1))
		})
		if status != 413 || codeOf(resp) != 61011 {
			t.Errorf("%d %v", status, resp)
		}
	})
}
