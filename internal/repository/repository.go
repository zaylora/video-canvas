package repository

import (
	"errors"

	"gorm.io/gorm"
)

// ErrNotFound 屏蔽 gorm 细节，service 层只依赖这个错误。
var ErrNotFound = errors.New("record not found")

func translate(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
