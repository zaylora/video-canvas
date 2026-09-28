package repository

import (
	"errors"

	"gorm.io/gorm"
)

// ErrNotFound 屏蔽 gorm 细节，service 层只依赖这个错误。
var ErrNotFound = errors.New("record not found")

// ErrRevisionConflict 乐观锁冲突：记录存在，但版本号已被其他请求更新。
var ErrRevisionConflict = errors.New("revision conflict")

func translate(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
