package handler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"video-canvas/internal/cache"
	. "video-canvas/internal/handler"
	"video-canvas/internal/middleware"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/repository"
	"video-canvas/internal/router"
	"video-canvas/internal/service"
	"video-canvas/internal/storage"
)

// 本文件测试 /api/v1/me/* 接口：真实 router（JWTAuth + RequireActive）+ 真实 UserService / MeService + 内存 fake 仓储与存储。

const meSecret = "me-handler-secret"

// meHRepo 同时实现 service.UserRepo（鉴权状态查询）与 service.MeRepo。
type meHRepo struct {
	mu     sync.Mutex
	users  map[uint64]*model.User
	ledger []model.MeLedgerItem
}

func (r *meHRepo) GetByID(_ context.Context, id uint64) (*model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *u
	return &cp, nil
}
func (r *meHRepo) GetByUsername(context.Context, string) (*model.User, error) {
	return nil, repository.ErrNotFound
}
func (r *meHRepo) Update(_ context.Context, id uint64, fields map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u, ok := r.users[id]
	if !ok {
		return repository.ErrNotFound
	}
	if v, ok := fields["nickname"].(string); ok {
		u.Nickname = v
	}
	if v, ok := fields["avatar_key"].(string); ok {
		u.AvatarKey = v
	}
	if v, ok := fields["avatar_storage_id"].(uint64); ok {
		u.AvatarStorageID = v
	}
	return nil
}
func (r *meHRepo) Count(context.Context) (int64, error)                      { return int64(len(r.users)), nil }
func (r *meHRepo) EmailExists(context.Context, string) (bool, error)         { return false, nil }
func (r *meHRepo) InsertLoginLog(context.Context, *model.UserLoginLog) error { return nil }
func (r *meHRepo) WithTx(context.Context, func(tx repository.UserTx) error) error {
	return errors.New("not supported")
}
func (r *meHRepo) ResetPassword(_ context.Context, id uint64, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.users[id]
	u.Password = hash
	u.TokenVersion++
	return nil
}
func (r *meHRepo) TaskStats(context.Context, uint64) (*model.UserTaskStats, error) {
	return &model.UserTaskStats{Total: 5, Success: 3, Failed: 1, Last7d: 2, SpentCredits: 40}, nil
}
func (r *meHRepo) ActivityDays(context.Context, uint64, string, time.Time, time.Time) ([]model.ActivityDay, error) {
	return []model.ActivityDay{{Date: "2026-10-08", Count: 2, Image: 2}}, nil
}
func (r *meHRepo) ListLedgerPage(_ context.Context, f repository.LedgerPageFilter) ([]model.MeLedgerItem, error) {
	if f.UserID != 1 {
		return nil, nil
	}
	if f.Offset >= len(r.ledger) {
		return nil, nil
	}
	return r.ledger[f.Offset:min(len(r.ledger), f.Offset+f.Limit)], nil
}
func (r *meHRepo) CountLedger(_ context.Context, userID uint64, _ []string) (int64, error) {
	if userID != 1 {
		return 0, nil
	}
	return int64(len(r.ledger)), nil
}

type meHCanvas struct{}

func (meHCanvas) CountByUser(context.Context, uint64) (int64, error) { return 3, nil }

// meHStore 是内存对象存储。
type meHStore struct {
	mu      sync.Mutex
	objects map[string][]byte
	deleted []string
}

func (s *meHStore) Put(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.objects[key] = b
	s.mu.Unlock()
	return nil
}
func (s *meHStore) Open(context.Context, string) (io.ReadCloser, error) {
	return nil, storage.ErrNotFound
}
func (s *meHStore) URL(_ context.Context, key string, _ time.Duration) (string, error) {
	return "https://cdn.test/" + key, nil
}
func (s *meHStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	s.deleted = append(s.deleted, key)
	delete(s.objects, key)
	s.mu.Unlock()
	return nil
}

