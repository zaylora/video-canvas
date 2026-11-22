package canvasgraph_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"video-canvas/internal/agent/canvasgraph"
)

// storyboardPayload 是一张带分镜表节点的画布：剧本连着分镜表，分镜表里有三个镜头。
const storyboardPayload = `{
  "nodes": [
    {"id":"n_txt","type":"canvas","position":{"x":0,"y":0},
     "data":{"kind":"script","label":"剧本","text":"雨夜，林晚推门进屋。"}},
    {"id":"n_sb","type":"canvas","position":{"x":700,"y":0},
     "data":{"kind":"storyboard","label":"分镜表","storyboard":{"shots":[
       {"id":"a","scene":"推门进屋","shot":"全景","camera":"推","action":"","dialogue":"","duration":4.5},
       {"id":"b","scene":"看向窗外","shot":"中景","camera":"固定","action":"","dialogue":"OS：又是雨天。","duration":2},
       {"id":"c","scene":"雨打玻璃","shot":"特写","camera":"固定","action":"","dialogue":"","duration":3}
     ]}}}
  ],
  "edges": [{"id":"e_1","source":"n_txt","target":"n_sb"}]
}`

// shot 是测试里读出来的一行镜头。
type shot struct {
	ID          string  `json:"id"`
	Scene       string  `json:"scene"`
	Shot        string  `json:"shot"`
	Camera      string  `json:"camera"`
	Action      string  `json:"action"`
	Dialogue    string  `json:"dialogue"`
	Duration    float64 `json:"duration"`
	ImageNodeID string  `json:"imageNodeId"`
	VideoNodeID string  `json:"videoNodeId"`
}

// shotsOf 读出某个分镜表节点的镜头。
func shotsOf(t *testing.T, g *canvasgraph.Graph, id string) []shot {
	t.Helper()
	n := g.Node(id)
	if n == nil {
		t.Fatalf("节点 %s 不存在", id)
	}
	raw, _ := json.Marshal(n.Data()["storyboard"])
	var sb struct {
		Shots []shot `json:"shots"`
	}
	if err := json.Unmarshal(raw, &sb); err != nil {
		t.Fatalf("storyboard 结构不对：%s", raw)
	}
	return sb.Shots
}

// scenes 取镜头的画面描述，按镜号顺序。
func scenes(list []shot) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Scene
	}
	return out
}

// applyErr 应用 ops 并期望失败，返回错误文字。
func applyErr(t *testing.T, payload, ops string) string {
	t.Helper()
	_, err := canvasgraph.Apply(mustParse(t, payload), mustOps(t, ops), opts())
	if err == nil {
		t.Fatalf("应该失败：%s", ops)
	}
	return err.Error()
}

// TestStoryboardNodeSurvivesRoundTrip 分镜表节点读得进来、写回去时镜头原样保留。
func TestStoryboardNodeSurvivesRoundTrip(t *testing.T) {
	g := mustParse(t, storyboardPayload)
	res := mustApply(t, g, `[{"op":"create_node","tempId":"a","kind":"script"}]`)
	out, err := res.Graph.Marshal()
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{`"scene":"推门进屋"`, `"dialogue":"OS：又是雨天。"`, `"duration":4.5`} {
		if !strings.Contains(string(out), want) {
			t.Errorf("分镜表的镜头丢了 %s：%s", want, out)
		}
	}
}

// TestAgentCreatesStoryboardFromScript Agent 新建分镜表、写入镜头并从剧本连线过来；没给的字段取默认值，镜头带新 id。
func TestAgentCreatesStoryboardFromScript(t *testing.T) {
	g := mustParse(t, storyboardPayload)
	res := mustApply(t, g, `[
	  {"op":"create_node","tempId":"sb","kind":"storyboard","label":"第一场分镜","storyboard":{"shots":[
	    {"scene":"月下瓜地","shot":"远景","camera":"摇","duration":5},
	    {"scene":"闰土举叉"}
	  ]}},
	  {"op":"connect","source":"n_txt","target":"sb"}
	]`)
	id := res.IDMap["sb"]
	n := res.Graph.Node(id)
	if n.Kind() != "storyboard" || n.Label() != "第一场分镜" {
		t.Fatalf("新节点不对：%v", n.Data())
	}
	list := shotsOf(t, res.Graph, id)
	if len(list) != 2 {
		t.Fatalf("应有 2 个镜头，得到 %d", len(list))
	}
	if list[0].Shot != "远景" || list[0].Camera != "摇" || list[0].Duration != 5 {
		t.Errorf("第 1 镜字段不对：%+v", list[0])
	}
	if list[1].Shot != "中景" || list[1].Camera != "固定" || list[1].Duration != 3 {
		t.Errorf("没给的字段应取默认中景 / 固定 / 3 秒：%+v", list[1])
	}
	if list[0].ID == "" || list[0].ID == list[1].ID {
		t.Errorf("每个镜头要有不同的 id：%+v", list)
	}
	connected := false
	for _, e := range res.Graph.Edges {
		connected = connected || e.Source() == "n_txt" && e.Target() == id
	}
	if !connected {
		t.Error("剧本应连到新分镜表")
	}
}

