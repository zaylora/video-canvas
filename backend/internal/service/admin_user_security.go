package service

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/repository"
)

// 后台用户的高危操作：调整角色、重置密码。只有 super_admin 能做；路由层挂了 RequireSuperAdmin，
// service 再判一次（权限判断用库里的最新角色，不依赖缓存），批量 / 内部调用同样受约束。

const (
	msgForbidSelfRole  = "不能修改自己的角色"
	msgLastSuperAdmin  = "不能降级最后一个超级管理员"
	minResetPasswordLn = 6   // 指定新密码的最短字数
	maxResetPasswordLn = 128 // 指定新密码的最长字数
	maxBcryptBytes     = 72  // bcrypt 只取前 72 字节，超过会被 x/crypto 拒绝，提前给出明确提示
	tempPasswordLen    = 16  // 生成的临时密码长度

	// 临时密码字符集：去掉容易混淆的 0 O 1 l I，运营口头 / 聊天软件转告时不易抄错。
	tempPasswordAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
)

// requireSuperAdmin 要求操作人是 super_admin。
func requireSuperAdmin(actor *model.User) error {
	if actor.Role != model.RoleSuperAdmin {
		return errcode.ErrForbidden
	}
	return nil
}

// validRole 判断是否是合法角色取值。
func validRole(role string) bool {
	return role == model.RoleUser || role == model.RoleAdmin || role == model.RoleSuperAdmin
}

// SetRole 调整用户角色（仅 super_admin）。角色没有变化时直接成功，不写库、不写审计。
// 不能改自己的角色；不能把最后一个 super_admin 降级。
func (s *AdminUserService) SetRole(ctx context.Context, actorID, targetID uint64, role string) error {
	// 1. 角色取值校验
	if !validRole(role) {
		return errcode.ErrInvalidParams.WithMsg("角色只能是 user、admin 或 super_admin")
	}
	// 2. 目标必须存在；操作人必须是 super_admin，且不能改自己的角色
	if _, err := s.loadTarget(ctx, targetID); err != nil {
		return err
	}
	actor, err := s.loadActor(ctx, actorID)
	if err != nil {
		return err
	}
	if err := requireSuperAdmin(actor); err != nil {
		return err
	}
	if actorID == targetID {
		return errcode.ErrForbidden.WithMsg(msgForbidSelfRole)
	}

	// 3. 事务：先取角色咨询锁串行化所有改角色请求，再读目标最新角色、统计 super_admin 数量、改角色。
	//    “统计数量”与“降级”必须在同一把锁里：否则两个超管同时互相降级，各自统计都看到 2 个，结果一个超管都不剩
	var from string
	err = s.repo.WithTx(ctx, func(tx repository.UserTx) error {
		var err error
		from, err = changeRoleLocked(ctx, tx, targetID, role)
		return err
	})
	if err != nil {
		return err
	}
	if from == role {
		return nil
	}

	// 4. 清缓存：RequireAdmin / RequireSuperAdmin 读的是缓存，不清的话新角色最多 30 分钟后才生效
	s.invalidate(ctx, targetID)
	// 5. 审计：记前后角色
	adminAudit(ctx, s.audit, actorID, model.AdminAuditUserRole, model.AdminAuditTargetUser, targetID,
		map[string]any{"from": from, "to": role})
	return nil
}

// changeRoleLocked 在事务里完成“取锁 → 读最新角色 → 校验最后一个超管 → 改角色”，返回改之前的角色（没有变化时不写库）。
func changeRoleLocked(ctx context.Context, tx repository.UserTx, targetID uint64, role string) (from string, err error) {
	if err := tx.LockRoleChange(ctx); err != nil {
		return "", err
	}
	cur, err := tx.GetByID(ctx, targetID)
	if errors.Is(err, repository.ErrNotFound) {
		return "", errcode.ErrUserNotFound
	}
	if err != nil {
		return "", err
	}
	if cur.Role == role {
		return cur.Role, nil // 没有变化
	}
	if cur.Role == model.RoleSuperAdmin {
		n, err := tx.CountByRole(ctx, model.RoleSuperAdmin)
		if err != nil {
			return "", err
		}
		if n <= 1 {
			return "", errcode.ErrForbidden.WithMsg(msgLastSuperAdmin)
		}
	}
	return cur.Role, tx.Update(ctx, targetID, map[string]any{"role": role})
}

// ResetPassword 重置用户密码（仅 super_admin，可以重置自己的）。newPassword 为空时生成强随机临时密码，否则按指定值（6..128 字）。
// 返回的明文只在这次响应里出现一次：库里存 bcrypt 哈希，日志与审计都不记明文。
func (s *AdminUserService) ResetPassword(ctx context.Context, actorID, targetID uint64, newPassword string) (*model.ResetPasswordView, error) {
	// 1. 目标存在 + 操作人必须是 super_admin
	if _, err := s.loadTarget(ctx, targetID); err != nil {
		return nil, err
	}
	actor, err := s.loadActor(ctx, actorID)
	if err != nil {
		return nil, err
	}
	if err := requireSuperAdmin(actor); err != nil {
		return nil, err
	}

	// 2. 确定新密码：未指定则生成；指定则校验长度
	generated := newPassword == ""
	if generated {
		if newPassword, err = generateTempPassword(); err != nil {
			return nil, err
		}
	} else if err := validNewPassword(newPassword); err != nil {
		return nil, err
	}

	// 3. bcrypt 哈希后写库，同一条 UPDATE 里 token_version +1：该用户已签发的 token 在 RequireActive 里版本对不上，立即失效
	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ResetPassword(ctx, targetID, string(hash)); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, errcode.ErrUserNotFound // 两次查询之间被删除
		}
		return nil, err
	}

	// 4. 清缓存：RequireActive 读缓存里的 token_version，不清的话旧 token 最多还能用 30 分钟
	s.invalidate(ctx, targetID)
	// 5. 审计只记是否系统生成，绝不记密码
	adminAudit(ctx, s.audit, actorID, model.AdminAuditResetPassword, model.AdminAuditTargetUser, targetID,
		map[string]any{"generated": generated})
	return &model.ResetPasswordView{TempPassword: newPassword}, nil
}

// validNewPassword 校验管理员指定的新密码：6..128 个字符，且不超过 bcrypt 的 72 字节上限。
func validNewPassword(p string) error {
	if n := utf8.RuneCountInString(p); n < minResetPasswordLn || n > maxResetPasswordLn {
		return errcode.ErrInvalidParams.WithMsg("新密码长度需为 6 到 128 位")
	}
	if len(p) > maxBcryptBytes {
		return errcode.ErrInvalidParams.WithMsg("新密码不能超过 72 字节（约 24 个汉字）")
	}
	return nil
}

// generateTempPassword 用 crypto/rand 生成临时密码：16 位、字符集见 tempPasswordAlphabet，且至少含一个字母和一个数字。
func generateTempPassword() (string, error) {
	upper := big.NewInt(int64(len(tempPasswordAlphabet)))
	buf := make([]byte, tempPasswordLen)
	for {
		var hasLetter, hasDigit bool
		for i := range buf {
			n, err := rand.Int(rand.Reader, upper) // 按范围取随机数，避免取模偏差
			if err != nil {
				return "", err
			}
			c := tempPasswordAlphabet[n.Int64()]
			buf[i] = c
			if c >= '0' && c <= '9' {
				hasDigit = true
			} else {
				hasLetter = true
			}
		}
		if hasLetter && hasDigit {
			return string(buf), nil
		}
	}
}
