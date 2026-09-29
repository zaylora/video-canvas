// 本文件：表达式引擎：基于 expr-lang 编译并执行 ${...} 表达式，包含自定义函数、模板字符串解析与渲染、
// 引用（input / model / ctx / resp）收集，以及各阶段（请求 / 响应 / 轮询）可用变量的静态检查。

package dsl

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/file"
	"github.com/expr-lang/expr/parser/lexer"
	"github.com/expr-lang/expr/vm"
)

// 表达式限制：源码长度与语法树节点数上限，防止配置被用来构造超大表达式。
const (
	maxExprLen   = 4096
	maxExprNodes = 500
)

// exprEnv 是表达式的编译与运行环境。所有变量都是 any 类型，
// 这样编译期只检查“变量名是否存在”，成员访问与运算留到运行时动态判断。
// 注意：这里没有任何 secret 相关字段——凭证不会进入表达式。
type exprEnv struct {
	Input   any `expr:"input"`
	Files   any `expr:"files"`
	Model   any `expr:"model"`
	Task    any `expr:"task"`
	Ctx     any `expr:"ctx"`
	Resp    any `expr:"resp"`
	Status  any `expr:"status"`
	Req     any `expr:"req"`
	Outputs any `expr:"outputs"`
	Upload  any `expr:"upload"`
}

// exprStage 描述表达式所处的阶段，决定哪些变量可用（保存时据此报错，例如 submit 的 body 里不能用 resp）。
type exprStage string

const (
	stageMapping  exprStage = "mapping"  // model.mapping：input / files / model / ctx
	stageUpload   exprStage = "upload"   // 上传请求：input / model / ctx / upload
	stageSubmit   exprStage = "submit"   // 提交请求：input / files / model / task / ctx
	stageQuery    exprStage = "query"    // 查询 / 取消请求：input / files / model / task / ctx
	stageResponse exprStage = "response" // success / extract / error_rules.when：再加 resp / status
	stageWebhook  exprStage = "webhook"  // webhook.task_id：req / ctx
	stageOutput   exprStage = "output"   // output.select：outputs / input / model / ctx
	stageAny      exprStage = "any"      // 运行时求值：不限制变量
)

// stageVars 各阶段允许引用的变量名。
var stageVars = map[exprStage][]string{
	stageMapping:  {"input", "files", "model", "ctx"},
	stageUpload:   {"input", "model", "ctx", "upload"},
	stageSubmit:   {"input", "files", "model", "task", "ctx"},
	stageQuery:    {"input", "files", "model", "task", "ctx"},
	stageResponse: {"input", "files", "model", "task", "ctx", "resp", "status"},
	stageWebhook:  {"req", "ctx"},
	stageOutput:   {"outputs", "input", "model", "ctx"},
}

// compiledExpr 是一次编译的结果：程序 + 引用到的变量与成员（供校验跨对象引用）。
type compiledExpr struct {
	program *vm.Program
	idents  map[string]struct{} // 引用的变量名
	// inputRefs / fileRefs 是 input.xxx / files.xxx 形式的静态引用（属性名）。
	inputRefs []string
	fileRefs  []string
}

var (
	exprCache     sync.Map // 源码 -> *compiledExpr（只缓存编译成功的）
	exprCacheSize atomic.Int64
)

const exprCacheLimit = 4096

// customFuncNames 是自定义函数名（允许出现在 CallNode 的被调用位置）。
var customFuncNames = map[string]struct{}{
	"parseJSON": {}, "regexMatches": {}, "coalesce": {}, "toString": {}, "toInt": {},
}

// exprOptions 返回统一的编译选项：变量环境、节点数上限、自定义函数。
func exprOptions() []expr.Option {
	return []expr.Option{
		expr.Env(exprEnv{}),
		expr.MaxNodes(maxExprNodes),
		expr.Function("parseJSON", fnParseJSON),
		// matches 是 expr 的内置运算符，函数调用形式 matches(a, b) 会在预处理里改写成 regexMatches(a, b)。
		expr.Function("regexMatches", fnMatches),
		expr.Function("coalesce", fnCoalesce),
		expr.Function("toString", fnToString),
		expr.Function("toInt", fnToInt),
	}
}