// TestAgentCreatesEmptyStoryboard 不写 storyboard 时建一个空表。
func TestAgentCreatesEmptyStoryboard(t *testing.T) {
	res := mustApply(t, mustParse(t, `{}`), `[{"op":"create_node","tempId":"sb","kind":"storyboard"}]`)
	n := res.Graph.Node(res.IDMap["sb"])
	if n.Label() != "分镜表" || len(shotsOf(t, res.Graph, n.ID())) != 0 {
		t.Fatalf("应建一个叫「分镜表」的空表：%v", n.Data())
	}
}

// TestAgentEditsStoryboardRowsByShotNumber 按镜号改、插、删、挪；每一步的镜号按上一步之后的顺序算。
func TestAgentEditsStoryboardRowsByShotNumber(t *testing.T) {
	g := mustParse(t, storyboardPayload)
	res := mustApply(t, g, `[{"op":"update_node","id":"n_sb","storyboard":{"edits":[
	  {"op":"update","no":2,"set":{"shot":"特写","camera":"推"}},
	  {"op":"insert","after":0,"set":{"scene":"片头黑场","duration":1.5}},
	  {"op":"delete","no":4},
	  {"op":"move","no":1,"to":3}
	]}}]`)
	list := shotsOf(t, res.Graph, "n_sb")
	if got := scenes(list); strings.Join(got, ",") != "推门进屋,看向窗外,片头黑场" {
		t.Fatalf("顺序不对：%v", got)
	}
	if list[1].ID != "b" || list[1].Shot != "特写" || list[1].Camera != "推" || list[1].Dialogue != "OS：又是雨天。" {
		t.Errorf("改第 2 镜只动给了的字段：%+v", list[1])
	}
	if list[0].ID != "a" || list[0].Duration != 4.5 {
		t.Errorf("没改的镜头原样保留：%+v", list[0])
	}
}

// TestAgentReplacesStoryboardShots shots 整表替换；和 edits 一起给时先替换再改。
func TestAgentReplacesStoryboardShots(t *testing.T) {
	g := mustParse(t, storyboardPayload)
	res := mustApply(t, g, `[{"op":"update_node","id":"n_sb","storyboard":{
	  "shots":[{"scene":"一"},{"scene":"二"}],
	  "edits":[{"op":"update","no":2,"set":{"dialogue":"台词"}}]
	}}]`)
	list := shotsOf(t, res.Graph, "n_sb")
	if strings.Join(scenes(list), ",") != "一,二" || list[1].Dialogue != "台词" {
		t.Fatalf("整表替换后再改不对：%+v", list)
	}
}

// TestStoryboardDurationIsClampedWithWarning 时长越界修正到 1–60 秒、保留 1 位小数，并给出提醒。
func TestStoryboardDurationIsClampedWithWarning(t *testing.T) {
	g := mustParse(t, storyboardPayload)
	res := mustApply(t, g, `[{"op":"update_node","id":"n_sb","storyboard":{"edits":[
	  {"op":"update","no":1,"set":{"duration":90}},
	  {"op":"update","no":2,"set":{"duration":2.46}}
	]}}]`)
	list := shotsOf(t, res.Graph, "n_sb")
	if list[0].Duration != 60 || list[1].Duration != 2.5 {
		t.Errorf("时长没修正：%v %v", list[0].Duration, list[1].Duration)
	}
	if !strings.Contains(strings.Join(res.Warnings, "；"), "1–60") {
		t.Errorf("越界要提醒：%v", res.Warnings)
	}
}

