package service

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// 验证码规则。
const (
	registerCodeTTL      = 10 * time.Minute // 验证码有效期
	registerCodeAttempts = 5                // 错误这么多次后验证码作废
	registerCodeCooldown = 60 * time.Second // 同邮箱重复发送的冷却
	registerIPLimit      = 20               // 同 IP 每小时最多发码次数
	registerIPWindow     = time.Hour
)

// registerState 汇总“能不能注册、要不要验证码”所需的现状。
type registerState struct {
	open           bool // 当前是否开放注册
	verifyRequired bool // 注册是否需要邮箱验证码
	smtpOn         bool // SMTP 是否已启用
}

// loadRegisterState 读取注册开关、SMTP 状态与用户表是否为空，按契约算出 register_enabled / email_verify_required：
//   - 开放 = 注册开关打开；
//   - 需要验证码 = SMTP 已启用 且 users 表非空（首个账号免验证）。
//     「邮件服务」页的启用开关就是是否验证邮箱的选择：没启用时邮箱照样必填，只是不验证（不写 email_verified_at）。
func (s *UserService) loadRegisterState(ctx context.Context) (registerState, error) {
	switchOn, err := s.Policy.RegisterEnabled(ctx)
	if err != nil {
		return registerState{}, err
	}
	smtpOn, err := s.Mail.Enabled(ctx)
	if err != nil {
		return registerState{}, err
	}
	n, err := s.Repo.Count(ctx)
	if err != nil {
		return registerState{}, err
	}
	empty := n == 0
	return registerState{open: switchOn, verifyRequired: smtpOn && !empty, smtpOn: smtpOn}, nil
}

// AuthConfig 返回登录页需要的注册配置（GET /auth/config）。
func (s *UserService) AuthConfig(ctx context.Context) (*model.AuthConfigView, error) {
	st, err := s.loadRegisterState(ctx)
	if err != nil {
		return nil, err
	}
	return &model.AuthConfigView{RegisterEnabled: st.open, EmailVerifyRequired: st.verifyRequired}, nil
}

// normalizeEmail 去掉首尾空白并转小写：邮箱大小写不敏感，库里统一存小写。
func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// SendRegisterCode 给邮箱发送 6 位注册验证码（POST /auth/register/code）。
func (s *UserService) SendRegisterCode(ctx context.Context, email, ip string) error {
	email = normalizeEmail(email)

	// 1. 注册关闭或没有启用 SMTP 时没有发码的意义，统一返回“暂未开放注册”
	st, err := s.loadRegisterState(ctx)
	if err != nil {
		return err
	}
	if !st.open || !st.smtpOn {
		return errcode.ErrRegisterClosed
	}

	// 2. 邮箱已被注册直接告知（注册页本来就要提示这一点，不额外防枚举）
	exists, err := s.Repo.EmailExists(ctx, email)
	if err != nil {
		return err
	}
	if exists {
		return errcode.ErrEmailExists
	}

	// 3. 限频：先查同 IP 每小时上限，再查同邮箱 60 秒冷却。
	//    顺序是故意的：冷却锁一旦占下就会挡住该邮箱，所以放在最后，被 IP 限频拒绝的请求不会白白锁住邮箱
	if ip == "" {
		ip = "unknown"
	}
	allowed, err := s.Codes.Allow(ctx, "ip:"+ip, registerIPLimit, registerIPWindow)
	if err != nil {
		return err
	}
	if !allowed {
		return errcode.ErrTooManyReqs
	}
	locked, err := s.Codes.TryLock(ctx, "email:"+email, registerCodeCooldown)
	if err != nil {
		return err
	}
	if !locked {
		return errcode.ErrTooManyReqs.WithMsg("验证码发送过于频繁，请 60 秒后再试")
	}

	// 4. 生成并保存验证码（覆盖旧码并清零错误次数），再发信
	code, err := newRegisterCode()
	if err != nil {
		return err
	}
	if err := s.Codes.Save(ctx, email, code, registerCodeTTL); err != nil {
		return err
	}
	// debug 模式把验证码打到日志，本地没有真实 SMTP 时也能联调；release 绝不输出（验证码等同于登录凭证）
	if s.Debug {
		logger.Info("【debug】注册验证码", zap.String("email", email), zap.String("code", code))
	}
	return s.Mail.SendVerifyCode(ctx, email, code)
}

