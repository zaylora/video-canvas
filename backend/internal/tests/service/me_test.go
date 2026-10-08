package service_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"

	"video-canvas/internal/cache"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/repository"
	. "video-canvas/internal/service"
	"video-canvas/internal/storage"
)

// 本文件测试个人中心 MeService：资料、改密码、统计、热力图、积分流水。头像见 me_avatar_test.go。

const meJWTSecret = "me-service-secret"

// fakeMeRepo 是内存版 MeRepo。
type fakeMeRepo struct {
	mu      sync.Mutex
	users   map[uint64]*model.User
	stats   *model.UserTaskStats
	days    []model.ActivityDay
	ledger  []model.MeLedgerItem
	total   int64
	errs    map[string]error
	updates []map[string]any

	// 最近一次查询的入参
	actUser       uint64
	actTZ         string
	actFrom       time.Time
	actTo         time.Time
	lastLedger    repository.LedgerPageFilter
	lastCountUser uint64
	lastCountType []string
}

func newFakeMeRepo() *fakeMeRepo {
	return &fakeMeRepo{users: map[uint64]*model.User{}, errs: map[string]error{}}
}

func (r *fakeMeRepo) GetByID(_ context.Context, id uint64) (*model.User, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.errs["GetByID"]; err != nil {
		return nil, err
	}
	u, ok := r.users[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	cp := *u
	return &cp, nil
}

func (r *fakeMeRepo) Update(_ context.Context, id uint64, fields map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.errs["Update"]; err != nil {
		return err
	}
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
	r.updates = append(r.updates, fields)
	return nil
}

func (r *fakeMeRepo) ResetPassword(_ context.Context, id uint64, hash string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.errs["ResetPassword"]; err != nil {
		return err
	}
	u, ok := r.users[id]
	if !ok {
		return repository.ErrNotFound
	}
	u.Password = hash
	u.TokenVersion++
	return nil
}

func (r *fakeMeRepo) TaskStats(context.Context, uint64) (*model.UserTaskStats, error) {
	if err := r.errs["TaskStats"]; err != nil {
		return nil, err
	}
	if r.stats == nil {
		return &model.UserTaskStats{}, nil
	}
	return r.stats, nil
}

func (r *fakeMeRepo) ActivityDays(_ context.Context, userID uint64, tz string, from, to time.Time) ([]model.ActivityDay, error) {
	if err := r.errs["ActivityDays"]; err != nil {
		return nil, err
	}
	r.actUser, r.actTZ, r.actFrom, r.actTo = userID, tz, from, to
	return r.days, nil
}

func (r *fakeMeRepo) ListLedgerPage(_ context.Context, f repository.LedgerPageFilter) ([]model.MeLedgerItem, error) {
	if err := r.errs["ListLedgerPage"]; err != nil {
		return nil, err
	}
	r.lastLedger = f
	return r.ledger, nil
}

func (r *fakeMeRepo) CountLedger(_ context.Context, userID uint64, types []string) (int64, error) {
	if err := r.errs["CountLedger"]; err != nil {
		return 0, err
	}
	r.lastCountUser, r.lastCountType = userID, types
	return r.total, nil
}

type fakeCanvasCounter struct {
	n    int64
	user uint64
}

func (c *fakeCanvasCounter) CountByUser(_ context.Context, userID uint64) (int64, error) {
	c.user = userID
	return c.n, nil
}

// meEnv 是一套 MeService 测试环境：用户 1 = alice（密码 meOldPassword），注册于 2025-03-12。
type meEnv struct {
	svc     *MeService
	repo    *fakeMeRepo
	canvas  *fakeCanvasCounter
	inval   *fakeInvalidator
	conns   *fakeDisconnector
	store   *fakeAssetStore
	reg     *fakeRegistry
	clock   *testClock
	limiter cache.PasswordFailLimiter
}

const (
	meUID         = 1
	meOldPassword = "Old-Pass-2025"
)

func newMeEnv(t *testing.T) *meEnv {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(meOldPassword), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	repo := newFakeMeRepo()
	verified := time.Date(2025, 3, 12, 9, 0, 0, 0, time.UTC)
	repo.users[meUID] = &model.User{
		BaseModel: model.BaseModel{ID: meUID, CreatedAt: time.Date(2025, 3, 12, 8, 0, 0, 0, time.UTC)},
		Username:  "alice", Nickname: "小爱", Email: "a@b.com", Role: model.RoleUser, Status: model.UserStatusActive,
		Password: string(hash), EmailVerifiedAt: &verified,
	}
	clock := &testClock{t: time.Date(2026, 10, 9, 3, 0, 0, 0, time.UTC)} // 上海时间 2026-10-09 11:00
	store := newFakeAssetStore()
	reg := newFakeRegistry(1, objectHandle(1, store, time.Hour))
	e := &meEnv{repo: repo, canvas: &fakeCanvasCounter{}, inval: &fakeInvalidator{}, conns: &fakeDisconnector{}, store: store, reg: reg, clock: clock}
	e.limiter = cache.NewMemoryPasswordFailLimiter(clock.now)
	users := NewUserService(UserDeps{JWTSecret: meJWTSecret, JWTIssuer: "test", JWTExpireHours: 1})
	e.svc = NewMeService(MeDeps{
		Repo: repo, Canvases: e.canvas, Stores: reg, Limiter: e.limiter,
		Users: e.inval, Conns: e.conns, Tokens: users, FileBaseURL: "", Now: clock.now,
	})
	return e
}

func strp(s string) *string { return &s }

// ---- GET /me ----

func TestMeService_Me(t *testing.T) {
	ctx := context.Background()
	t.Run("返回资料；没有头像时 avatar_url 为空串", func(t *testing.T) {
		e := newMeEnv(t)
		v, err := e.svc.Me(ctx, meUID)
		if err != nil {
			t.Fatal(err)
		}
		if v.ID != meUID || v.Username != "alice" || v.Nickname != "小爱" || v.Email != "a@b.com" || v.Role != model.RoleUser ||
			v.AvatarURL != "" || v.EmailVerifiedAt == nil || v.CreatedAt.IsZero() {
			t.Fatalf("%+v", v)
		}
	})
	t.Run("有头像时 avatar_url = /files/<key>", func(t *testing.T) {
		e := newMeEnv(t)
		e.repo.users[meUID].AvatarKey = "avatars/1/abcdef0123456789.webp"
		v, _ := e.svc.Me(ctx, meUID)
		if v.AvatarURL != "/files/avatars/1/abcdef0123456789.webp" {
			t.Fatalf("avatar_url = %q", v.AvatarURL)
		}
	})
	t.Run("用户不存在：20001", func(t *testing.T) {
		e := newMeEnv(t)
		_, err := e.svc.Me(ctx, 99)
		wantBizErr(t, err, errcode.ErrUserNotFound)
	})
}

// ---- PATCH /me ----

func TestMeService_UpdateProfile(t *testing.T) {
	ctx := context.Background()
	t.Run("成功：去掉首尾空白后保存，清缓存", func(t *testing.T) {
		e := newMeEnv(t)
		v, err := e.svc.UpdateProfile(ctx, meUID, &model.UpdateMeReq{Nickname: strp("  新昵称  ")})
		if err != nil {
			t.Fatal(err)
		}
		if v.Nickname != "新昵称" || e.repo.users[meUID].Nickname != "新昵称" {
			t.Fatalf("%+v", v)
		}
		if len(e.inval.ids) != 1 || e.inval.ids[0] != meUID {
			t.Fatalf("应清缓存：%v", e.inval.ids)
		}
	})
	t.Run("空串可以（回落显示 username）", func(t *testing.T) {
		e := newMeEnv(t)
		v, err := e.svc.UpdateProfile(ctx, meUID, &model.UpdateMeReq{Nickname: strp("")})
		if err != nil || v.Nickname != "" {
			t.Fatalf("%v %+v", err, v)
		}
	})
	t.Run("恰好 32 个字符可以", func(t *testing.T) {
		e := newMeEnv(t)
		if _, err := e.svc.UpdateProfile(ctx, meUID, &model.UpdateMeReq{Nickname: strp(strings.Repeat("字", 32))}); err != nil {
			t.Fatal(err)
		}
	})
	bad := map[string]*string{
		"33 个字符":  strp(strings.Repeat("字", 33)),
		"含换行":     strp("a\nb"),
		"含制表符":    strp("a\tb"),
		"含其他控制字符": strp("a\x07b"),
		"缺少字段":    nil,
	}
	for name, nick := range bad {
		t.Run("拒绝："+name, func(t *testing.T) {
			e := newMeEnv(t)
			_, err := e.svc.UpdateProfile(ctx, meUID, &model.UpdateMeReq{Nickname: nick})
			wantBizErr(t, err, errcode.ErrInvalidParams)
			if len(e.repo.updates) != 0 || len(e.inval.ids) != 0 {
				t.Fatal("失败不应写库 / 清缓存")
			}
		})
	}
}

// ---- PUT /me/password ----

func TestMeService_ChangePassword(t *testing.T) {
	ctx := context.Background()
	req := func(old, nw string) *model.ChangePasswordReq {
		return &model.ChangePasswordReq{OldPassword: old, NewPassword: nw}
	}

	t.Run("成功：token_version+1、哈希更新、清缓存、断开 WS、返回带新版本的 token", func(t *testing.T) {
		e := newMeEnv(t)
		view, err := e.svc.ChangePassword(ctx, meUID, req(meOldPassword, "Brand-New-77"))
		if err != nil {
			t.Fatal(err)
		}
		u := e.repo.users[meUID]
		if u.TokenVersion != 1 || bcrypt.CompareHashAndPassword([]byte(u.Password), []byte("Brand-New-77")) != nil {
			t.Fatalf("库里没改对：%+v", u)
		}
		if len(e.inval.ids) != 1 || len(e.conns.ids) != 1 || e.conns.ids[0] != meUID {
			t.Fatalf("应清缓存并断开 WS：%v %v", e.inval.ids, e.conns.ids)
		}
		claims, err := utils.ParseToken(view.Token, meJWTSecret)
		if err != nil || claims.TokenVersion != 1 || claims.UserID != meUID || view.Role != model.RoleUser || view.ExpireAt == 0 {
			t.Fatalf("新 token 不对：%v %+v %+v", err, claims, view)
		}
	})

	t.Run("当前密码错误：55001 次数递减，第 5 次提示锁定，第 6 次 55004（即使密码正确）", func(t *testing.T) {
		e := newMeEnv(t)
		for i, left := range []int{4, 3, 2, 1} {
			_, err := e.svc.ChangePassword(ctx, meUID, req("wrong-pass", "Brand-New-77"))
			ec := bizErrOf(t, err, errcode.ErrOldPasswordWrong)
			want := "当前密码错误，还可尝试 " + itoaT(left) + " 次"
			if ec.Msg != want {
				t.Fatalf("第 %d 次 Msg = %q，期望 %q", i+1, ec.Msg, want)
			}
			if ec.HTTPStatus() != 400 {
				t.Fatalf("55001 应为 400：%d", ec.HTTPStatus())
			}
		}
		_, err := e.svc.ChangePassword(ctx, meUID, req("wrong-pass", "Brand-New-77"))
		ec := bizErrOf(t, err, errcode.ErrOldPasswordWrong)
		if !strings.Contains(ec.Msg, "15 分钟") {
			t.Fatalf("第 5 次应提示锁定 15 分钟：%q", ec.Msg)
		}
		_, err = e.svc.ChangePassword(ctx, meUID, req(meOldPassword, "Brand-New-77"))
		ec = bizErrOf(t, err, errcode.ErrPasswordTooFrequent)
		if ec.HTTPStatus() != 429 {
			t.Fatalf("55004 应为 429：%d", ec.HTTPStatus())
		}
		if e.repo.users[meUID].TokenVersion != 0 {
			t.Fatal("锁定期间不应改密码")
		}
		// 15 分钟后解锁
		e.clock.add(15*time.Minute + time.Second)
		if _, err := e.svc.ChangePassword(ctx, meUID, req(meOldPassword, "Brand-New-77")); err != nil {
			t.Fatalf("锁定过期后应能改密码：%v", err)
		}
	})

	t.Run("成功后清零失败次数", func(t *testing.T) {
		e := newMeEnv(t)
		_, _ = e.svc.ChangePassword(ctx, meUID, req("wrong-pass", "Brand-New-77"))
		_, _ = e.svc.ChangePassword(ctx, meUID, req("wrong-pass", "Brand-New-77"))
		if _, err := e.svc.ChangePassword(ctx, meUID, req(meOldPassword, "Brand-New-77")); err != nil {
			t.Fatal(err)
		}
		_, err := e.svc.ChangePassword(ctx, meUID, req("wrong-pass", "Another-New-88"))
		if ec := bizErrOf(t, err, errcode.ErrOldPasswordWrong); !strings.Contains(ec.Msg, "还可尝试 4 次") {
			t.Fatalf("次数应已清零：%q", ec.Msg)
		}
	})

	errCases := []struct {
		name string
		old  string
		nw   string
		want *errcode.Error
	}{
		{"新旧相同：55002", meOldPassword, meOldPassword, errcode.ErrPasswordSame},
		{"命中弱密码表：55003", meOldPassword, "12345678", errcode.ErrPasswordWeak},
		{"等于用户名：55003", meOldPassword, "alice", errcode.ErrInvalidParams}, // 5 字节先被长度拦住
		{"超过 72 字节：10001", meOldPassword, strings.Repeat("a", 73), errcode.ErrInvalidParams},
		{"不足 8 字节：10001", meOldPassword, "Ab3$xyz", errcode.ErrInvalidParams},
	}
	for _, c := range errCases {
		t.Run(c.name, func(t *testing.T) {
			e := newMeEnv(t)
			_, err := e.svc.ChangePassword(ctx, meUID, req(c.old, c.nw))
			wantBizErr(t, err, c.want)
			if e.repo.users[meUID].TokenVersion != 0 || len(e.inval.ids) != 0 || len(e.conns.ids) != 0 {
				t.Fatal("失败不应改密码 / 清缓存 / 断开连接")
			}
		})
	}

	t.Run("新密码不合规时不计入失败次数", func(t *testing.T) {
		e := newMeEnv(t)
		for range 6 {
			_, err := e.svc.ChangePassword(ctx, meUID, req("wrong-pass", "12345678"))
			wantBizErr(t, err, errcode.ErrPasswordWeak)
		}
		_, err := e.svc.ChangePassword(ctx, meUID, req("wrong-pass", "Brand-New-77"))
		if ec := bizErrOf(t, err, errcode.ErrOldPasswordWrong); !strings.Contains(ec.Msg, "还可尝试 4 次") {
			t.Fatalf("%q", ec.Msg)
		}
	})

	t.Run("等于用户名（长度合规）：55003", func(t *testing.T) {
		e := newMeEnv(t)
		e.repo.users[meUID].Username = "alice_2026"
		_, err := e.svc.ChangePassword(ctx, meUID, req(meOldPassword, "alice_2026"))
		wantBizErr(t, err, errcode.ErrPasswordWeak)
	})

	t.Run("写库失败原样返回内部错误", func(t *testing.T) {
		e := newMeEnv(t)
		e.repo.errs["ResetPassword"] = errBoom
		if _, err := e.svc.ChangePassword(ctx, meUID, req(meOldPassword, "Brand-New-77")); err == nil || codeOf(err) != -1 {
			t.Fatalf("应是内部错误：%v", err)
		}
	})
}

func itoaT(n int) string { return string(rune('0' + n)) }

// ---- GET /me/stats ----

func TestMeService_Stats(t *testing.T) {
	e := newMeEnv(t)
	e.repo.stats = &model.UserTaskStats{Total: 10, Success: 6, Failed: 3, Last7d: 2, SpentCredits: 120}
	e.canvas.n = 4
	v, err := e.svc.Stats(context.Background(), meUID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Total != 10 || v.Success != 6 || v.Failed != 3 || v.Last7d != 2 || v.SpentCredits != 120 || v.CanvasCount != 4 {
		t.Fatalf("%+v", v)
	}
	if e.canvas.user != meUID {
		t.Fatalf("画布数必须按本人统计：%d", e.canvas.user)
	}
}

// ---- GET /me/activity ----

func TestMeService_Activity(t *testing.T) {
	ctx := context.Background()
	sh, _ := time.LoadLocation("Asia/Shanghai")

	t.Run("默认最近一年：end=上海今天，start=end-364，UTC 边界正确，只查本人", func(t *testing.T) {
		e := newMeEnv(t)
		e.repo.days = []model.ActivityDay{{Date: "2026-10-08", Count: 3, Image: 2, Video: 1}, {Date: "2026-01-01", Count: 2, Text: 2}}
		v, err := e.svc.Activity(ctx, meUID, &model.MeActivityReq{TZ: "Asia/Shanghai"})
		if err != nil {
			t.Fatal(err)
		}
		if v.TZ != "Asia/Shanghai" || v.Start != "2025-10-10" || v.End != "2026-10-09" || v.Total != 5 || len(v.Days) != 2 {
			t.Fatalf("%+v", v)
		}
		if len(v.Years) != 2 || v.Years[0] != 2025 || v.Years[1] != 2026 {
			t.Fatalf("years = %v", v.Years)
		}
		if e.repo.actUser != meUID || e.repo.actTZ != "Asia/Shanghai" {
			t.Fatalf("查询条件不对：%d %s", e.repo.actUser, e.repo.actTZ)
		}
		if !e.repo.actFrom.Equal(time.Date(2025, 10, 10, 0, 0, 0, 0, sh)) || !e.repo.actTo.Equal(time.Date(2026, 10, 10, 0, 0, 0, 0, sh)) {
			t.Fatalf("UTC 边界不对：%v %v", e.repo.actFrom.UTC(), e.repo.actTo.UTC())
		}
	})

	t.Run("今天按用户时区算：UTC 17:00 在上海已是次日，在纽约还是当天", func(t *testing.T) {
		e := newMeEnv(t)
		e.clock.t = time.Date(2026, 10, 8, 17, 0, 0, 0, time.UTC)
		v, _ := e.svc.Activity(ctx, meUID, &model.MeActivityReq{TZ: "Asia/Shanghai"})
		if v.End != "2026-10-09" {
			t.Fatalf("上海 end = %s", v.End)
		}
		v, _ = e.svc.Activity(ctx, meUID, &model.MeActivityReq{TZ: "America/New_York"})
		if v.End != "2026-10-08" || v.TZ != "America/New_York" || e.repo.actTZ != "America/New_York" {
			t.Fatalf("纽约 %+v", v)
		}
	})

	for _, tz := range []string{"", "Mars/Base", "Local", "../etc/passwd"} {
		t.Run("非法时区回落上海："+tz, func(t *testing.T) {
			e := newMeEnv(t)
			v, err := e.svc.Activity(ctx, meUID, &model.MeActivityReq{TZ: tz})
			if err != nil {
				t.Fatal(err)
			}
			if v.TZ != "Asia/Shanghai" || e.repo.actTZ != "Asia/Shanghai" {
				t.Fatalf("应回落上海：%s / %s", v.TZ, e.repo.actTZ)
			}
		})
	}

	t.Run("指定往年：整年闭区间", func(t *testing.T) {
		e := newMeEnv(t)
		v, err := e.svc.Activity(ctx, meUID, &model.MeActivityReq{TZ: "Asia/Shanghai", Year: 2025})
		if err != nil {
			t.Fatal(err)
		}
		if v.Start != "2025-01-01" || v.End != "2025-12-31" {
			t.Fatalf("%+v", v)
		}
		if !e.repo.actTo.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, sh)) {
			t.Fatalf("to = %v", e.repo.actTo)
		}
	})

	t.Run("指定今年：end 截到今天", func(t *testing.T) {
		e := newMeEnv(t)
		v, _ := e.svc.Activity(ctx, meUID, &model.MeActivityReq{TZ: "Asia/Shanghai", Year: 2026})
		if v.Start != "2026-01-01" || v.End != "2026-10-09" {
			t.Fatalf("%+v", v)
		}
	})

	for _, y := range []int{2024, 2027, 1} {
		t.Run("year 越界：10001 "+time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006"), func(t *testing.T) {
			e := newMeEnv(t)
			_, err := e.svc.Activity(ctx, meUID, &model.MeActivityReq{TZ: "Asia/Shanghai", Year: y})
			wantBizErr(t, err, errcode.ErrInvalidParams)
		})
	}

	t.Run("没有数据时 days 是空数组而不是 null", func(t *testing.T) {
		e := newMeEnv(t)
		v, _ := e.svc.Activity(ctx, meUID, &model.MeActivityReq{})
		if v.Days == nil || v.Total != 0 {
			t.Fatalf("%+v", v)
		}
	})
}