// TestStoryboardRejectsInvalidInput 枚举外的景别运镜、空画面、镜号越界、未知操作都拒绝，并说清原因。
func TestStoryboardRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name, ops, want string
	}{
		{"景别不在枚举里", `{"edits":[{"op":"update","no":1,"set":{"shot":"超大特写"}}]}`, "大远景"},
		{"运镜不在枚举里", `{"edits":[{"op":"update","no":1,"set":{"camera":"飞"}}]}`, "环绕"},
		{"新镜头没写画面", `{"edits":[{"op":"insert","after":1,"set":{"shot":"近景"}}]}`, "画面"},
		{"整表里有空画面", `{"shots":[{"scene":" "}]}`, "画面"},
		{"画面不能改成空", `{"edits":[{"op":"update","no":1,"set":{"scene":""}}]}`, "画面"},
		{"镜号越界", `{"edits":[{"op":"delete","no":9}]}`, "第 9 镜"},
		{"插入位置越界", `{"edits":[{"op":"insert","after":7,"set":{"scene":"x"}}]}`, "after"},
		{"未知操作", `{"edits":[{"op":"split","no":1}]}`, "update"},
		{"update 没给字段", `{"edits":[{"op":"update","no":1}]}`, "set"},
		{"空的 storyboard", `{}`, "shots"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			msg := applyErr(t, storyboardPayload, `[{"op":"update_node","id":"n_sb","storyboard":`+c.ops+`}]`)
			if !strings.Contains(msg, c.want) {
				t.Errorf("错误信息应提到 %q：%s", c.want, msg)
			}
		})
	}
}

// TestStoryboardOnlyAcceptsItsOwnFields 分镜表节点只能写标题、位置和镜头；storyboard 也只能写在分镜表上。
func TestStoryboardOnlyAcceptsItsOwnFields(t *testing.T) {
	for _, ops := range []string{
		`[{"op":"update_node","id":"n_sb","prompt":"x"}]`,
		`[{"op":"update_node","id":"n_sb","model":"m"}]`,
		`[{"op":"create_node","tempId":"s","kind":"storyboard","prompt":"x"}]`,
	} {
		if msg := applyErr(t, storyboardPayload, ops); !strings.Contains(msg, "分镜表") {
			t.Errorf("%s 应拒绝并提到分镜表：%s", ops, msg)
		}
	}
	if msg := applyErr(t, storyboardPayload, `[{"op":"update_node","id":"n_txt","storyboard":{"shots":[]}}]`); !strings.Contains(msg, "storyboard") {
		t.Errorf("storyboard 写在文本节点上应拒绝：%s", msg)
	}
	res := mustApply(t, mustParse(t, storyboardPayload), `[{"op":"update_node","id":"n_sb","label":"第二场"}]`)
	if res.Graph.Node("n_sb").Label() != "第二场" {
		t.Error("分镜表可以改标题")
	}
}

// TestStoryboardLinkRules 只有文本能连进分镜表；分镜表能连到图片、视频节点，别的连不出去。
func TestStoryboardLinkRules(t *testing.T) {
	for _, ops := range []string{
		// 进：图片、视频、音频都不能连进分镜表
		`[{"op":"create_node","tempId":"i","kind":"image"},{"op":"connect","source":"i","target":"n_sb"}]`,
		`[{"op":"create_node","tempId":"v","kind":"video"},{"op":"connect","source":"v","target":"n_sb"}]`,
		// 出：音频、文本、分镜表自己连不上
		`[{"op":"create_node","tempId":"a","kind":"audio"},{"op":"connect","source":"n_sb","target":"a"}]`,
		`[{"op":"create_node","tempId":"s","kind":"script"},{"op":"connect","source":"n_sb","target":"s"}]`,
		`[{"op":"create_node","tempId":"b","kind":"storyboard"},{"op":"connect","source":"n_sb","target":"b"}]`,
	} {
		applyErr(t, storyboardPayload, ops)
	}
	g := mustParse(t, storyboardPayload)
	for _, ops := range []string{
		`[{"op":"create_node","tempId":"s","kind":"script"},{"op":"connect","source":"s","target":"n_sb"}]`,
		`[{"op":"create_node","tempId":"i","kind":"image"},{"op":"connect","source":"n_sb","target":"i"}]`,
		`[{"op":"create_node","tempId":"v","kind":"video"},{"op":"connect","source":"n_sb","target":"v"}]`,
	} {
		mustApply(t, g, ops)
	}
}

