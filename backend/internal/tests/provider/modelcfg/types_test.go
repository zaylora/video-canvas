// 本文件：types.go 的单元测试：InputSchema 的保序编解码、Duration 的解析。

package modelcfg_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"video-canvas/internal/provider/modelcfg"
)

func TestInputSchema_保序编解码(t *testing.T) {
	src := `{"z":{"type":"text","label":"Z"},"a":{"type":"number","label":"A","min":1},"m":{"type":"enum","label":"M","options":[{"value":1,"label":"一"}]}}`
	var s modelcfg.InputSchema
	if err := json.Unmarshal([]byte(src), &s); err != nil {
		t.Fatal(err)
	}
	if len(s) != 3 || s[0].Name != "z" || s[1].Name != "a" || s[2].Name != "m" {
		t.Fatalf("顺序不对：%+v", s)
	}
	out, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), `{"z":`) || strings.Index(string(out), `"a":`) > strings.Index(string(out), `"m":`) {
		t.Fatalf("输出顺序不对：%s", out)
	}
	var back modelcfg.InputSchema
	if err := json.Unmarshal(out, &back); err != nil {
		t.Fatal(err)
	}
	if len(back) != 3 || back[1].Min == nil || *back[1].Min != 1 {
		t.Fatalf("往返丢失信息：%+v", back)
	}
	// null 与空对象
	var empty modelcfg.InputSchema
	if err := json.Unmarshal([]byte(`null`), &empty); err != nil || empty != nil {
		t.Fatalf("null 应得到空 schema：%v %v", empty, err)
	}
	if err := json.Unmarshal([]byte(`{}`), &empty); err != nil || len(empty) != 0 {
		t.Fatalf("空对象应得到空 schema：%v %v", empty, err)
	}
	if err := json.Unmarshal([]byte(`[]`), &empty); err == nil {
		t.Fatal("数组应报错")
	}
}

func TestDuration_解析(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{`"10s"`, 10 * time.Second, false},
		{`"1h30m"`, 90 * time.Minute, false},
		{`30`, 30 * time.Second, false}, // 数字按秒
		{`""`, 0, false},
		{`null`, 0, false},
		{`"soon"`, 0, true},
		{`true`, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			var d modelcfg.Duration
			err := json.Unmarshal([]byte(tt.in), &d)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v，期望出错=%v", err, tt.wantErr)
			}
			if err == nil && d.D() != tt.want {
				t.Fatalf("得到 %v，期望 %v", d.D(), tt.want)
			}
		})
	}
	out, _ := json.Marshal(modelcfg.Duration(90 * time.Second))
	if string(out) != `"1m30s"` {
		t.Fatalf("输出 %s", out)
	}
}
