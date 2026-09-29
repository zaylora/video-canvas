package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// JSONText 是存成 PostgreSQL json（而不是 jsonb）的 JSON 文本。
//
// jsonb 会重排对象的键，而配置正文里 input_schema 的书写顺序就是前端的渲染顺序，
// 所以配置版本表必须保留原文。datatypes.JSON 在 PostgreSQL 下固定映射成 jsonb，不能用。
type JSONText json.RawMessage

// Value 实现 driver.Valuer：空值写成 SQL NULL 以外的空对象前由调用方保证非空。
func (j JSONText) Value() (driver.Value, error) {
	if len(j) == 0 {
		return nil, nil
	}
	return string(j), nil
}

// Scan 实现 sql.Scanner。
func (j *JSONText) Scan(value any) error {
	switch v := value.(type) {
	case nil:
		*j = nil
	case []byte:
		*j = append((*j)[:0], v...)
	case string:
		*j = append((*j)[:0], v...)
	default:
		return fmt.Errorf("JSONText: 不支持的扫描类型 %T", value)
	}
	return nil
}

// MarshalJSON 输出原文；空值输出 null。
func (j JSONText) MarshalJSON() ([]byte, error) {
	if len(j) == 0 {
		return []byte("null"), nil
	}
	return j, nil
}

// UnmarshalJSON 保存原文。
func (j *JSONText) UnmarshalJSON(data []byte) error {
	if j == nil {
		return errors.New("JSONText: UnmarshalJSON on nil pointer")
	}
	*j = append((*j)[:0], data...)
	return nil
}

// GormDataType 告诉 GORM 这是 json 类型。
func (JSONText) GormDataType() string { return "json" }

// GormDBDataType 按数据库方言返回列类型，PostgreSQL 用 json。
func (JSONText) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db.Dialector.Name() == "postgres" {
		return "json"
	}
	return "text"
}
