package stepkit_test

import (
	"strings"
	"testing"

	"video-canvas/internal/stepkit"
)

func TestCheckSchema(t *testing.T) {
	schema := []byte(`{
	  "type": "object", "required": ["n", "kind"],
	  "properties": {
	    "n": {"type": "integer", "minimum": 1, "maximum": 5},
	    "kind": {"type": "string", "enum": ["a", "b"]},
	    "name": {"type": "string", "minLength": 2, "maxLength": 4},
	    "ok": {"type": "boolean"},
	    "tags": {"type": "array", "items": {"type": "string"}}
	  }
	}`)
	good := `{"n":3,"kind":"a","name":"abc","ok":true,"tags":["x"],"extra":1}`
	if errs := stepkit.CheckSchema(schema, []byte(good)); len(errs) != 0 {
		t.Fatalf("合格输出不应有错：%v", errs)
	}
	cases := map[string]struct{ in, want string }{
		"缺必填":   {`{"n":3}`, "kind"},
		"枚举外":   {`{"n":3,"kind":"c"}`, "kind"},
		"整数小数":  {`{"n":1.5,"kind":"a"}`, "n"},
		"太小":    {`{"n":0,"kind":"a"}`, "n"},
		"太大":    {`{"n":6,"kind":"a"}`, "n"},
		"字符串太短": {`{"n":3,"kind":"a","name":"x"}`, "name"},
		"字符串太长": {`{"n":3,"kind":"a","name":"abcde"}`, "name"},
		"布尔类型错": {`{"n":3,"kind":"a","ok":"yes"}`, "ok"},
		"数组元素错": {`{"n":3,"kind":"a","tags":["x",2]}`, "tags[1]"},
	}
	for name, c := range cases {
		errs := stepkit.CheckSchema(schema, []byte(c.in))
		if len(errs) == 0 {
			t.Fatalf("%s：应报错", name)
		}
		if !strings.Contains(strings.Join(errs, ";"), c.want) {
			t.Fatalf("%s：错误里应指出 %q：%v", name, c.want, errs)
		}
	}
}
