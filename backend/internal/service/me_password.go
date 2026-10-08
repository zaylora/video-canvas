package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/logger"
	"video-canvas/internal/repository"
)

// 改密码的防爆破参数：15 分钟内当前密码输错 5 次，锁定 15 分钟。
const (
	pwChangeMaxFails = 5
	pwChangeWindow   = 15 * time.Minute
)

// ChangePassword 修改当前用户的密码（PUT /me/password），成功返回给当前设备续签的新 token（与登录响应同构）。
// 成功后 token_version +1：其他设备上的旧 token 立即失效、WebSocket 被断开；当前设备用新 token 无感续用。
func (s *MeService) ChangePassword(ctx context.Context, userID uint64, req *model.ChangePasswordReq) (*model.LoginView, error) {
	// 1. 锁定中直接拒绝（55004 / 429），连 bcrypt 都不做：防止锁定期间继续被用来试密码
	left, err := s.Limiter.Locked(ctx, userID, pwChangeMaxFails)
	if err != nil {
		return nil, err
	}
	if left > 0 {
		return nil, errcode.ErrPasswordTooFrequent.WithMsg(fmt.Sprintf("尝试过于频繁，请 %d 分钟后再试", ceilMinutes(left)))
	}

	// 2. 读用户（统一规则要比对用户名，比对旧密码要哈希）
	u, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	// 3. 新密码按统一规则校验（与注册、后台重置同一份实现）。放在比对旧密码之前：
	//    新密码不合规是“填错表单”，不应消耗当前密码的尝试次数
	if err := validateNewPassword(u.Username, req.NewPassword); err != nil {
		return nil, err
	}

	// 4. 比对当前密码：错误时计一次失败，提示剩余次数；第 5 次错误后锁定，提示 15 分钟后再试
	if bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(req.OldPassword)) != nil {
		return nil, s.oldPasswordWrong(ctx, userID)
	}

	// 5. 新旧相同：当前密码已验证通过，所以直接比较明文即可
	if req.NewPassword == req.OldPassword {
		return nil, errcode.ErrPasswordSame
	}

	// 6. 当前密码已通过：清零失败计数。清零失败只记日志——最坏情况是之前的错误次数多留一会儿，不影响本次改密码
	if err := s.Limiter.Reset(ctx, userID); err != nil {
		logger.Warn("清零改密码失败计数失败", zap.Error(err), zap.Uint64("user_id", userID))
	}

	// 7. 写库：同一条 UPDATE 改哈希并 token_version +1，已签发的 token 在 RequireActive 里版本对不上，立即失效
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	if err := s.Repo.ResetPassword(ctx, userID, string(hash)); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrUserNotFound
		}
		return nil, err
	}

	// 8. 清缓存（RequireActive 读缓存里的 token_version）并断开全部 WS：当前页会用新 token 重新申请 ticket 后重连
	s.Users.InvalidateUser(ctx, userID)
	s.Conns.DisconnectUser(userID)

	// 9. 重新读用户拿到最新的 token_version，给当前设备签发新 token
	fresh, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	return s.Tokens.IssueToken(fresh)
}

// oldPasswordWrong 记一次失败并构造 55001：Msg 带剩余次数；次数用完时提示已锁定。
func (s *MeService) oldPasswordWrong(ctx context.Context, userID uint64) error {
	n, err := s.Limiter.Fail(ctx, userID, pwChangeMaxFails, pwChangeWindow)
	if err != nil {
		return err
	}
	if left := pwChangeMaxFails - n; left > 0 {
		return errcode.ErrOldPasswordWrong.WithMsg(fmt.Sprintf("当前密码错误，还可尝试 %d 次", left))
	}
	return errcode.ErrOldPasswordWrong.WithMsg(fmt.Sprintf("当前密码错误，尝试次数已用完，请 %d 分钟后再试", int(pwChangeWindow/time.Minute)))
}

// ceilMinutes 把剩余时长向上取整到分钟（至少 1 分钟），用于提示文案。
func ceilMinutes(d time.Duration) int {
	m := int((d + time.Minute - 1) / time.Minute)
	return max(m, 1)
}
