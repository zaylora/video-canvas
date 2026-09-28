package pagination

const (
	DefaultPage     = 1
	DefaultPageSize = 10
	MaxPageSize     = 100
)

// Query 是通用分页参数，可直接用于 gin 的 ShouldBindQuery。
type Query struct {
	Page     int `form:"page" json:"page"`
	PageSize int `form:"page_size" json:"page_size"`
}

// Normalize 把非法值修正为默认值，并限制单页最大条数。
func (q *Query) Normalize() {
	if q.Page < 1 {
		q.Page = DefaultPage
	}
	if q.PageSize < 1 {
		q.PageSize = DefaultPageSize
	}
	if q.PageSize > MaxPageSize {
		q.PageSize = MaxPageSize
	}
}

func (q Query) Offset() int { return (q.Page - 1) * q.PageSize }

func (q Query) Limit() int { return q.PageSize }
