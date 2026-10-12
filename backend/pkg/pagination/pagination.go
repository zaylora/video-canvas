package pagination

import "strings"

const (
	DefaultPage     = 1
	DefaultPageSize = 10
	MaxPageSize     = 50
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

// Options 描述一类列表的分页规则；零值字段取包默认（DefaultPageSize / MaxPageSize）。
type Options struct {
	DefaultSize int // 没传 page_size 时每页条数
	MaxSize     int // page_size 上限
}

// AdminOptions 是后台列表共用的规则：默认每页 20，最多 100（运营需要一屏看更多）。
var AdminOptions = Options{DefaultSize: 20, MaxSize: 100}

// NewQuery 按规则修正页码和每页条数：页码最小 1，条数缺省取默认值、超过上限取上限。
func NewQuery(page, size int, opt Options) Query {
	if opt.DefaultSize < 1 {
		opt.DefaultSize = DefaultPageSize
	}
	if opt.MaxSize < 1 {
		opt.MaxSize = MaxPageSize
	}
	q := Query{Page: page, PageSize: size}
	if q.Page < 1 {
		q.Page = DefaultPage
	}
	if q.PageSize < 1 {
		q.PageSize = opt.DefaultSize
	}
	if q.PageSize > opt.MaxSize {
		q.PageSize = opt.MaxSize
	}
	return q
}

// AdminQuery 是 NewQuery(page, size, AdminOptions) 的简写。
func AdminQuery(page, size int) Query { return NewQuery(page, size, AdminOptions) }

// Result 是一页结果：当页的行、命中总数，以及修正后的页码和每页条数（响应回显用）。
type Result[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// Slice 取出某一页；页码超出范围得到空切片。返回值永远不是 nil，JSON 序列化为 []。
func Slice[T any](items []T, q Query) []T {
	start := q.Offset()
	if start >= len(items) {
		return []T{}
	}
	end := min(start+q.Limit(), len(items))
	return append([]T{}, items[start:end]...)
}

// Paginate 先按 keep 过滤（nil 表示不过滤）再取某一页，Total 是过滤后的总数。
// 用于数据已整表读入内存、需要按派生字段筛选的小列表；大表请在 SQL 里分页。
func Paginate[T any](items []T, q Query, keep func(T) bool) Result[T] {
	matched := items
	if keep != nil {
		matched = make([]T, 0, len(items))
		for _, it := range items {
			if keep(it) {
				matched = append(matched, it)
			}
		}
	}
	return Result[T]{List: Slice(matched, q), Total: int64(len(matched)), Page: q.Page, PageSize: q.PageSize}
}

// MatchKeyword 判断任一字段是否包含关键字（忽略大小写和首尾空格）；关键字为空恒为真。
func MatchKeyword(q string, fields ...string) bool {
	text := strings.ToLower(strings.TrimSpace(q))
	if text == "" {
		return true
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), text) {
			return true
		}
	}
	return false
}
