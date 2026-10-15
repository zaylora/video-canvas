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

// ErrDuplicate 唯一约束冲突：记录已存在（如同一插件下版本号重复、渠道 key 重复）。
var ErrDuplicate = errors.New("duplicate record")

// ErrInUse 记录仍被引用，不能删除（如插件版本被渠道或进行中的任务使用）。
var ErrInUse = errors.New("record in use")

// ErrOrderMismatch 排序请求里的 key 列表不是现有记录的全集（重复、漏项），不写任何数据。
var ErrOrderMismatch = errors.New("order keys mismatch")

// sqlStateUniqueViolation 是 PostgreSQL 唯一约束冲突的 SQLSTATE。
const sqlStateUniqueViolation = "23505"

// isUniqueViolation 判断 err 是否为 PostgreSQL 唯一约束冲突（SQLSTATE 23505）。
// 用 SQLState() 方法的接口匹配 pgx 的 *pgconn.PgError，避免 repository 直接依赖 pgx 包。
func isUniqueViolation(err error) bool {
	var se interface{ SQLState() string }
	return errors.As(err, &se) && se.SQLState() == sqlStateUniqueViolation
}

// defaultListLimit 是列表类查询 limit<=0 时的默认条数。
const defaultListLimit = 50