type meHConns struct{ n int }

func (c *meHConns) DisconnectUser(uint64) int { c.n++; return 0 }

type meHEnv struct {
	engine *gin.Engine
	repo   *meHRepo
	store  *meHStore
	conns  *meHConns
	token  string
}

func newMeHEnv(t *testing.T) *meHEnv {
	t.Helper()
	repo := &meHRepo{users: map[uint64]*model.User{
		1: {BaseModel: model.BaseModel{ID: 1, CreatedAt: time.Now().AddDate(-1, 0, 0)}, Username: "alice", Role: model.RoleUser, Status: model.UserStatusActive, Password: mustBcrypt(t, "Old-Pass-2025")},
		2: {BaseModel: model.BaseModel{ID: 2, CreatedAt: time.Now()}, Username: "bob", Role: model.RoleUser, Status: model.UserStatusActive, Password: mustBcrypt(t, "Bob-Pass-2025")},
	}}
	store := &meHStore{objects: map[string][]byte{}}
	conns := &meHConns{}
	userSvc := service.NewUserService(service.UserDeps{Repo: repo, Cache: cache.NewUserCache(nil), JWTSecret: meSecret, JWTIssuer: "test", JWTExpireHours: 1})
	meSvc := service.NewMeService(service.MeDeps{
		Repo: repo, Canvases: meHCanvas{}, Stores: handlerRegistry{&storage.Handle{ID: 1, Provider: storage.ProviderS3, Storage: store}},
		Limiter: cache.NewMemoryPasswordFailLimiter(time.Now), Users: userSvc, Conns: conns, Tokens: userSvc,
	})
	stateLookup := func(ctx context.Context, id uint64) (*middleware.UserState, error) {
		u, err := userSvc.State(ctx, id)
		if err != nil || u == nil {
			return nil, err
		}
		return &middleware.UserState{Role: u.Role, Status: u.Status, TokenVersion: u.TokenVersion}, nil
	}
	tok, _, err := utils.GenerateToken(1, "alice", 0, meSecret, "test", 1)
	if err != nil {
		t.Fatal(err)
	}
	engine := router.New(gin.TestMode, meSecret, router.Handlers{Me: NewMeHandler(meSvc), UserState: stateLookup})
	return &meHEnv{engine: engine, repo: repo, store: store, conns: conns, token: tok}
}

func (e *meHEnv) send(req *http.Request, token string) uaResp {
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	e.engine.ServeHTTP(w, req)
	var parsed struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &parsed)
	return uaResp{Status: w.Code, Code: parsed.Code, Msg: parsed.Msg, Data: parsed.Data, Raw: w.Body.String()}
}

func (e *meHEnv) json(method, path string, body any, token string) uaResp {
	var rd io.Reader = http.NoBody
	switch b := body.(type) {
	case nil:
	case string:
		rd = strings.NewReader(b)
	default:
		raw, _ := json.Marshal(b)
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	return e.send(req, token)
}

func (e *meHEnv) upload(data []byte, token string) uaResp {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "avatar.png")
	_, _ = fw.Write(data)
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/avatar", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return e.send(req, token)
}

func smallPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 16, 16))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestMeHandler_GetAndPatch(t *testing.T) {
	e := newMeHEnv(t)
	r := e.json(http.MethodGet, "/api/v1/me", nil, e.token)
	r.want(t, 200, 0)
	var v map[string]any
	_ = json.Unmarshal(r.Data, &v)
	for _, k := range []string{"id", "username", "nickname", "email", "role", "avatar_url", "created_at", "email_verified_at"} {
		if _, ok := v[k]; !ok {
			t.Fatalf("MeView 缺字段 %s：%s", k, r.Raw)
		}
	}
	e.json(http.MethodGet, "/api/v1/me", nil, "").want(t, 401, errcode.ErrUnauthorized.Code)

	e.json(http.MethodPatch, "/api/v1/me", map[string]any{"nickname": " 小爱 "}, e.token).want(t, 200, 0)
	if e.repo.users[1].Nickname != "小爱" {
		t.Fatalf("%q", e.repo.users[1].Nickname)
	}
	e.json(http.MethodPatch, "/api/v1/me", map[string]any{"nickname": strings.Repeat("a", 33)}, e.token).want(t, 400, errcode.ErrInvalidParams.Code)
	e.json(http.MethodPatch, "/api/v1/me", map[string]any{"nickname": "a\nb"}, e.token).want(t, 400, errcode.ErrInvalidParams.Code)
	e.json(http.MethodPatch, "/api/v1/me", "oops", e.token).want(t, 400, errcode.ErrInvalidParams.Code)
}