// TestStoryboardLinkIsProvenanceOnly 分镜表连到图片节点的线只表示来源：下游生成时不读分镜表的内容，
// 既没有它的文字，也不会把它当作还没有产物的参考素材。
func TestStoryboardLinkIsProvenanceOnly(t *testing.T) {
	g := mustParse(t, storyboardPayload)
	res := mustApply(t, g, `[
	  {"op":"create_node","tempId":"i","kind":"image","prompt":"一只猫"},
	  {"op":"connect","source":"n_sb","target":"i"}
	]`)
	src, err := canvasgraph.Generation(res.Graph, res.IDMap["i"])
	if err != nil {
		t.Fatalf("Generation: %v", err)
	}
	if len(src.Texts) != 0 || len(src.Assets) != 0 || src.Pending != 0 {
		t.Fatalf("分镜表不该给下游任何文字或素材：%+v", src)
	}
	if got := canvasgraph.ComposePrompt(src, nil); got != "一只猫" {
		t.Errorf("下游提示词应只有自己的：%q", got)
	}
}

// TestStoryboardCannotGenerate 分镜表不调模型，不能申请生成。
func TestStoryboardCannotGenerate(t *testing.T) {
	g := mustParse(t, storyboardPayload)
	if _, err := canvasgraph.Generation(g, "n_sb"); err == nil {
		t.Fatal("分镜表不能申请生成")
	}
}

// TestStoryboardEditProducesUndoableChange 改镜头产生 data.storyboard 的差异，撤销本轮能恢复。
func TestStoryboardEditProducesUndoableChange(t *testing.T) {
	g := mustParse(t, storyboardPayload)
	res := mustApply(t, g, `[{"op":"update_node","id":"n_sb","storyboard":{"edits":[{"op":"delete","no":1}]}}]`)
	var found bool
	for _, c := range res.Changes {
		if c.ID == "n_sb" && c.Op == canvasgraph.ChangeUpdate {
			_, found = c.After["data.storyboard"]
		}
	}
	if !found {
		t.Fatalf("应有 data.storyboard 的改动：%+v", res.Changes)
	}
	back := canvasgraph.Revert(res.Graph, res.Changes)
	if len(shotsOf(t, back.Graph, "n_sb")) != 3 {
		t.Error("撤销后应恢复 3 个镜头")
	}
}

// TestStoryboardCatalogSummary 目录里分镜表的摘要是「N 镜」加首镜画面。
func TestStoryboardCatalogSummary(t *testing.T) {
	c := canvasgraph.BuildCatalog(mustParse(t, storyboardPayload), canvasgraph.CatalogOptions{})
	for _, row := range c.Nodes {
		if row.ID == "n_sb" && !strings.HasPrefix(row.Prompt, "3 镜") {
			t.Errorf("摘要应以「3 镜」开头：%q", row.Prompt)
		}
	}
}

// TestStoryboardEnumsMatchFrontend 景别、运镜的枚举和前端 constants/storyboard.ts 是同一份。
func TestStoryboardEnumsMatchFrontend(t *testing.T) {
	src, err := os.ReadFile("../../../../../web/src/constants/storyboard.ts")
	if err != nil {
		t.Fatalf("读前端枚举：%v", err)
	}
	list := func(name string) []string {
		m := regexp.MustCompile(`(?s)export const ` + name + ` = \[(.*?)\] as const`).FindSubmatch(src)
		if m == nil {
			t.Fatalf("前端找不到 %s", name)
		}
		var out []string
		for _, q := range regexp.MustCompile(`"([^"]+)"`).FindAllSubmatch(m[1], -1) {
			out = append(out, string(q[1]))
		}
		return out
	}
	if got, want := canvasgraph.ShotSizes, list("SHOT_SIZES"); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("景别不一致：后端 %v，前端 %v", got, want)
	}
	if got, want := canvasgraph.CameraMoves, list("CAMERA_MOVES"); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("运镜不一致：后端 %v，前端 %v", got, want)
	}
}