// compileExpr 预处理并编译表达式；同一源码只编译一次。
func compileExpr(src string) (*compiledExpr, error) {
	if v, ok := exprCache.Load(src); ok {
		return v.(*compiledExpr), nil
	}
	if strings.TrimSpace(src) == "" {
		return nil, fmt.Errorf("表达式不能为空")
	}
	if utf8.RuneCountInString(src) > maxExprLen {
		return nil, fmt.Errorf("表达式过长（最多 %d 个字符）", maxExprLen)
	}
	pre := preprocessExpr(src)
	prog, err := expr.Compile(pre, exprOptions()...)
	if err != nil {
		return nil, fmt.Errorf("%s", cleanExprError(err))
	}
	ce := &compiledExpr{program: prog, idents: map[string]struct{}{}}
	collectRefs(prog.Node(), ce)
	if exprCacheSize.Load() < exprCacheLimit {
		if _, loaded := exprCache.LoadOrStore(src, ce); !loaded {
			exprCacheSize.Add(1)
		}
	}
	return ce, nil
}

// cleanExprError 把 expr 的错误压成一行，去掉带箭头的源码片段。
func cleanExprError(err error) string {
	if fe, ok := err.(*file.Error); ok {
		return fmt.Sprintf("%s（第 %d 列）", fe.Message, fe.Column+1)
	}
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return msg
}

// collectRefs 遍历语法树，收集变量名以及 input.xxx / files.xxx 静态引用。
func collectRefs(root ast.Node, ce *compiledExpr) {
	v := &refVisitor{ce: ce, declared: map[string]struct{}{}}
	ast.Walk(&root, v)
	for _, name := range v.idents {
		if _, ok := v.declared[name]; ok {
			continue
		}
		ce.idents[name] = struct{}{}
	}
}

type refVisitor struct {
	ce       *compiledExpr
	idents   []string
	declared map[string]struct{}
}

func (v *refVisitor) Visit(node *ast.Node) {
	switch n := (*node).(type) {
	case *ast.VariableDeclaratorNode:
		v.declared[n.Name] = struct{}{}
	case *ast.IdentifierNode:
		v.idents = append(v.idents, n.Value)
	case *ast.MemberNode:
		id, ok := n.Node.(*ast.IdentifierNode)
		if !ok {
			return
		}
		prop, ok := n.Property.(*ast.StringNode)
		if !ok {
			return
		}
		switch id.Value {
		case "input":
			v.ce.inputRefs = append(v.ce.inputRefs, prop.Value)
		case "files":
			v.ce.fileRefs = append(v.ce.fileRefs, prop.Value)
		}
	}
}

// checkStage 校验表达式只用到本阶段允许的变量；自定义函数名不算变量。
func (ce *compiledExpr) checkStage(stage exprStage) error {
	if stage == stageAny {
		return nil
	}
	allowed := map[string]struct{}{}
	for _, n := range stageVars[stage] {
		allowed[n] = struct{}{}
	}
	var bad []string
	for name := range ce.idents {
		if _, ok := allowed[name]; ok {
			continue
		}
		if _, ok := customFuncNames[name]; ok {
			continue
		}
		bad = append(bad, name)
	}
	if len(bad) == 0 {
		return nil
	}
	sort.Strings(bad)
	return fmt.Errorf("此处不能使用变量 %s（可用：%s）", strings.Join(bad, "、"), strings.Join(stageVars[stage], "、"))
}

// predicateBuiltins 是 expr 里接收“谓词闭包”的内置函数：它们的花括号参数被当成闭包体，而不是对象字面量。
var predicateBuiltins = map[string]struct{}{
	"all": {}, "none": {}, "any": {}, "one": {}, "filter": {}, "map": {}, "count": {}, "sum": {},
	"find": {}, "findIndex": {}, "findLast": {}, "findLastIndex": {}, "groupBy": {}, "sortBy": {}, "reduce": {},
}