func TestMeHandler_ChangePassword(t *testing.T) {
	t.Run("成功：旧 token 401，新 token 可用，WS 被断开", func(t *testing.T) {
		e := newMeHEnv(t)
		r := e.json(http.MethodPut, "/api/v1/me/password", map[string]any{"old_password": "Old-Pass-2025", "new_password": "Brand-New-77"}, e.token)
		r.want(t, 200, 0)
		var lv model.LoginView
		_ = json.Unmarshal(r.Data, &lv)
		if lv.Token == "" || lv.ExpireAt == 0 || lv.Role != model.RoleUser {
			t.Fatalf("%s", r.Raw)
		}
		e.json(http.MethodGet, "/api/v1/me", nil, e.token).want(t, 401, errcode.ErrUnauthorized.Code)
		e.json(http.MethodGet, "/api/v1/me", nil, lv.Token).want(t, 200, 0)
		if e.conns.n != 1 {
			t.Fatalf("应断开 WS：%d", e.conns.n)
		}
	})
	t.Run("参数与业务错误", func(t *testing.T) {
		e := newMeHEnv(t)
		e.json(http.MethodPut, "/api/v1/me/password", map[string]any{"old_password": "Old-Pass-2025"}, e.token).want(t, 400, errcode.ErrInvalidParams.Code)
		e.json(http.MethodPut, "/api/v1/me/password", map[string]any{"old_password": "Old-Pass-2025", "new_password": "1234567"}, e.token).want(t, 400, errcode.ErrInvalidParams.Code)
		e.json(http.MethodPut, "/api/v1/me/password", map[string]any{"old_password": "Old-Pass-2025", "new_password": "12345678"}, e.token).want(t, 400, errcode.ErrPasswordWeak.Code)
		e.json(http.MethodPut, "/api/v1/me/password", map[string]any{"old_password": "Old-Pass-2025", "new_password": "Old-Pass-2025"}, e.token).want(t, 400, errcode.ErrPasswordSame.Code)
		for range 5 {
			e.json(http.MethodPut, "/api/v1/me/password", map[string]any{"old_password": "wrong-pass", "new_password": "Brand-New-77"}, e.token).want(t, 400, errcode.ErrOldPasswordWrong.Code)
		}
		e.json(http.MethodPut, "/api/v1/me/password", map[string]any{"old_password": "Old-Pass-2025", "new_password": "Brand-New-77"}, e.token).want(t, 429, errcode.ErrPasswordTooFrequent.Code)
	})
}

