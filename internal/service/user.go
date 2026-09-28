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
	"video-canvas/pkg/pagination"
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
	// 1. 按用户名查用户，不存在时返回「用户名或密码错误」
	user, err := s.userRepo.GetByUsername(ctx, username)
	if errors.Is(err, repository.ErrNotFound) {
		return "", 0, errcode.ErrInvalidCredential
	}
	if err != nil {
		return "", 0, err
	}

	// 2. 校验密码：数据库存的是 bcrypt 哈希，不能直接比较明文
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(password)); err != nil {
		return "", 0, errcode.ErrInvalidCredential
	}

	// 3. 签发 JWT，载荷里带上用户 ID 和用户名，供鉴权中间件解析
	token, expireAt, err := utils.GenerateToken(uint(user.ID), user.Username, s.jwtSecret, s.jwtIssuer, s.jwtExpireHours)
	if err != nil {
		return "", 0, err
	}
	return token, expireAt, nil
}

// Register 用户注册，用户名重复时返回 ErrUserExists，密码以 bcrypt 哈希存储。
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

// Get 查询用户详情，优先读缓存；缓存读写失败不影响主流程。
func (s *UserService) Get(ctx context.Context, id uint64) (*model.User, error) {
	// 1. 先查 Redis 缓存，命中直接返回；未启用 Redis、未命中或读取出错都继续查库
	if u, err := s.cache.Get(ctx, id); err == nil && u != nil {
		return u, nil
	}
	// 2. 查数据库，记录不存在时转成业务错误「用户不存在」
	u, err := s.userRepo.GetByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, errcode.ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	// 3. 回写缓存（30 分钟过期），写失败只影响下次命中率，所以忽略错误
	_ = s.cache.Set(ctx, u)
	return u, nil
}

// List 分页查询用户列表，返回当前页数据、总数和修正后的分页参数（供响应回显）。
func (s *UserService) List(ctx context.Context, req *model.ListUserReq) ([]model.User, int64, pagination.Query, error) {
	// 1. 修正分页参数：page < 1 取 1，page_size < 1 取 10，最大 100
	q := pagination.Query{Page: req.Page, PageSize: req.PageSize}
	q.Normalize()
	// 2. 按 id 倒序分页查询；列表不走缓存，避免分页数据与缓存不一致
	users, total, err := s.userRepo.List(ctx, q.Offset(), q.Limit())
	return users, total, q, err
}
