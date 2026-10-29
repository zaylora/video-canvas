package handler_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"

	"video-canvas/internal/canvasdoc"
	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/service"
)

// transferHandlerHash 是测试素材的内容哈希（64 位小写十六进制）。
var transferHandlerHash = strings.Repeat("ab", 32)

// fakeTransferHandlerAssets 是导出导入共用的素材服务：素材 101 属于用户 1，复制总能成功。
type fakeTransferHandlerAssets struct{}

func (fakeTransferHandlerAssets) EnsureSha256(_ context.Context, userID, assetID uint64) (string, *model.Asset, error) {
	if userID != 1 || assetID != 101 {
		return "", nil, service.ErrAssetUnreadable
	}
	return transferHandlerHash, &model.Asset{ID: 101, UserID: 1, Kind: "image", MimeType: "image/png", ByteSize: 10, FileName: "a.png"}, nil
}

func (fakeTransferHandlerAssets) CopyBySha256(_ context.Context, userID uint64, sha string, byteSize int64) (*model.Asset, error) {
	return &model.Asset{UserID: userID, Kind: "image", StorageKey: "u1/copy", ByteSize: byteSize, Sha256: sha}, nil
}

func (fakeTransferHandlerAssets) DiscardCopies(context.Context, []*model.Asset) {}

func (fakeTransferHandlerAssets) FileURL(key string) string { return "/files/" + key }

// fakeTransferHandlerTx 是内存事务：只关心画布写入。
type fakeTransferHandlerTx struct{ canvases []*model.CanvasProject }

func (f *fakeTransferHandlerTx) WithTx(_ context.Context, fn func(tx service.TransferTx) error) error {
	return fn(f)
}

func (f *fakeTransferHandlerTx) CreateAsset(_ context.Context, a *model.Asset) error {
	a.ID = 500
	return nil
}

func (f *fakeTransferHandlerTx) CreateCanvas(_ context.Context, p *model.CanvasProject) error {
	p.ID = 700
	f.canvases = append(f.canvases, p)
	return nil
}

// newTransferRouter 装好导出导入路由：当前用户是 1，画布 1 里有一个图片节点（素材 101）。
func newTransferRouter() (*gin.Engine, *fakeTransferHandlerTx) {
	repo := &fakeCanvasRepo{items: map[uint64]*model.CanvasProject{
		1: {
			BaseModel: model.BaseModel{ID: 1}, UserID: 1, Title: "我的画布", Revision: 2,
			PayloadJSON: datatypes.JSON(`{"nodes":[{"id":"n1","type":"canvas","position":{"x":0,"y":0},` +
				`"data":{"kind":"image","assetId":"101","src":"/files/u1/a.png","status":"success"}}],"edges":[]}`),
		},
	}}
	tx := &fakeTransferHandlerTx{}
	svc := service.NewCanvasTransferService(service.CanvasTransferDeps{
		Canvases: repo, Assets: fakeTransferHandlerAssets{},
		Tx: tx, AppVersion: "v1", Limits: canvasdoc.DefaultLimits(),
		Now: func() time.Time { return time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC) },
	})
	h := NewCanvasTransferHandler(svc)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set(middleware.CtxUserIDKey, uint(1)); c.Next() })
	g := r.Group("/api/v1/canvas")
	g.GET("/:id/export", h.Export)
	g.POST("/import", h.Import)
	return r, tx
}