func TestMeHandler_Avatar(t *testing.T) {
	e := newMeHEnv(t)
	r := e.upload(smallPNG(t), e.token)
	r.want(t, 200, 0)
	var v model.MeView
	_ = json.Unmarshal(r.Data, &v)
	if !strings.HasPrefix(v.AvatarURL, "/files/avatars/1/") {
		t.Fatalf("%s", r.Raw)
	}
	first := e.repo.users[1].AvatarKey

	// 替换：旧 key 被删
	e.upload(smallPNG(t), e.token).want(t, 200, 0)
	if len(e.store.deleted) != 1 || e.store.deleted[0] != first {
		t.Fatalf("应删除旧头像：%v", e.store.deleted)
	}

	e.upload([]byte("just text, not a png"), e.token).want(t, 400, errcode.ErrAvatarFormat.Code)
	e.upload(append(smallPNG(t), make([]byte, service.AvatarMaxBytes)...), e.token).want(t, 400, errcode.ErrAvatarTooLarge.Code)

	// 不是 multipart：10001
	e.json(http.MethodPost, "/api/v1/me/avatar", map[string]any{}, e.token).want(t, 400, errcode.ErrInvalidParams.Code)

	r = e.json(http.MethodDelete, "/api/v1/me/avatar", nil, e.token)
	r.want(t, 200, 0)
	_ = json.Unmarshal(r.Data, &v)
	if v.AvatarURL != "" || e.repo.users[1].AvatarKey != "" {
		t.Fatalf("%s", r.Raw)
	}
}

func TestMeHandler_StatsActivityLedger(t *testing.T) {
	e := newMeHEnv(t)
	r := e.json(http.MethodGet, "/api/v1/me/stats", nil, e.token)
	r.want(t, 200, 0)
	var st map[string]any
	_ = json.Unmarshal(r.Data, &st)
	for _, k := range []string{"total", "success", "failed", "last7d", "spent_credits", "canvas_count"} {
		if _, ok := st[k]; !ok {
			t.Fatalf("stats 缺字段 %s：%s", k, r.Raw)
		}
	}

	r = e.json(http.MethodGet, "/api/v1/me/activity?tz=Asia/Shanghai", nil, e.token)
	r.want(t, 200, 0)
	var act model.MeActivityView
	_ = json.Unmarshal(r.Data, &act)
	if act.TZ != "Asia/Shanghai" || act.Total != 2 || len(act.Days) != 1 {
		t.Fatalf("%s", r.Raw)
	}
	e.json(http.MethodGet, "/api/v1/me/activity?tz=Bad/Zone", nil, e.token).want(t, 200, 0)
	e.json(http.MethodGet, "/api/v1/me/activity?year=1999", nil, e.token).want(t, 400, errcode.ErrInvalidParams.Code)
	e.json(http.MethodGet, "/api/v1/me/activity?year=abc", nil, e.token).want(t, 400, errcode.ErrInvalidParams.Code)

	for i := range 25 {
		e.repo.ledger = append(e.repo.ledger, model.MeLedgerItem{ID: uint64(100 - i), Type: model.LedgerSettle, Amount: 1})
	}
	r = e.json(http.MethodGet, "/api/v1/me/credits/ledger?type=all&page=2&page_size=20", nil, e.token)
	r.want(t, 200, 0)
	var page model.MeLedgerPage
	_ = json.Unmarshal(r.Data, &page)
	if page.Total != 25 || page.Page != 2 || page.PageSize != 20 || len(page.Items) != 5 || page.Items[0].Amount != -1 {
		t.Fatalf("%s", r.Raw)
	}
	if strings.Contains(r.Raw, "operator_id") {
		t.Fatalf("不应返回 operator_id：%s", r.Raw)
	}
	r = e.json(http.MethodGet, "/api/v1/me/credits/ledger?page=9", nil, e.token)
	r.want(t, 200, 0)
	if !strings.Contains(r.Raw, `"items":[]`) || !strings.Contains(r.Raw, `"total":25`) {
		t.Fatalf("超范围应返回空 items + 真实 total：%s", r.Raw)
	}
	e.json(http.MethodGet, "/api/v1/me/credits/ledger?page_size=30", nil, e.token).want(t, 400, errcode.ErrInvalidParams.Code)
	e.json(http.MethodGet, "/api/v1/me/credits/ledger?page=0", nil, e.token).want(t, 400, errcode.ErrInvalidParams.Code)
	e.json(http.MethodGet, "/api/v1/me/credits/ledger?type=bogus", nil, e.token).want(t, 400, errcode.ErrInvalidParams.Code)
}
