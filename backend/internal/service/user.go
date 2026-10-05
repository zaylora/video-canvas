package service

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"video-canvas/internal/cache"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/repository"
)

// UserRepo 是用户服务的数据访问接口（真实实现是 repository.UserRepository）。
type UserRepo interface {
	// GetByID 按 id 查询用户，不存在返回 repository.ErrNotFound。
	GetByID(ctx context.Context, id uint64) (*model.User, error)
	// GetByUsername 按用户名查询用户，不存在返回 repository.ErrNotFound。
	GetByUsername(ctx context.Context, username string) (*model.User, error)
	// Update 按 id 更新指定字段，用户不存在返回 repository.ErrNotFound。
	Update(ctx context.Context, id uint64, fields map[string]any) error
	// Count 统计用户数。
	Count(ctx context.Context) (int64, error)
	// EmailExists 邮箱是否已被注册（按小写比较）。
	EmailExists(ctx context.Context, email string) (bool, error)
	// InsertLoginLog 写一条登录记录。
	InsertLoginLog(ctx context.Context, l *model.UserLoginLog) error
	// WithTx 在一个事务里执行 fn（注册用），fn 返回错误整体回滚。
	WithTx(ctx context.Context, fn func(tx repository.UserTx) error) error
}

// RegisterCodeStore 保存注册验证码并提供发码限频原语（真实实现是 cache.NewRegisterCodeStore）。
type RegisterCodeStore interface {
	// Save 保存（覆盖）邮箱的验证码并清零错误次数。
	Save(ctx context.Context, email, code string, ttl time.Duration) error
	// Check 原子校验：正确则一次性消费；错误累计到 maxAttempts 次后作废。
	Check(ctx context.Context, email, code string, maxAttempts int) (bool, error)
	// TryLock 占住 key 一段时间，ttl 内再次尝试返回 false。
	TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error)
	// Allow 固定窗口限频：window 内超过 limit 次返回 false。
	Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error)
}

// RegisterPolicy 提供注册相关的运行时设置（后台可改，库值优先，config 为缺省）。
type RegisterPolicy interface {
	// RegisterEnabled 注册开关是否打开。
	RegisterEnabled(ctx context.Context) (bool, error)
	// InitialCredits 新用户初始积分。
	InitialCredits(ctx context.Context) (int, error)
}

// RegisterMail 提供注册需要的邮件能力（真实实现是 SMTPService）。
type RegisterMail interface {
	// Enabled SMTP 是否已配置并启用。
	Enabled(ctx context.Context) (bool, error)
	// SendVerifyCode 向 to 发送注册验证码；失败返回 errcode.ErrSMTPSendFailed 等业务错误。
	SendVerifyCode(ctx context.Context, to, code string) error
}

// ClientMeta 是请求方的客户端信息，写进登录记录与用于发码限频。
type ClientMeta struct {
	IP        string
	UserAgent string
}

// UserDeps 是用户服务的依赖与配置。
type UserDeps struct {
	Repo   UserRepo
	Cache  *cache.UserCache // 用户状态缓存，Redis 未启用时是空操作
	Codes  RegisterCodeStore
	Policy RegisterPolicy
	Mail   RegisterMail

	JWTSecret      string
	JWTIssuer      string
	JWTExpireHours int
	// Debug 为 true（gin debug 模式）时把验证码打印到日志，方便本地联调；release 模式绝不输出。
	Debug bool
}

// UserService 负责登录、注册（含验证码）、鉴权状态查询。
type UserService struct {
	UserDeps
}

// NewUserService 创建用户服务。
func NewUserService(deps UserDeps) *UserService {
	return &UserService{UserDeps: deps}
}

// maxUserAgentLen 是登录记录里 User-Agent 的最大长度（库字段 255）。
const maxUserAgentLen = 255