// ---- GET /me/credits/ledger ----

func intpT(n int) *int { return &n }

func TestMeService_Ledger(t *testing.T) {
	ctx := context.Background()
	t.Run("默认第 1 页 20 条；金额带符号；只查本人", func(t *testing.T) {
		e := newMeEnv(t)
		e.repo.total = 3
		e.repo.ledger = []model.MeLedgerItem{
			{ID: 3, Type: model.LedgerAdminAdjust, Amount: -5, Note: "国庆活动补发"},
			{ID: 2, Type: model.LedgerSettle, Amount: 10, TaskID: model.TaskIDPtr(7)},
			{ID: 1, Type: model.LedgerFreeze, Amount: 10, TaskID: model.TaskIDPtr(7)},
		}
		v, err := e.svc.Ledger(ctx, meUID, &model.MeLedgerReq{})
		if err != nil {
			t.Fatal(err)
		}
		if v.Page != 1 || v.PageSize != 20 || v.Total != 3 || len(v.Items) != 3 {
			t.Fatalf("%+v", v)
		}
		if v.Items[0].Amount != -5 || v.Items[0].Note != "国庆活动补发" || v.Items[1].Amount != -10 || v.Items[2].Amount != -10 {
			t.Fatalf("金额 / 备注不对：%+v", v.Items)
		}
		f := e.repo.lastLedger
		if f.UserID != meUID || f.Offset != 0 || f.Limit != 20 || f.Types != nil || e.repo.lastCountUser != meUID {
			t.Fatalf("查询条件不对：%+v", f)
		}
	})
	t.Run("type=task / admin 复用后台筛选口径；页码换算 offset", func(t *testing.T) {
		e := newMeEnv(t)
		_, err := e.svc.Ledger(ctx, meUID, &model.MeLedgerReq{Type: "task", Page: intpT(3), PageSize: intpT(10)})
		if err != nil {
			t.Fatal(err)
		}
		f := e.repo.lastLedger
		if f.Offset != 20 || f.Limit != 10 || strings.Join(f.Types, ",") != "freeze,settle,refund" || strings.Join(e.repo.lastCountType, ",") != "freeze,settle,refund" {
			t.Fatalf("%+v %v", f, e.repo.lastCountType)
		}
		_, _ = e.svc.Ledger(ctx, meUID, &model.MeLedgerReq{Type: "admin", PageSize: intpT(50)})
		if strings.Join(e.repo.lastLedger.Types, ",") != "admin_adjust" || e.repo.lastLedger.Limit != 50 {
			t.Fatalf("%+v", e.repo.lastLedger)
		}
	})
	t.Run("page 超范围：空 items + 真实 total", func(t *testing.T) {
		e := newMeEnv(t)
		e.repo.total = 3
		v, err := e.svc.Ledger(ctx, meUID, &model.MeLedgerReq{Page: intpT(9)})
		if err != nil || v.Items == nil || len(v.Items) != 0 || v.Total != 3 || v.Page != 9 {
			t.Fatalf("%v %+v", err, v)
		}
	})
	for name, r := range map[string]*model.MeLedgerReq{
		"page=0":        {Page: intpT(0)},
		"page=-1":       {Page: intpT(-1)},
		"page_size=30":  {PageSize: intpT(30)},
		"page_size=0":   {PageSize: intpT(0)},
		"page_size=100": {PageSize: intpT(100)},
		"type 非法":       {Type: "bogus"},
	} {
		t.Run("参数错误："+name, func(t *testing.T) {
			e := newMeEnv(t)
			_, err := e.svc.Ledger(ctx, meUID, r)
			wantBizErr(t, err, errcode.ErrInvalidParams)
		})
	}
}

var _ = storage.ErrNotFound