// preprocessExpr 对表达式源码做两处改写，使设计文档里的写法能被 expr 接受：
//  1. 函数调用形式的 matches(a, b) 改写成 regexMatches(a, b)（matches 在 expr 里是运算符，不能当函数名）；
//  2. 谓词内置函数（map / filter …）的参数如果是 {key: value} 对象字面量，外面补一层花括号，
//     即 map(xs, {url: .url}) 等价于 map(xs, {{url: .url}})。
//
// 任何词法错误都原样返回，交给 expr 自己报错。
func preprocessExpr(src string) string {
	if !strings.Contains(src, "matches") && !strings.Contains(src, "{") {
		return src
	}
	tokens, err := lexer.Lex(file.NewSource(src))
	if err != nil {
		return src
	}
	type bracket struct {
		open     string
		predCall bool // 该 "(" 属于谓词内置函数的调用
		wrap     bool // 该 "{" 需要在 "}" 后补一个闭合
	}
	type edit struct {
		from, to int // 替换的 rune 区间 [from, to)
		text     string
	}
	var (
		stack []bracket
		edits []edit
	)
	valueEnd := func(t lexer.Token) bool {
		switch t.Kind {
		case lexer.Identifier, lexer.Number, lexer.String, lexer.Bytes:
			return true
		case lexer.Bracket:
			return t.Value == ")" || t.Value == "]" || t.Value == "}"
		}
		return false
	}
	for i, t := range tokens {
		var prev *lexer.Token
		if i > 0 {
			prev = &tokens[i-1]
		}
		switch {
		case t.Is(lexer.Operator, "matches"):
			// 前面不是值、后面紧跟 "(" 才是函数调用形式
			if (prev == nil || !valueEnd(*prev)) && i+1 < len(tokens) && tokens[i+1].Is(lexer.Bracket, "(") {
				edits = append(edits, edit{t.From, t.To, "regexMatches"})
			}
		case t.Is(lexer.Bracket, "("):
			pred := false
			if prev != nil && prev.Kind == lexer.Identifier {
				_, pred = predicateBuiltins[prev.Value]
				if i >= 2 && tokens[i-2].Is(lexer.Operator, ".") {
					pred = false
				}
			}
			stack = append(stack, bracket{open: "(", predCall: pred})
		case t.Is(lexer.Bracket, "["):
			stack = append(stack, bracket{open: "["})
		case t.Is(lexer.Bracket, "{"):
			b := bracket{open: "{"}
			if n := len(stack); n > 0 && stack[n-1].open == "(" && stack[n-1].predCall &&
				prev != nil && (prev.Is(lexer.Bracket, "(") || prev.Is(lexer.Operator, ",")) &&
				i+2 < len(tokens) &&
				(tokens[i+1].Kind == lexer.Identifier || tokens[i+1].Kind == lexer.String) &&
				tokens[i+2].Is(lexer.Operator, ":") {
				b.wrap = true
				edits = append(edits, edit{t.From, t.From, "{ "})
			}
			stack = append(stack, b)
		case t.Is(lexer.Bracket, ")"), t.Is(lexer.Bracket, "]"), t.Is(lexer.Bracket, "}"):
			if n := len(stack); n > 0 {
				top := stack[n-1]
				stack = stack[:n-1]
				if top.wrap && t.Value == "}" {
					edits = append(edits, edit{t.To, t.To, " }"})
				}
			}
		}
	}
	if len(edits) == 0 {
		return src
	}
	sort.SliceStable(edits, func(i, j int) bool { return edits[i].from < edits[j].from })
	runes := []rune(src)
	var sb strings.Builder
	pos := 0
	for _, e := range edits {
		if e.from < pos || e.to > len(runes) {
			return src // 位置异常，放弃改写
		}
		sb.WriteString(string(runes[pos:e.from]))
		sb.WriteString(e.text)
		pos = e.to
	}
	sb.WriteString(string(runes[pos:]))
	return sb.String()
}

// ---------- 自定义函数 ----------

// fnParseJSON parseJSON(s)：把 JSON 字符串解析成对象 / 数组；非法 JSON 返回错误。
// 空值（nil 或空白字符串）返回空对象而不是 nil：平台经常把“没有内容”写成 ""，
// 而 expr 对 nil 取字段会直接报错（cannot fetch x from <nil>），返回空对象才能写 parseJSON(s).a ?? 默认值。
func fnParseJSON(params ...any) (any, error) {
	if len(params) != 1 {
		return nil, fmt.Errorf("parseJSON 需要 1 个参数")
	}
	switch v := params[0].(type) {
	case nil:
		return map[string]any{}, nil
	case string:
		if strings.TrimSpace(v) == "" {
			return map[string]any{}, nil
		}
		return decodeJSONValue([]byte(v))
	case []byte:
		return decodeJSONValue(v)
	default:
		// 已经是结构化数据（平台直接返回了对象），原样返回
		return v, nil
	}
}

