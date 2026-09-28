package service

import (
	"context"
	"errors"

	"golang.org/x/crypto/bcrypt"

	"video-canvas/internal/cache"
	"video-canvas/internal/model"
	"video-canvas/internal/pkg/errcode"
	"video-canvas/internal/pkg/utils"
	"video-canvas/internal/repository"
)

// 依赖以接口声明在使用方，便于单元测试时替换成 mock。
type UserRepo interface {
	Create(ctx context.Context, u *model.User) error
	GetByID(ctx context.Context, id uint64) (*model.User, error)
	GetByUsername(ctx context.Context, username string) (*model.User, error)
	List(ctx context.Context, offset, limit int) ([]model.User, int64, error)
	Update(ctx context.Context, id uint64, fields map[string]any) error
	Delete(ctx context.Context, id uint64) error
}

type UserService struct {
	userRepo UserRepo
	cache    *cache.UserCache

	jwtSecret      string
	jwtIssuer      string
	jwtExpireHours int
}

func NewUserService(repo UserRepo, userCache *cache.UserCache, jwtSecret, jwtIssuer string, jwtExpireHours int) *UserService {
	return &UserService{
		userRepo:       repo,
		cache:          userCache,
		jwtSecret:      jwtSecret,
		jwtIssuer:      jwtIssuer,
		jwtExpireHours: jwtExpireHours,
	}
}

// Login 用户名密码登录，成功后返回 token 和过期时间（Unix 秒）。
// 用户不存在和密码错误统一返回同一个错误，避免暴露「用户名是否存在」。
func (s *UserService) Login(ctx context.Context, username, password string) (string, int64, error) {
	user, err := s.userRepo.GetByUsername(ctx, username)
	if errors.Is(err, repository.ErrNotFound) {
		return "", 0, errcode.ErrInvalidCredential
	}
	if err != nil {
		return "", 0, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return "", 0, errcode.ErrInvalidCredential
	}

	token, expireAt, err := utils.GenerateToken(uint(user.ID), user.Username, s.jwtSecret, s.jwtIssuer, s.jwtExpireHours)
	if err != nil {
		return "", 0, err
	}
	return token, expireAt, nil
}

// 用户注册，成功后返回用户 ID。
func (s *UserService) Register(ctx context.Context, username, password string) error {
	// 1. 检查用户名是否已存在
	if _, err := s.userRepo.GetByUsername(ctx, username); err == nil {
		return errcode.ErrUserExists
	} else if !errors.Is(err, repository.ErrNotFound) {
		return err
	}

	// 2. 密码加密
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}

	// 3. 创建用户
	user := &model.User{
		Username: username,
		Password: string(hashedPassword),
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return err
	}
	return nil
}