// TestStoryboardDetailHidesAssetsFromAgent 手传分镜图的素材 id 和地址属于用户的私有资源，不给模型看；
// 模型能看到这一行有没有图，也能看到关联的图片、视频节点 id（要改提示词、连线都得靠它）。
func TestStoryboardDetailHidesAssetsFromAgent(t *testing.T) {
	g := mustParse(t, `{"nodes":[{"id":"n_sb","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"storyboard","label":"分镜表","storyboard":{"shots":[
	  {"id":"a","scene":"推门","shot":"全景","camera":"推","action":"","dialogue":"","duration":3,"image":{"assetId":"90451","src":"/files/u1/a.png"}},
	  {"id":"b","scene":"看窗","shot":"中景","camera":"固定","action":"","dialogue":"","duration":3,"imageNodeId":"n_img","videoNodeId":"n_vid"}
	]}}}],"edges":[]}`)
	res, err := canvasgraph.Detail(g, []string{"n_sb"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(res.Nodes[0].Data)
	for _, leak := range []string{"90451", "/files/u1/a.png", "assetId", `"src"`} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("详情里不该出现 %q：%s", leak, raw)
		}
	}
	for _, want := range []string{`"hasImage":true`, "n_img", "n_vid"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("详情里应该有 %q：%s", want, raw)
		}
	}
}