// Login 用户名密码登录，成功返回 token、过期时间与角色。
// 用户不存在和密码错误统一返回同一个错误，避免暴露「用户名是否存在」；停用账号在密码正确后才提示，避免泄露账号状态。
func (s *UserService) Login(ctx context.Context, username, password string, meta ClientMeta) (*model.LoginView, error) {
	// 1. 按用户名查用户，不存在时记一条 badpw（user_id=0）并返回「用户名或密码错误」
	user, err := s.Repo.GetByUsername(ctx, username)
	if errors.Is(err, repository.ErrNotFound) {
		s.logLogin(ctx, 0, model.LoginKindLogin, model.LoginResultBadPw, meta)
		return nil, errcode.ErrInvalidCredential
	}
	if err != nil {
		return nil, err
	}

	// 2. 校验密码：数据库存的是 bcrypt 哈希，不能直接比较明文
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		s.logLogin(ctx, user.ID, model.LoginKindLogin, model.LoginResultBadPw, meta)
		return nil, errcode.ErrInvalidCredential
	}

	// 3. 账号已停用：记 blocked，返回 403（前端据此提示联系管理员）
	if user.Status == model.UserStatusDisabled {
		s.logLogin(ctx, user.ID, model.LoginKindLogin, model.LoginResultBlocked, meta)
		return nil, errcode.ErrAccountDisabled
	}

	// 4. 签发 JWT：载荷带用户 ID、用户名和当前 token_version（重置密码后旧 token 靠它失效）
	view, err := s.issueToken(user)
	if err != nil {
		return nil, err
	}

	// 5. 更新最近登录时间并写成功记录。这两步只是运营辅助信息，失败只记日志，不能因此让用户登不进去
	if err := s.Repo.Update(ctx, user.ID, map[string]any{"last_login_at": time.Now()}); err != nil {
		logger.Warn("更新最近登录时间失败", zap.Error(err), zap.Uint64("user_id", user.ID))
	}
	s.logLogin(ctx, user.ID, model.LoginKindLogin, model.LoginResultOK, meta)
	return view, nil
}

// issueToken 为用户签发 JWT 并组装登录响应。
func (s *UserService) issueToken(u *model.User) (*model.LoginView, error) {
	token, expireAt, err := utils.GenerateToken(uint(u.ID), u.Username, u.TokenVersion, s.JWTSecret, s.JWTIssuer, s.JWTExpireHours)
	if err != nil {
		return nil, err
	}
	return &model.LoginView{Token: token, ExpireAt: expireAt, Role: u.Role}, nil
}

// logLogin 写一条登录记录。写失败只记日志：记录是审计辅助，不影响登录结果。
func (s *UserService) logLogin(ctx context.Context, userID uint64, kind, result string, meta ClientMeta) {
	l := &model.UserLoginLog{UserID: userID, Kind: kind, Result: result, IP: meta.IP, UserAgent: truncate(meta.UserAgent, maxUserAgentLen)}
	if err := s.Repo.InsertLoginLog(ctx, l); err != nil {
		logger.Warn("写登录记录失败", zap.Error(err), zap.Uint64("user_id", userID), zap.String("result", result))
	}
}

// State 返回鉴权用的用户当前状态（角色、停用状态、token_version），优先读缓存；用户不存在返回 (nil, nil)。
// RequireActive 与 RequireAdmin / RequireSuperAdmin 共用它：封禁、改角色、重置密码后主动 InvalidateUser，立即生效。
func (s *UserService) State(ctx context.Context, id uint64) (*model.User, error) {
	// 1. 先查缓存；未启用 Redis、未命中或读取出错都继续查库（缓存只是加速，不能影响鉴权）
	if u, err := s.Cache.Get(ctx, id); err == nil && u != nil {
		return u, nil
	}
	// 2. 查库：不存在返回 (nil, nil)，由中间件按未登录处理；库故障返回 error，由中间件按 500 处理，而不是误放行
	u, err := s.Repo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// 3. 回写缓存；写失败只影响下次命中率，所以忽略
	_ = s.Cache.Set(ctx, u)
	return u, nil
}

// InvalidateUser 删除用户的状态缓存。封禁、启用、改角色、重置密码、改并发上限后必须调用，否则最多 30 分钟后才生效。
func (s *UserService) InvalidateUser(ctx context.Context, id uint64) {
	if err := s.Cache.Delete(ctx, id); err != nil {
		logger.Warn("删除用户缓存失败", zap.Error(err), zap.Uint64("user_id", id))
	}
}
