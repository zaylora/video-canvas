package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/idcodec"
	"video-canvas/internal/repository"
	"video-canvas/internal/service"
)

const (
	hCardUserID  = uint64(1)
	hCardAvatar  = uint64(1) // 用户 1 的图片
	hCardAudioID = uint64(2) // 用户 1 的音频
	hCardVideo   = uint64(3) // 用户 1 的视频
	hCardOther   = uint64(4) // 用户 2 的图片
)

// hCharacterCards 是 handler 测试用的内存仓储：只保存当前用户的卡，ID 从 7 开始。
type hCharacterCards struct {
	card *model.CharacterCard
	next uint64
}

func (h *hCharacterCards) List(_ context.Context, userID uint64, q string) ([]model.CharacterCard, error) {
	if h.card == nil || h.card.UserID != userID || h.card.DramaID != nil {
		return []model.CharacterCard{}, nil
	}
	return []model.CharacterCard{*h.card}, nil
}

func (h *hCharacterCards) ListByIDs(_ context.Context, userID uint64, ids []uint64) ([]model.CharacterCard, error) {
	for _, id := range ids {
		if h.card != nil && h.card.UserID == userID && h.card.ID == id {
			return []model.CharacterCard{*h.card}, nil
		}
	}
	return []model.CharacterCard{}, nil
}

func (h *hCharacterCards) GetByID(_ context.Context, userID, id uint64) (*model.CharacterCard, error) {
	if h.card == nil || h.card.UserID != userID || h.card.ID != id {
		return nil, repository.ErrNotFound
	}
	copied := *h.card
	return &copied, nil
}

func (h *hCharacterCards) Count(_ context.Context, _ uint64) (int64, error) {
	if h.card == nil {
		return 0, nil
	}
	return 1, nil
}

func (h *hCharacterCards) Create(_ context.Context, card *model.CharacterCard) error {
	h.next = 7
	card.ID = h.next
	h.card = card
	return nil
}

func (h *hCharacterCards) Update(_ context.Context, userID uint64, card *model.CharacterCard) error {
	if h.card == nil || h.card.UserID != userID {
		return repository.ErrNotFound
	}
	saved := *card
	h.card = &saved
	return nil
}

func (h *hCharacterCards) Delete(_ context.Context, userID, id uint64) error {
	if h.card == nil || h.card.UserID != userID || h.card.ID != id {
		return repository.ErrNotFound
	}
	h.card = nil
	return nil
}

// hCharacterAssets 是 handler 测试用的素材仓储。
type hCharacterAssets struct{}

func (hCharacterAssets) GetByID(_ context.Context, userID, id uint64) (*model.Asset, error) {
	owned := map[uint64]string{hCardAvatar: "image", hCardAudioID: "audio", hCardVideo: "video"}
	if kind, ok := owned[id]; ok && userID == hCardUserID {
		return &model.Asset{ID: id, UserID: userID, Kind: kind}, nil
	}
	if id == hCardOther && userID == 2 {
		return &model.Asset{ID: id, UserID: 2, Kind: "image"}, nil
	}
	return nil, repository.ErrNotFound
}

// hCharacterViews 生成稳定地址的素材视图。
type hCharacterViews struct{}

func (hCharacterViews) ViewOf(_ context.Context, a *model.Asset) (*model.AssetView, error) {
	return &model.AssetView{ID: a.ID, Kind: a.Kind, URL: "/files/test"}, nil
}

// newCharacterTestRouter 组装真实 service + 内存依赖，并用中间件模拟登录用户 1。
func newCharacterTestRouter() (*gin.Engine, *hCharacterCards) {
	gin.SetMode(gin.TestMode)
	cards := &hCharacterCards{}
	svc := service.NewCharacterCardService(service.CharacterCardDeps{Cards: cards, Assets: hCharacterAssets{}, Views: hCharacterViews{}})
	h := handler.NewCharacterCardHandler(svc)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.CtxUserIDKey, uint(hCardUserID))
		c.Next()
	})
	r.GET("/characters", h.List)
	r.GET("/characters/:id", h.Get)
	r.POST("/characters", h.Create)
	r.PATCH("/characters/:id", h.Update)
	r.DELETE("/characters/:id", h.Delete)
	return r, cards
}