// transferDocument 是一份合法的导出文档：一个图片节点引用素材 1。
func transferDocument(t *testing.T) json.RawMessage {
	t.Helper()
	doc, _, err := canvasdoc.Build(canvasdoc.ExportInput{
		Title:   "雨夜短片",
		Payload: []byte(`{"nodes":[{"id":"n1","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"image","assetId":"101","src":"/files/u1/a.png"}}],"edges":[]}`),
		Assets: map[string]canvasdoc.AssetEntry{"101": {
			Hash: transferHandlerHash, Info: canvasdoc.AssetInfo{Kind: "image", MimeType: "image/png", ByteSize: 10, FileName: "a.png"},
		}},
		AppVersion: "v1", Now: time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// 导出成功：返回文档（格式标识固定）和报告，文档里素材换成了内容哈希。
func TestCanvasTransferHandler_ExportSuccess(t *testing.T) {
	r, _ := newTransferRouter()
	status, resp := canvasCall(t, r, http.MethodGet, "/api/v1/canvas/"+idcodec.Encode(1)+"/export", "")
	if status != http.StatusOK || respCode(resp) != 0 {
		t.Fatalf("导出应成功：%d %v", status, resp)
	}
	data := resp["data"].(map[string]any)
	doc := data["document"].(map[string]any)
	if doc["format"] != "video-canvas/canvas" {
		t.Errorf("format 不对：%v", doc["format"])
	}
	report, ok := data["report"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 report：%v", data)
	}
	if skipped, ok := report["skipped_assets"]; !ok || skipped != float64(0) {
		t.Errorf("skipped_assets 应为 0：%v", report)
	}
	if _, ok := doc["assets"].(map[string]any)[transferHandlerHash]; !ok {
		t.Errorf("文档素材表应以内容哈希为键：%v", doc["assets"])
	}
}

// 导出不存在的画布和别人的画布一样：404 + 30001；ID 格式不对仍是 400。
func TestCanvasTransferHandler_ExportNotFound(t *testing.T) {
	r, _ := newTransferRouter()
	status, resp := canvasCall(t, r, http.MethodGet, "/api/v1/canvas/"+idcodec.Encode(99)+"/export", "")
	if status != http.StatusNotFound || respCode(resp) != errcode.ErrCanvasNotFound.Code {
		t.Errorf("不存在的画布应 404 + 30001：%d %v", status, resp)
	}
	status, resp = canvasCall(t, r, http.MethodGet, "/api/v1/canvas/abc/export", "")
	if status != http.StatusBadRequest || respCode(resp) != errcode.ErrInvalidParams.Code {
		t.Errorf("非法 ID 应 400 + 10001：%d %v", status, resp)
	}
}

// 导入成功：返回新画布（十六进制 ID、revision 1）和报告，画布真的写进了事务。
func TestCanvasTransferHandler_ImportSuccess(t *testing.T) {
	r, tx := newTransferRouter()
	body := `{"document":` + string(transferDocument(t)) + `}`
	status, resp := canvasCall(t, r, http.MethodPost, "/api/v1/canvas/import", body)
	if status != http.StatusOK || respCode(resp) != 0 {
		t.Fatalf("导入应成功：%d %v", status, resp)
	}
	data := resp["data"].(map[string]any)
	canvas := data["canvas"].(map[string]any)
	if id, _ := canvas["id"].(string); id != idcodec.Encode(700) {
		t.Errorf("画布 ID 应是十六进制串：%v", canvas["id"])
	}
	if canvas["title"] != "雨夜短片（导入）" || canvas["revision"] != float64(1) {
		t.Errorf("新画布标题或版本不对：%v", canvas)
	}
	assets := data["report"].(map[string]any)["assets"].(map[string]any)
	if assets["total"] != float64(1) || assets["copied"] != float64(1) {
		t.Errorf("素材汇总不对：%v", assets)
	}
	if len(tx.canvases) != 1 {
		t.Errorf("应写入一张新画布：%d", len(tx.canvases))
	}
}

// 缺 document 或请求体不是 JSON 是普通参数错误：400 + 10001；document 显式为 null 则当作不合法的文件：400 + 30005。
func TestCanvasTransferHandler_ImportMissingDocument(t *testing.T) {
	r, _ := newTransferRouter()
	for _, body := range []string{`{}`, `not json`} {
		status, resp := canvasCall(t, r, http.MethodPost, "/api/v1/canvas/import", body)
		if status != http.StatusBadRequest || respCode(resp) != errcode.ErrInvalidParams.Code {
			t.Errorf("body %q 应 400 + 10001：%d %v", body, status, resp)
		}
	}
}

func TestCanvasTransferHandler_ImportNullDocument(t *testing.T) {
	r, _ := newTransferRouter()
	status, resp := canvasCall(t, r, http.MethodPost, "/api/v1/canvas/import", `{"document":null}`)
	if status != http.StatusBadRequest || respCode(resp) != errcode.ErrCanvasImportInvalid.Code {
		t.Errorf("document 为 null 应 400 + 30005：%d %v", status, resp)
	}
}

// format 不是本系统的画布文件：400 + 30005。
func TestCanvasTransferHandler_ImportWrongFormat(t *testing.T) {
	r, _ := newTransferRouter()
	status, resp := canvasCall(t, r, http.MethodPost, "/api/v1/canvas/import", `{"document":{"format":"other/thing","version":1}}`)
	if status != http.StatusBadRequest || respCode(resp) != errcode.ErrCanvasImportInvalid.Code {
		t.Errorf("format 错误应 400 + 30005：%d %v", status, resp)
	}
}

// 请求体超过文件上限加 1 MB 时在读取阶段被截断：413 + 30007，不是 400 / 500。
func TestCanvasTransferHandler_ImportBodyTooLarge(t *testing.T) {
	r, tx := newTransferRouter()
	// 10 MB 上限 + 1 MB 余量 = 11 MB，用一个超长字符串字段撑到略大于它，节省内存只造一份
	pad := strings.Repeat(" ", int(canvasdoc.DefaultLimits().MaxBytes)+(1<<20)+1024)
	body := `{"document":{"format":"video-canvas/canvas","pad":"` + pad + `"}}`
	status, resp := canvasCall(t, r, http.MethodPost, "/api/v1/canvas/import", body)
	if status != http.StatusRequestEntityTooLarge || respCode(resp) != errcode.ErrCanvasImportTooLarge.Code {
		t.Errorf("超大请求体应 413 + 30007：%d %v", status, resp)
	}
	if len(tx.canvases) != 0 {
		t.Errorf("超限请求不应写入画布")
	}
}

// 文档本身超过文件上限（但请求体仍在余量内）由服务层拒绝：同样 413 + 30007。
func TestCanvasTransferHandler_ImportDocumentTooLarge(t *testing.T) {
	r, _ := newTransferRouter()
	pad := strings.Repeat(" ", int(canvasdoc.DefaultLimits().MaxBytes)+1024)
	body := `{"document":{"format":"video-canvas/canvas","pad":"` + pad + `"}}`
	status, resp := canvasCall(t, r, http.MethodPost, "/api/v1/canvas/import", body)
	if status != http.StatusRequestEntityTooLarge || respCode(resp) != errcode.ErrCanvasImportTooLarge.Code {
		t.Errorf("超大文档应 413 + 30007：%d %v", status, resp)
	}
}
