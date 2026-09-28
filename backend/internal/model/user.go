package model

type User struct {
	BaseModel
	Username string `gorm:"size:64;uniqueIndex;not null" json:"username"`
	Password string `gorm:"size:128;not null" json:"-"`
	Nickname string `gorm:"size:64" json:"nickname"`
	Email    string `gorm:"size:128" json:"email"`
}

func (User) TableName() string { return "users" }

// 登录请求参数
type LoginUserReq struct {
	Username string `json:"username" binding:"required,min=3,max=64" label:"用户名"`
	Password string `json:"password" binding:"required,min=6,max=128" label:"密码"`
}

// 注册请求参数
type RegisterUserReq struct {
	Username string `json:"username" binding:"required,min=3,max=64" label:"用户名"`
	Password string `json:"password" binding:"required,min=6,max=128" label:"密码"`
	Nickname string `json:"nickname" binding:"omitempty,min=1,max=64" label:"昵称"`
	Email    string `json:"email" binding:"omitempty,email,max=128" label:"邮箱"`
}

// 用户列表请求参数
type ListUserReq struct {
	Page     int `form:"page" label:"页码"`
	PageSize int `form:"page_size" label:"每页条数"`
}