// newRegisterCode 用密码学随机数生成 6 位数字验证码（允许前导 0）。
func newRegisterCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("生成验证码失败：%w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// Register 注册并直接登录（POST /auth/register）。
// users 表为空时第一个注册的账号成为 super_admin（并发安全）；之后的账号都是普通用户。
func (s *UserService) Register(ctx context.Context, req *model.RegisterUserReq, meta ClientMeta) (*model.LoginView, error) {
	email := normalizeEmail(req.Email)

	// 1. 注册必须开放（注册开关打开）
	st, err := s.loadRegisterState(ctx)
	if err != nil {
		return nil, err
	}
	if !st.open {
		return nil, errcode.ErrRegisterClosed
	}

	// 2. 需要验证码时校验：缺失、错误、过期、次数用尽统一返回“验证码错误或已过期”，不泄露具体原因；
	//    验证码在这里一次性消费。按契约顺序它排在用户名 / 邮箱唯一性检查之前
	verified := false
	if st.verifyRequired {
		ok := false
		if req.Code != "" {
			if ok, err = s.Codes.Check(ctx, email, req.Code, registerCodeAttempts); err != nil {
				return nil, err
			}
		}
		if !ok {
			return nil, errcode.ErrCodeInvalid
		}
		verified = true
	}

	// 3. 事务外先做耗时操作：bcrypt 哈希与读初始积分，避免持有注册锁时做 CPU 密集计算
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	initial, err := s.Policy.InitialCredits(ctx)
	if err != nil {
		return nil, err
	}

	// 4. 事务内建号
	user := &model.User{Username: req.Username, Email: email, Password: string(hashed), Status: model.UserStatusActive}
	if err := s.Repo.WithTx(ctx, func(tx repository.UserTx) error {
		return s.registerInTx(ctx, tx, user, initial, verified, st.smtpOn, meta)
	}); err != nil {
		return nil, err
	}

	// 5. 注册即登录：签发 token
	return s.issueToken(user)
}

// registerInTx 是注册事务体：
//  1. 取注册锁，串行化所有注册，让“表是否为空”的判断与插入原子；
//  2. 锁内重新判断是否第一个用户：预检（锁外）之后可能已有别人注册，此时必须按“非首个”的规则复核，
//     否则两个并发请求都会以“免验证码”的身份注册成功，且都当上 super_admin；
//  3. 查用户名、邮箱唯一性（锁内，结果可靠）；
//  4. 建用户、积分账户（含 initial 流水）与 register 登录记录。
func (s *UserService) registerInTx(ctx context.Context, tx repository.UserTx, user *model.User, initial int, verified, smtpOn bool, meta ClientMeta) error {
	if err := tx.LockRegistration(ctx); err != nil {
		return err
	}
	n, err := tx.Count(ctx)
	if err != nil {
		return err
	}
	first := n == 0
	if !first {
		// 启用了 SMTP 时，非首个账号必须验证过邮箱。这里复核是为了兜住预检与加锁之间的竞态
		if smtpOn && !verified {
			return errcode.ErrCodeInvalid
		}
	}
	if exists, err := tx.UsernameExists(ctx, user.Username); err != nil {
		return err
	} else if exists {
		return errcode.ErrUserExists
	}
	if exists, err := tx.EmailExists(ctx, user.Email); err != nil {
		return err
	} else if exists {
		return errcode.ErrEmailExists
	}

	user.Role = model.RoleUser
	if first {
		user.Role = model.RoleSuperAdmin
	}
	if verified {
		now := time.Now()
		user.EmailVerifiedAt = &now
	}
	if err := tx.Create(ctx, user); err != nil {
		// 唯一索引兜底（理论上被上面的检查拦住了）：按用户名重复处理
		if errors.Is(err, repository.ErrDuplicate) {
			return errcode.ErrUserExists
		}
		return err
	}
	if err := tx.CreateCredit(ctx, user.ID, initial); err != nil {
		return err
	}
	return tx.InsertLoginLog(ctx, &model.UserLoginLog{
		UserID: user.ID, Kind: model.LoginKindRegister, Result: model.LoginResultOK, IP: meta.IP, UserAgent: truncate(meta.UserAgent, maxUserAgentLen),
	})
}

// truncate 把字符串截到最多 n 字节。
func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