// decodeJSONValue 解码任意 JSON，整数保持 int，其余数字为 float64。
func decodeJSONValue(b []byte) (any, error) {
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("parseJSON 失败：%v", err)
	}
	if v == nil { // 字面量 "null" 与空值同样处理，避免后面对 nil 取字段报错
		return map[string]any{}, nil
	}
	return NormalizeJSON(v), nil
}

// NormalizeJSON 把 UseNumber 解出来的 json.Number 转成 int（放得下的整数）或 float64，
// 这样既能参与 expr 运算，又不丢失大整数精度。
func NormalizeJSON(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return int(i)
		}
		f, err := t.Float64()
		if err != nil {
			return t.String()
		}
		return f
	case map[string]any:
		for k, x := range t {
			t[k] = NormalizeJSON(x)
		}
		return t
	case []any:
		for i, x := range t {
			t[i] = NormalizeJSON(x)
		}
		return t
	}
	return v
}

const maxRegexLen = 512

var (
	regexCache     sync.Map
	regexCacheSize atomic.Int64
)

// fnMatches matches(s, pattern)：RE2 正则（线性时间，无回溯爆炸），s 为 nil 时按空串处理。
func fnMatches(params ...any) (any, error) {
	if len(params) != 2 {
		return nil, fmt.Errorf("matches 需要 2 个参数：字符串与正则")
	}
	pat, ok := params[1].(string)
	if !ok {
		return nil, fmt.Errorf("matches 的正则必须是字符串")
	}
	if len(pat) > maxRegexLen {
		return nil, fmt.Errorf("正则过长（最多 %d 字节）", maxRegexLen)
	}
	var re *regexp.Regexp
	if v, ok := regexCache.Load(pat); ok {
		re = v.(*regexp.Regexp)
	} else {
		var err error
		re, err = regexp.Compile(pat)
		if err != nil {
			return nil, fmt.Errorf("正则不合法：%v", err)
		}
		if regexCacheSize.Load() < 1024 {
			regexCache.Store(pat, re)
			regexCacheSize.Add(1)
		}
	}
	return re.MatchString(stringify(params[0])), nil
}

// fnCoalesce coalesce(a, b, ...)：返回第一个非 nil 且不是空字符串的值；都没有则返回 nil。
func fnCoalesce(params ...any) (any, error) {
	for _, p := range params {
		if p == nil {
			continue
		}
		if s, ok := p.(string); ok && s == "" {
			continue
		}
		return p, nil
	}
	return nil, nil
}

// fnToString toString(v)：nil 得到空串；数字不带科学计数法；对象 / 数组得到 JSON 文本。
func fnToString(params ...any) (any, error) {
	if len(params) != 1 {
		return nil, fmt.Errorf("toString 需要 1 个参数")
	}
	return stringify(params[0]), nil
}

// fnToInt toInt(v)：数字截断取整，数字字符串解析后取整，布尔 true=1；无法转换返回错误。nil 得到 0。
func fnToInt(params ...any) (any, error) {
	if len(params) != 1 {
		return nil, fmt.Errorf("toInt 需要 1 个参数")
	}
	switch v := params[0].(type) {
	case nil:
		return 0, nil
	case bool:
		if v {
			return 1, nil
		}
		return 0, nil
	case string:
		s := strings.TrimSpace(v)
		if i, err := strconv.ParseInt(s, 10, 64); err == nil {
			return int(i), nil
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("toInt 无法转换 %q", v)
		}
		return int(f), nil
	}
	if f, ok := toFloat(params[0]); ok {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, fmt.Errorf("toInt 无法转换 %v", f)
		}
		return int(f), nil
	}
	return nil, fmt.Errorf("toInt 无法转换 %T", params[0])
}

// toFloat 把各种数字类型统一成 float64。
func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int8:
		return float64(t), true
	case int16:
		return float64(t), true
	case int32:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint:
		return float64(t), true
	case uint8:
		return float64(t), true
	case uint16:
		return float64(t), true
	case uint32:
		return float64(t), true
	case uint64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	}
	return 0, false
}

// Stringify 把表达式结果转成文本，用于字符串插值与 toString：
// nil -> ""；float64 整数不带小数点和科学计数法；对象 / 数组 -> JSON。
func Stringify(v any) string { return stringify(v) }