// characterCall 发一次请求并解出统一响应体；204 要求正文为空。
func characterCall(t *testing.T, r *gin.Engine, method, path, body string) (int, map[string]any) {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code == http.StatusNoContent {
		if w.Body.Len() != 0 {
			t.Fatalf("204 响应不应有正文：%s", w.Body.String())
		}
		return w.Code, nil
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是 JSON：%s", w.Body.String())
	}
	return w.Code, resp
}

// responseCode 取统一响应里的业务码。
func responseCode(t *testing.T, resp map[string]any) int {
	t.Helper()
	code, ok := resp["code"].(float64)
	if !ok {
		t.Fatalf("响应缺少 code：%+v", resp)
	}
	return int(code)
}

// responseData 取统一响应里的 data。
func responseData(t *testing.T, resp map[string]any) map[string]any {
	t.Helper()
	data, ok := resp["data"].(map[string]any)
	if !ok {
		t.Fatalf("响应缺少 data：%+v", resp)
	}
	return data
}

func TestCharacterCardHandler_InvalidInputs(t *testing.T) {
	r, _ := newCharacterTestRouter()
	// 先建一张卡：PATCH 会先确认归属，没有卡时拿到的是 66004 而不是参数错误。
	if status, resp := characterCall(t, r, "POST", "/characters", `{"name":"甲","avatar_asset_id":1}`); status != http.StatusCreated {
		t.Fatalf("准备数据失败：%d %+v", status, resp)
	}
	tests := []struct {
		name         string
		method, path string
		body         string
		status, code int
	}{
		{"ids 格式错误返回参数错误", "GET", "/characters?ids=not-an-id", "", http.StatusBadRequest, 10001},
		{"ids 超过 100 个返回参数错误", "GET", "/characters?ids=" + repeatIDs(101), "", http.StatusBadRequest, 10001},
		{"路径 id 格式错误", "GET", "/characters/not-an-id", "", http.StatusBadRequest, 10001},
		{"请求体不是合法 JSON", "POST", "/characters", "{", http.StatusBadRequest, 10001},
		{"字段类型错误", "POST", "/characters", `{"name":"林晚","avatar_asset_id":"x"}`, http.StatusBadRequest, 10001},
		{"主图零 ID 返回素材不可用", "POST", "/characters", `{"name":"林晚","avatar_asset_id":0}`, http.StatusBadRequest, 66003},
		{"主图是视频返回素材不可用", "POST", "/characters", `{"name":"林晚","avatar_asset_id":3}`, http.StatusBadRequest, 66003},
		{"名称为空返回参数不合法", "POST", "/characters", `{"name":"   ","avatar_asset_id":1}`, http.StatusBadRequest, 66005},
		{"PATCH 声音格式错误", "PATCH", "/characters/" + idcodec.Encode(7), `{"voice_asset_id":"abc"}`, http.StatusBadRequest, 10001},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, resp := characterCall(t, r, tt.method, tt.path, tt.body)
			if status != tt.status || responseCode(t, resp) != tt.code {
				t.Fatalf("期望 %d/%d，实际 %d/%+v", tt.status, tt.code, status, resp)
			}
		})
	}
}

// repeatIDs 生成 n 个合法但互不相同的十六进制 ID。
func repeatIDs(n int) string {
	buf := make([]byte, 0, n*(idcodec.EncodedLen+1))
	for i := range n {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, idcodec.Encode(uint64(i+1))...)
	}
	return string(buf)
}