// TestAgentEditsKeepShotLinksAndImages Agent 改镜头文字时，行上的手传图和关联节点 id 原样保留，不会被抹掉。
func TestAgentEditsKeepShotLinksAndImages(t *testing.T) {
	g := mustParse(t, `{"nodes":[{"id":"n_sb","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"storyboard","label":"分镜表","storyboard":{"shots":[
	  {"id":"a","scene":"推门","shot":"全景","camera":"推","action":"","dialogue":"","duration":3,"image":{"assetId":"90451","src":"/files/u1/a.png"},"imageNodeId":"n_img","videoNodeId":"n_vid"}
	]}}}],"edges":[]}`)
	res := mustApply(t, g, `[{"op":"update_node","id":"n_sb","storyboard":{"edits":[{"op":"update","no":1,"set":{"shot":"特写"}}]}}]`)
	raw, _ := json.Marshal(res.Graph.Node("n_sb").Data()["storyboard"])
	for _, want := range []string{`"assetId":"90451"`, `"src":"/files/u1/a.png"`, `"imageNodeId":"n_img"`, `"videoNodeId":"n_vid"`, `"shot":"特写"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("改文字后应保留 %q：%s", want, raw)
		}
	}
}

// TestAgentDeleteAndMoveKeepOtherShotsLinks 删一行、挪一行，别的行的关联和手传图不受影响。
func TestAgentDeleteAndMoveKeepOtherShotsLinks(t *testing.T) {
	g := mustParse(t, `{"nodes":[{"id":"n_sb","type":"canvas","position":{"x":0,"y":0},"data":{"kind":"storyboard","label":"分镜表","storyboard":{"shots":[
	  {"id":"a","scene":"一","shot":"中景","camera":"固定","action":"","dialogue":"","duration":3,"imageNodeId":"n_i1"},
	  {"id":"b","scene":"二","shot":"中景","camera":"固定","action":"","dialogue":"","duration":3,"imageNodeId":"n_i2"},
	  {"id":"c","scene":"三","shot":"中景","camera":"固定","action":"","dialogue":"","duration":3,"imageNodeId":"n_i3"}
	]}}}],"edges":[]}`)
	res := mustApply(t, g, `[{"op":"update_node","id":"n_sb","storyboard":{"edits":[{"op":"delete","no":1},{"op":"move","no":2,"to":1}]}}]`)
	raw, _ := json.Marshal(res.Graph.Node("n_sb").Data()["storyboard"])
	var sb struct {
		Shots []struct {
			ID          string `json:"id"`
			ImageNodeID string `json:"imageNodeId"`
		} `json:"shots"`
	}
	if err := json.Unmarshal(raw, &sb); err != nil {
		t.Fatal(err)
	}
	if len(sb.Shots) != 2 || sb.Shots[0].ID != "c" || sb.Shots[0].ImageNodeID != "n_i3" || sb.Shots[1].ID != "b" || sb.Shots[1].ImageNodeID != "n_i2" {
		t.Errorf("挪动后各行应带着自己的关联：%s", raw)
	}
}

// linkPayload 是一张分镜表加画布上已有的素材：v1 已出片、v2 还没出片、i1 是图片。
// 用来验证「分镜表后来才加」「镜头不是从这张表生成」时把已有节点挂进来的路。
const linkPayload = `{
  "nodes": [
    {"id":"n_sb","type":"canvas","position":{"x":0,"y":0},
     "data":{"kind":"storyboard","label":"分镜表","storyboard":{"shots":[
       {"id":"a","scene":"推门进屋","shot":"全景","camera":"推","action":"","dialogue":"","duration":4},
       {"id":"b","scene":"看向窗外","shot":"中景","camera":"固定","action":"","dialogue":"","duration":3}
     ]}}},
    {"id":"v1","type":"canvas","position":{"x":900,"y":0},"data":{"kind":"video","label":"S1","src":"https://x/1.mp4","assetId":"11"}},
    {"id":"v2","type":"canvas","position":{"x":900,"y":400},"data":{"kind":"video","label":"S2"}},
    {"id":"i1","type":"canvas","position":{"x":0,"y":600},"data":{"kind":"image","label":"首帧","src":"https://x/1.png","assetId":"12"}}
  ],
  "edges": []
}`

// TestStoryboardLinksExistingNodes 分镜表能把画布上已有的视频、图片挂到镜头上；传空串取消关联。
func TestStoryboardLinksExistingNodes(t *testing.T) {
	res := mustApply(t, mustParse(t, linkPayload), `[{"op":"update_node","id":"n_sb","storyboard":{"edits":[
	  {"op":"update","no":1,"set":{"videoNodeId":"v1","imageNodeId":"i1"}},
	  {"op":"update","no":2,"set":{"videoNodeId":"v2"}}
	]}}]`)
	list := shotsOf(t, res.Graph, "n_sb")
	if list[0].VideoNodeID != "v1" || list[0].ImageNodeID != "i1" || list[1].VideoNodeID != "v2" {
		t.Fatalf("关联没写进去：%+v", list)
	}

	// 传空串是取消关联，不是报错
	res = mustApply(t, res.Graph, `[{"op":"update_node","id":"n_sb","storyboard":{"edits":[{"op":"update","no":1,"set":{"videoNodeId":""}}]}}]`)
	if got := shotsOf(t, res.Graph, "n_sb")[0].VideoNodeID; got != "" {
		t.Errorf("取消关联后还留着 %q", got)
	}
}

// TestStoryboardLinkRejectsWrongNodes 挂错节点要在写入前拦下来：种类不对、节点不在画布上。
func TestStoryboardLinkRejectsWrongNodes(t *testing.T) {
	for _, c := range []struct{ name, ops, want string }{
		{"种类不对", `[{"op":"update_node","id":"n_sb","storyboard":{"edits":[{"op":"update","no":1,"set":{"videoNodeId":"i1"}}]}}]`, "不能当视频节点"},
		{"节点不存在", `[{"op":"update_node","id":"n_sb","storyboard":{"edits":[{"op":"update","no":1,"set":{"videoNodeId":"nope"}}]}}]`, "不在画布上"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if msg := applyErr(t, linkPayload, c.ops); !strings.Contains(msg, c.want) {
				t.Errorf("错误信息应提到 %q：%s", c.want, msg)
			}
		})
	}
}

// TestStoryboardSummaryCountsLinkedVideos 目录摘要要带上「已出片的视频几段」：模型据此判断能不能直接出成片。
func TestStoryboardSummaryCountsLinkedVideos(t *testing.T) {
	promptOf := func(g *canvasgraph.Graph) string {
		for _, row := range canvasgraph.BuildCatalog(g, canvasgraph.CatalogOptions{PromptRunes: 200}).Nodes {
			if row.ID == "n_sb" {
				return row.Prompt
			}
		}
		t.Fatal("目录里没有分镜表")
		return ""
	}
	if got := promptOf(mustParse(t, linkPayload)); !strings.HasPrefix(got, "2 镜 · 推门进屋") {
		t.Errorf("没挂视频时摘要=%q", got)
	}
	res := mustApply(t, mustParse(t, linkPayload), `[{"op":"update_node","id":"n_sb","storyboard":{"edits":[
	  {"op":"update","no":1,"set":{"videoNodeId":"v1"}},
	  {"op":"update","no":2,"set":{"videoNodeId":"v2"}}
	]}}]`)
	// v2 还没出片，只数 v1
	if got := promptOf(res.Graph); !strings.HasPrefix(got, "2 镜 · 视频 1 段已出片 · 推门进屋") {
		t.Errorf("挂上视频后摘要=%q", got)
	}
}