func stringify(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(t), 'f', -1, 32)
	case json.Number:
		return t.String()
	case fmt.Stringer:
		return t.String()
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(rv.Int(), 10)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(rv.Uint(), 10)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

// ---------- 模板 ----------

// segment 是模板字符串切出来的一段：字面量或 ${ } 表达式。
type segment struct {
	text   string // 字面量文本，或表达式源码
	isExpr bool
}

// parseTemplate 把 "前缀-${ expr }-后缀" 切成段。"$${" 表示字面量 "${"。
// 表达式内部的花括号（对象字面量）和字符串里的 "}" 都能正确跳过。
func parseTemplate(s string) ([]segment, error) {
	var segs []segment
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			segs = append(segs, segment{text: lit.String()})
			lit.Reset()
		}
	}
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], "$${") {
			lit.WriteString("${")
			i += 3
			continue
		}
		if !strings.HasPrefix(s[i:], "${") {
			lit.WriteByte(s[i])
			i++
			continue
		}
		end, err := findExprEnd(s, i+2)
		if err != nil {
			return nil, err
		}
		src := strings.TrimSpace(s[i+2 : end])
		if src == "" {
			return nil, fmt.Errorf("${ } 里的表达式为空")
		}
		flush()
		segs = append(segs, segment{text: src, isExpr: true})
		i = end + 1
	}
	flush()
	return segs, nil
}

// findExprEnd 从 start 开始找与 "${" 配对的 "}"，返回它的下标。
func findExprEnd(s string, start int) (int, error) {
	depth := 0
	for i := start; i < len(s); i++ {
		switch c := s[i]; c {
		case '\'', '"', '`':
			j := i + 1
			for ; j < len(s); j++ {
				if s[j] == '\\' && c != '`' {
					j++
					continue
				}
				if s[j] == c {
					break
				}
			}
			if j >= len(s) {
				return 0, fmt.Errorf("表达式里的字符串缺少结束引号")
			}
			i = j
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return i, nil
			}
			depth--
		}
	}
	return 0, fmt.Errorf("表达式缺少结束的 }")
}

// ---------- 求值 ----------

// toEnv 把 RenderContext 转成表达式运行环境。
func (rc *RenderContext) toEnv() exprEnv {
	if rc == nil {
		return exprEnv{}
	}
	// resp / req 缺失（响应不是 JSON、还没进入响应阶段）时给空对象，outputs 缺失时给空数组，
	// 这样 resp.xxx ?? 默认值 和 filter(outputs, ...) 不会因为 nil 而报错。
	resp, req, outputs := rc.Resp, rc.Req, rc.Outputs
	if resp == nil {
		resp = map[string]any{}
	}
	if req == nil {
		req = map[string]any{}
	}
	if outputs == nil {
		outputs = []any{}
	}
	return exprEnv{
		Input: rc.Input, Files: rc.Files, Model: rc.Model, Task: rc.Task, Ctx: rc.Ctx,
		Resp: resp, Status: rc.Status, Req: req, Outputs: outputs, Upload: rc.Upload,
	}
}

// runExpr 编译（带缓存）并求值；panic 转成错误，保证引擎不会被表达式打崩。
func runExpr(src string, rc *RenderContext) (result any, err error) {
	ce, err := compileExpr(src)
	if err != nil {
		return nil, fmt.Errorf("表达式 %q 编译失败：%w", src, err)
	}
	defer func() {
		if r := recover(); r != nil {
			result, err = nil, fmt.Errorf("表达式 %q 求值异常：%v", src, r)
		}
	}()
	out, err := vm.Run(ce.program, rc.toEnv())
	if err != nil {
		return nil, fmt.Errorf("表达式 %q 求值失败：%s", src, cleanExprError(err))
	}
	return out, nil
}

func evalExpr(src string, rc *RenderContext) (any, error) { return runExpr(src, rc) }

func evalBool(src string, rc *RenderContext) (bool, error) {
	v, err := runExpr(src, rc)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("表达式 %q 的结果必须是 bool，实际是 %T", src, v)
	}
	return b, nil
}

