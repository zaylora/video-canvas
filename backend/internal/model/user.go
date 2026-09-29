package model

// 用户角色。
const (
	RoleUser  = "user"  // 普通用户
	RoleAdmin = "admin" // 管理员
)

type User struct {
	BaseModel
	Username string `gorm:"size:64;uniqueIndex;not null" json:"username"` // 用户名，唯一
	Password string `gorm:"size:128;not null" json:"-"`                   // bcrypt 哈希后的密码
	Nickname string `gorm:"size:64" json:"nickname"`                      // 昵称
	Email    string `gorm:"size:128" json:"email"`                        // 邮箱
	Role     string `gorm:"size:16;not null;default:user" json:"role"`    // user / admin，管理接口要求 admin
}

func (User) TableName() string { return "users" }

// 登录请求参数
type LoginUserReq struct {
	Username string `json:"username" binding:"required,min=3,max=64" label:"用户名"` // 用户名
	Password string `json:"password" binding:"required,min=6,max=128" label:"密码"` // 明文密码
}

// 注册请求参数
type RegisterUserReq struct {
	Username string `json:"username" binding:"required,min=3,max=64" label:"用户名"` // 用户名
	Password string `json:"password" binding:"required,min=6,max=128" label:"密码"` // 明文密码
	Nickname string `json:"nickname" binding:"omitempty,min=1,max=64" label:"昵称"` // 昵称，可不传
	Email    string `json:"email" binding:"omitempty,email,max=128" label:"邮箱"`   // 邮箱，可不传
}

// 用户列表请求参数
type ListUserReq struct {
	Page     int `form:"page" label:"页码"`        // 页码，从 1 开始
	PageSize int `form:"page_size" label:"每页条数"` // 每页条数
}
