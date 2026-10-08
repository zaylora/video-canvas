package service

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// 个人中心（docs/design/个人中心/个人中心.md）：当前用户自助查看 / 修改资料、头像、密码，查看统计、热力图与积分流水。
// 所有方法都只按调用方传入的 userID（来自 JWT）读写，天然只能操作自己的数据。

// MeRepo 是个人中心的数据访问接口（真实实现是 repository.UserRepository）。
type MeRepo interface {
	// GetByID 按 id 查询用户，不存在返回 repository.ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.User, error)
	// Update 按 id 更新指定字段（昵称、头像），用户不存在返回 repository.ErrNotFound。
	Update(ctx context.Context, id uint64, fields map[string]any) error
	// ResetPassword 原子地改密码哈希并把 token_version +1，用户不存在返回 repository.ErrNotFound。
	ResetPassword(ctx context.Context, id uint64, hash string) error
	// TaskStats 汇总用户的正式任务统计（排除试跑）与已结算积分。
	TaskStats(ctx context.Context, userID uint64) (*model.UserTaskStats, error)
	// ActivityDays 按 tz 时区的日期统计用户在 [from, to) 内创建的正式任务数（排除试跑），只返回有任务的日子，按日期升序。
	ActivityDays(ctx context.Context, userID uint64, tz string, from, to time.Time) ([]model.ActivityDay, error)
	// ListLedgerPage 按 created_at DESC, id DESC 分页查询用户的积分流水（不含操作人）。
	ListLedgerPage(ctx context.Context, f repository.LedgerPageFilter) ([]model.MeLedgerItem, error)
	// CountLedger 统计用户的积分流水条数，types 为空不筛类型。
	CountLedger(ctx context.Context, userID uint64, types []string) (int64, error)
}

// CanvasCounter 统计用户未删除的画布数，由 repository.CanvasProjectRepository 实现。
type CanvasCounter interface {
	// CountByUser 统计用户未删除的画布数。
	CountByUser(ctx context.Context, userID uint64) (int64, error)
}

// PasswordFailLimiter 是改密码的失败计数器（真实实现是 cache.NewPasswordFailLimiter）。
type PasswordFailLimiter interface {
	// Locked 返回剩余锁定时长，没有锁定返回 0。
	Locked(ctx context.Context, userID uint64, limit int) (time.Duration, error)
	// Fail 记一次失败，返回窗口内的失败次数；达到 limit 次时锁定 window。
	Fail(ctx context.Context, userID uint64, limit int, window time.Duration) (int, error)
	// Reset 清零失败次数。
	Reset(ctx context.Context, userID uint64) error
}

// TokenIssuer 为用户签发与登录同构的 token，由 UserService 实现。
type TokenIssuer interface {
	// IssueToken 按用户当前的 token_version 签发 JWT。
	IssueToken(u *model.User) (*model.LoginView, error)
}

var (
	_ TokenIssuer   = (*UserService)(nil)
	_ CanvasCounter = (*repository.CanvasProjectRepository)(nil)
)

// MeDeps 是个人中心服务的依赖与配置。
type MeDeps struct {
	Repo     MeRepo
	Canvases CanvasCounter
	Stores   StoreRegistry       // 头像写入默认存储，删除按头像记录的 storage_id
	Limiter  PasswordFailLimiter // 改密码防爆破
	Users    UserInvalidator     // 资料、头像、密码变化后清用户缓存
	Conns    UserDisconnector    // 改密码后断开该用户全部 WebSocket
	Tokens   TokenIssuer         // 改密码后给当前设备续签

	// FileBaseURL 是 /files 稳定地址的站点前缀，与素材一致取 storage.local.base_url（为空时是相对路径）。
	FileBaseURL string
	// Now 返回当前时间，测试可注入；为 nil 时用 time.Now。
	Now func() time.Time
}

// MeService 提供个人中心的全部业务。
type MeService struct {
	MeDeps
}

// NewMeService 创建个人中心服务。
func NewMeService(deps MeDeps) *MeService {
	if deps.Now == nil {
		deps.Now = time.Now
	}
	return &MeService{MeDeps: deps}
}

// maxNicknameRunes 是昵称的最大字符数（按 rune 计，库里列宽 64）。
const maxNicknameRunes = 32

// loadUser 读取当前用户；不存在（token 有效但账号已被删除）返回 ErrUserNotFound。
func (s *MeService) loadUser(ctx context.Context, userID uint64) (*model.User, error) {
	u, err := s.Repo.GetByID(ctx, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrUserNotFound
	}
	return u, err
}

// meView 把用户转成个人中心视图：avatar_url 由 avatar_key 拼成 /files/<key>，没有头像时为空串。
func (s *MeService) meView(u *model.User) *model.MeView {
	url := ""
	if u.AvatarKey != "" {
		url = strings.TrimRight(s.FileBaseURL, "/") + "/files/" + u.AvatarKey
	}
	return &model.MeView{
		ID: u.ID, Username: u.Username, Nickname: u.Nickname, Email: u.Email, Role: u.Role,
		AvatarURL: url, CreatedAt: u.CreatedAt, EmailVerifiedAt: u.EmailVerifiedAt,
	}
}

// reloadView 重新读取用户并返回视图（写操作之后用，保证返回的是库里的最新值）。
func (s *MeService) reloadView(ctx context.Context, userID uint64) (*model.MeView, error) {
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.meView(u), nil
}