// renderString 渲染一个字符串模板。
func renderString(s string, rc *RenderContext) (any, error) {
	if !strings.Contains(s, "${") {
		return s, nil
	}
	segs, err := parseTemplate(s)
	if err != nil {
		return nil, err
	}
	// 整体就是一个 ${ expr }：保留结果类型
	if len(segs) == 1 && segs[0].isExpr {
		return runExpr(segs[0].text, rc)
	}
	var sb strings.Builder
	for _, sg := range segs {
		if !sg.isExpr {
			sb.WriteString(sg.text)
			continue
		}
		v, err := runExpr(sg.text, rc)
		if err != nil {
			return nil, err
		}
		sb.WriteString(stringify(v))
	}
	return sb.String(), nil
}

// renderValue 递归渲染 map / slice / 字符串；不修改入参。
func renderValue(tpl any, rc *RenderContext) (any, error) {
	switch t := tpl.(type) {
	case string:
		return renderString(t, rc)
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, v := range t {
			r, err := renderValue(v, rc)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = r
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, v := range t {
			r, err := renderValue(v, rc)
			if err != nil {
				return nil, fmt.Errorf("[%d]: %w", i, err)
			}
			out[i] = r
		}
		return out, nil
	case map[string]string:
		out := make(map[string]any, len(t))
		for k, v := range t {
			r, err := renderString(v, rc)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", k, err)
			}
			out[k] = r
		}
		return out, nil
	}
	return tpl, nil
}

// walkTemplate 遍历模板里所有字符串叶子节点，path 是精确到叶子的 JSON 路径。
func walkTemplate(path string, tpl any, fn func(path, s string)) {
	switch t := tpl.(type) {
	case string:
		fn(path, t)
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walkTemplate(joinPath(path, k), t[k], fn)
		}
	case []any:
		for i, v := range t {
			walkTemplate(fmt.Sprintf("%s[%d]", path, i), v, fn)
		}
	}
}

// joinPath 拼接 JSON 路径。
func joinPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

// ref 是一处静态引用：Path 是引用所在的 JSON 路径，Name 是被引用的 input / files 属性名。
type ref struct {
	Path string
	Name string
}

// templateRefs 是模板 / 表达式里静态引用到的 input 与 files 属性（input.xxx / files.xxx）。
type templateRefs struct {
	input, files []ref
}

// checkTemplate 编译模板里所有表达式并检查变量；path 是模板所在的路径。
// 发现的问题追加到 issues，静态引用追加到 refs（refs 可为 nil）。
func checkTemplate(path string, tpl any, stage exprStage, issues *[]Issue, refs *templateRefs) {
	walkTemplate(path, tpl, func(p, s string) {
		if !strings.Contains(s, "${") {
			return
		}
		segs, err := parseTemplate(s)
		if err != nil {
			*issues = append(*issues, Issue{Path: p, Message: err.Error()})
			return
		}
		for _, sg := range segs {
			if sg.isExpr {
				checkExprSource(p, sg.text, stage, issues, refs)
			}
		}
	})
}

// checkExprSource 编译一段纯表达式源码并检查变量。
func checkExprSource(path, src string, stage exprStage, issues *[]Issue, refs *templateRefs) {
	ce, err := compileExpr(src)
	if err != nil {
		*issues = append(*issues, Issue{Path: path, Message: "表达式错误：" + err.Error()})
		return
	}
	if err := ce.checkStage(stage); err != nil {
		*issues = append(*issues, Issue{Path: path, Message: err.Error()})
		return
	}
	if refs != nil {
		for _, n := range ce.inputRefs {
			refs.input = append(refs.input, ref{path, n})
		}
		for _, n := range ce.fileRefs {
			refs.files = append(refs.files, ref{path, n})
		}
	}
}

// RenderStringEscaped 渲染字符串模板并返回字符串：${ } 表达式的结果先转成文本再交给 escape 处理，
// 字面量部分原样保留。用于拼接 URL 路径——表达式产生的内容必须转义（例如 url.PathEscape），
// 否则用户输入里的 "/"、"?"、".." 会改变请求的目标。escape 为 nil 时不转义。
func RenderStringEscaped(tpl string, rc *RenderContext, escape func(string) string) (string, error) {
	if !strings.Contains(tpl, "${") {
		return tpl, nil
	}
	segs, err := parseTemplate(tpl)
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, sg := range segs {
		if !sg.isExpr {
			sb.WriteString(sg.text)
			continue
		}
		v, err := runExpr(sg.text, rc)
		if err != nil {
			return "", err
		}
		s := stringify(v)
		if escape != nil {
			s = escape(s)
		}
		sb.WriteString(s)
	}
	return sb.String(), nil
}
