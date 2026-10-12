package pagination_test

import (
	"testing"

	"video-canvas/pkg/pagination"
)

func TestNewQuery(t *testing.T) {
	cases := []struct {
		name       string
		page, size int
		opt        pagination.Options
		want       pagination.Query
	}{
		{"都没传：第 1 页，默认条数", 0, 0, pagination.Options{DefaultSize: 20, MaxSize: 100}, pagination.Query{Page: 1, PageSize: 20}},
		{"负数当作没传", -3, -1, pagination.Options{DefaultSize: 20, MaxSize: 100}, pagination.Query{Page: 1, PageSize: 20}},
		{"正常值原样保留", 3, 50, pagination.Options{DefaultSize: 20, MaxSize: 100}, pagination.Query{Page: 3, PageSize: 50}},
		{"超过上限按上限", 2, 500, pagination.Options{DefaultSize: 20, MaxSize: 100}, pagination.Query{Page: 2, PageSize: 100}},
		{"零值 Options 用包默认（10 / 50）", 0, 999, pagination.Options{}, pagination.Query{Page: 1, PageSize: 50}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pagination.NewQuery(c.page, c.size, c.opt); got != c.want {
				t.Errorf("实际 %+v，期望 %+v", got, c.want)
			}
		})
	}
}

func TestAdminQuery(t *testing.T) {
	if got := pagination.AdminQuery(0, 0); got != (pagination.Query{Page: 1, PageSize: 20}) {
		t.Errorf("后台列表默认每页 20：%+v", got)
	}
	if got := pagination.AdminQuery(1, 500); got.PageSize != 100 {
		t.Errorf("后台列表单页上限 100：%+v", got)
	}
}

func TestSlice(t *testing.T) {
	list := []int{1, 2, 3, 4, 5}
	cases := []struct {
		name string
		q    pagination.Query
		want []int
	}{
		{"第一页", pagination.Query{Page: 1, PageSize: 2}, []int{1, 2}},
		{"最后一页不足一页", pagination.Query{Page: 3, PageSize: 2}, []int{5}},
		{"超出范围得到空切片", pagination.Query{Page: 9, PageSize: 2}, []int{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pagination.Slice(list, c.q)
			if got == nil || len(got) != len(c.want) {
				t.Fatalf("实际 %v，期望 %v（且不能是 nil）", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Errorf("实际 %v，期望 %v", got, c.want)
				}
			}
		})
	}
	if got := pagination.Slice([]int(nil), pagination.Query{Page: 1, PageSize: 2}); got == nil {
		t.Error("空输入也要返回非 nil 切片，JSON 才是 []")
	}
}

func TestPaginate(t *testing.T) {
	list := []string{"a", "b", "c", "d", "e"}
	t.Run("过滤后分页，total 是命中总数", func(t *testing.T) {
		page := pagination.Paginate(list, pagination.Query{Page: 1, PageSize: 2}, func(s string) bool { return s != "a" })
		if page.Total != 4 || len(page.List) != 2 || page.List[0] != "b" || page.Page != 1 || page.PageSize != 2 {
			t.Errorf("实际 %+v", page)
		}
	})
	t.Run("filter 为 nil 表示不过滤", func(t *testing.T) {
		page := pagination.Paginate(list, pagination.Query{Page: 3, PageSize: 2}, nil)
		if page.Total != 5 || len(page.List) != 1 || page.List[0] != "e" {
			t.Errorf("实际 %+v", page)
		}
	})
	t.Run("没有命中：空列表不是 nil", func(t *testing.T) {
		page := pagination.Paginate(list, pagination.Query{Page: 1, PageSize: 2}, func(string) bool { return false })
		if page.Total != 0 || page.List == nil || len(page.List) != 0 {
			t.Errorf("实际 %+v", page)
		}
	})
}

func TestMatchKeyword(t *testing.T) {
	cases := []struct {
		name   string
		q      string
		fields []string
		want   bool
	}{
		{"关键字为空恒为真", "", []string{"abc"}, true},
		{"只有空格也算空", "   ", []string{"abc"}, true},
		{"忽略大小写", "ABC", []string{"xabcx"}, true},
		{"任一字段命中即可", "对象", []string{"s3", "对象存储"}, true},
		{"首尾空格被忽略", " s3 ", []string{"S3"}, true},
		{"都不含则不命中", "zzz", []string{"a", "b"}, false},
		{"没有字段不命中", "a", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pagination.MatchKeyword(c.q, c.fields...); got != c.want {
				t.Errorf("实际 %v，期望 %v", got, c.want)
			}
		})
	}
}