func TestCharacterCardHandler_Lifecycle(t *testing.T) {
	r, _ := newCharacterTestRouter()
	id := idcodec.Encode(7)

	t.Run("创建返回 201 和角色卡视图", func(t *testing.T) {
		status, resp := characterCall(t, r, "POST", "/characters", `{"name":"甲","avatar_asset_id":1,"voice_asset_id":2}`)
		if status != http.StatusCreated || responseCode(t, resp) != 0 {
			t.Fatalf("创建失败：%d %+v", status, resp)
		}
		data := responseData(t, resp)
		if data["id"] != id || data["name"] != "甲" || data["voice"] == nil || data["extras"] == nil {
			t.Fatalf("创建视图不对：%+v", data)
		}
	})

	t.Run("同名允许创建", func(t *testing.T) {
		status, resp := characterCall(t, r, "POST", "/characters", `{"name":"甲","avatar_asset_id":1}`)
		if status != http.StatusCreated || responseCode(t, resp) != 0 {
			t.Fatalf("同名创建：%d %+v", status, resp)
		}
	})

	t.Run("ids 批量读取，缺席的不报错", func(t *testing.T) {
		status, resp := characterCall(t, r, "GET", "/characters?ids="+id+","+idcodec.Encode(99), "")
		items, _ := responseData(t, resp)["items"].([]any)
		if status != http.StatusOK || len(items) != 1 {
			t.Fatalf("批量读取：%d %+v", status, resp)
		}
	})

	t.Run("空 ids 返回空列表", func(t *testing.T) {
		status, resp := characterCall(t, r, "GET", "/characters?ids=", "")
		items, ok := responseData(t, resp)["items"].([]any)
		if status != http.StatusOK || !ok || len(items) != 0 {
			t.Fatalf("空 ids：%d %+v", status, resp)
		}
	})

	t.Run("PATCH 清空声音与补充图", func(t *testing.T) {
		status, resp := characterCall(t, r, "PATCH", "/characters/"+id, `{"voice_asset_id":null,"extra_asset_ids":[]}`)
		data := responseData(t, resp)
		if status != http.StatusOK || data["voice"] != nil || len(data["extras"].([]any)) != 0 || data["name"] != "甲" {
			t.Fatalf("清空：%d %+v", status, resp)
		}
	})

	t.Run("DELETE 返回 204 且无正文", func(t *testing.T) {
		status, _ := characterCall(t, r, "DELETE", "/characters/"+id, "")
		if status != http.StatusNoContent {
			t.Fatalf("删除：%d", status)
		}
	})

	t.Run("删除后详情返回 404 和 66004", func(t *testing.T) {
		status, resp := characterCall(t, r, "GET", "/characters/"+id, "")
		if status != http.StatusNotFound || responseCode(t, resp) != 66004 {
			t.Fatalf("缺席：%d %+v", status, resp)
		}
	})

	t.Run("他人的卡按不存在处理", func(t *testing.T) {
		status, resp := characterCall(t, r, "GET", "/characters/"+idcodec.Encode(8), "")
		if status != http.StatusNotFound || responseCode(t, resp) != 66004 {
			t.Fatalf("他人数据：%d %+v", status, resp)
		}
	})
}

func TestCharacterCardHandler_DramaFields(t *testing.T) {
	r, cards := newCharacterTestRouter()
	status, resp := characterCall(t, r, "POST", "/characters", `{"name":"短剧角色","avatar_asset_id":null,"look_name":"童年","voice_prompt":"清亮少年音"}`)
	data := responseData(t, resp)
	if status != http.StatusCreated || data["avatar"] != nil || data["drama_id"] != nil || data["look_name"] != "童年" || data["voice_prompt"] != "清亮少年音" {
		t.Fatalf("新建无形象角色：%d %+v", status, resp)
	}
	id := idcodec.Encode(cards.card.ID)
	status, resp = characterCall(t, r, "PATCH", "/characters/"+id, `{"avatar_asset_id":1,"look_name":"成年","voice_prompt":"沉稳"}`)
	data = responseData(t, resp)
	if status != http.StatusOK || data["avatar"] == nil || data["look_name"] != "成年" || data["voice_prompt"] != "沉稳" {
		t.Fatalf("写回形象：%d %+v", status, resp)
	}
	status, resp = characterCall(t, r, "PATCH", "/characters/"+id, `{"avatar_asset_id":null}`)
	if status != http.StatusOK || responseData(t, resp)["avatar"] != nil {
		t.Fatalf("清空形象：%d %+v", status, resp)
	}
	for _, body := range []string{
		`{"look_name":"` + strings.Repeat("型", 65) + `"}`,
		`{"voice_prompt":"` + strings.Repeat("音", 501) + `"}`,
	} {
		status, resp = characterCall(t, r, "PATCH", "/characters/"+id, body)
		if status != http.StatusBadRequest || responseCode(t, resp) != 66005 {
			t.Fatalf("长度超限：%d %+v", status, resp)
		}
	}
	cards.card.DramaID = new(uint64(42))
	status, resp = characterCall(t, r, "GET", "/characters", "")
	if status != http.StatusOK || len(responseData(t, resp)["items"].([]any)) != 0 {
		t.Fatalf("普通列表不应包含短剧角色：%d %+v", status, resp)
	}
	status, resp = characterCall(t, r, "GET", "/characters?ids="+id, "")
	items := responseData(t, resp)["items"].([]any)
	if status != http.StatusOK || len(items) != 1 || items[0].(map[string]any)["drama_id"] != idcodec.Encode(42) {
		t.Fatalf("批量读取应包含短剧角色并编码短剧 ID：%d %+v", status, resp)
	}
}