// Me 返回当前用户的资料（GET /me）。直接查库而不是读鉴权缓存：缓存里不含全部展示字段，且资料页要最新值。
func (s *MeService) Me(ctx context.Context, userID uint64) (*model.MeView, error) {
	return s.reloadView(ctx, userID)
}

// UpdateProfile 修改昵称（PATCH /me）：去掉首尾空白后 0..32 个字符，不能含控制字符 / 换行；空串表示清空（前端回落显示 username）。
// 资料修改是“后写覆盖”，不加版本号：只有本人能改，冲突概率极低。
func (s *MeService) UpdateProfile(ctx context.Context, userID uint64, req *model.UpdateMeReq) (*model.MeView, error) {
	// 1. 校验：没传字段与传了空串要区分开，前者是参数错误
	if req.Nickname == nil {
		return nil, errcode.ErrInvalidParams.WithMsg("缺少昵称")
	}
	nick := strings.TrimSpace(*req.Nickname)
	if utf8.RuneCountInString(nick) > maxNicknameRunes {
		return nil, errcode.ErrInvalidParams.WithMsg("昵称最多 32 个字符")
	}
	// 控制字符（含换行、制表符）会破坏各处单行展示，也可能被用来伪造界面文案，一律拒绝
	if strings.IndexFunc(nick, unicode.IsControl) >= 0 {
		return nil, errcode.ErrInvalidParams.WithMsg("昵称不能包含换行或控制字符")
	}

	// 2. 写库：用户不存在说明账号在请求期间被删除
	if err := s.Repo.Update(ctx, userID, map[string]any{"nickname": nick}); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrUserNotFound
		}
		return nil, err
	}

	// 3. 清缓存：User 整体在 Redis 里，不清的话其他地方最多 30 分钟后才看到新昵称
	s.Users.InvalidateUser(ctx, userID)
	return s.reloadView(ctx, userID)
}

// Stats 返回概览统计卡片的数据（GET /me/stats）：任务统计口径与后台 TaskStats 一致（排除试跑），另加未删除画布数。
func (s *MeService) Stats(ctx context.Context, userID uint64) (*model.MeStatsView, error) {
	// 1. 任务统计：直接复用后台用户详情的同一条查询，保证两边数字一致
	st, err := s.Repo.TaskStats(ctx, userID)
	if err != nil {
		return nil, err
	}
	// 2. 画布数：只数未软删除的
	n, err := s.Canvases.CountByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &model.MeStatsView{
		Total: st.Total, Success: st.Success, Failed: st.Failed, Last7d: st.Last7d, SpentCredits: st.SpentCredits, CanvasCount: n,
	}, nil
}

// 积分流水分页：只允许这几个每页条数，与前端的选择器一一对应。
const defaultLedgerPageSize = 20

var allowedLedgerPageSizes = map[int]bool{10: true, 20: true, 50: true}

// ledgerTypes 把流水类型筛选值映射成库里的类型集合，用户侧与后台 ListLedger 共用同一口径：
// admin 只看管理员调整；task 看 freeze / settle / refund（不含 initial）；all / 空不筛。
func ledgerTypes(filter string) ([]string, error) {
	switch filter {
	case "", "all":
		return nil, nil
	case "admin":
		return []string{model.LedgerAdminAdjust}, nil
	case "task":
		return []string{model.LedgerFreeze, model.LedgerSettle, model.LedgerRefund}, nil
	}
	return nil, errcode.ErrInvalidParams.WithMsg("类型筛选值不合法")
}

// Ledger 页码分页查询当前用户的积分流水（GET /me/credits/ledger），按 created_at DESC, id DESC 排序。
// page 超出范围时返回空 items 与真实 total，由前端跳到末页；不返回操作人。
func (s *MeService) Ledger(ctx context.Context, userID uint64, req *model.MeLedgerReq) (*model.MeLedgerPage, error) {
	// 1. 分页参数：没传取默认（第 1 页、每页 20）；传了非法值直接报错，而不是悄悄修正，
	//    因为页码要同步到 URL，悄悄修正会让地址栏和内容对不上
	page, size := 1, defaultLedgerPageSize
	if req.Page != nil {
		if page = *req.Page; page < 1 {
			return nil, errcode.ErrInvalidParams.WithMsg("页码必须大于等于 1")
		}
	}
	if req.PageSize != nil {
		if size = *req.PageSize; !allowedLedgerPageSizes[size] {
			return nil, errcode.ErrInvalidParams.WithMsg("每页条数只能是 10、20 或 50")
		}
	}

	// 2. 类型筛选：与后台同一口径
	types, err := ledgerTypes(req.Type)
	if err != nil {
		return nil, err
	}

	// 3. 总数 + 当前页；两条查询都带 user_id，只能查到自己的流水
	total, err := s.Repo.CountLedger(ctx, userID, types)
	if err != nil {
		return nil, err
	}
	rows, err := s.Repo.ListLedgerPage(ctx, repository.LedgerPageFilter{UserID: userID, Types: types, Offset: (page - 1) * size, Limit: size})
	if err != nil {
		return nil, err
	}

	// 4. 金额映射成带符号的“对可用积分的影响”，与后台流水同口径（只改响应，库里的值不变）；items 永远不是 null
	if rows == nil {
		rows = []model.MeLedgerItem{}
	}
	for i := range rows {
		rows[i].Amount = signedLedgerAmount(rows[i].Type, rows[i].Amount)
	}
	return &model.MeLedgerPage{Items: rows, Total: total, Page: page, PageSize: size}, nil
}
