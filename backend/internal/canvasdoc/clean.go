package canvasdoc

import "fmt"

// foreignKeys 是合法导出文件里任何层级都不会出现的原环境字段：Build 把它们删掉或改名，
// 并用 checkNoLeak 保证改写后一个不剩。url 不在其中，原因见 CheckDocumentClean。
var foreignKeys = map[string]bool{
	"assetId": true, "src": true, "taskId": true, "pendingTaskIds": true,
	"errorTaskRef": true, "characterId": true, "activeOutputId": true, "labelCache": true,
}

// CheckDocumentClean 扫描导入文档的节点和连线（任意层级，包括 data.params、data.scene 这类自由区域），
// 发现原环境字段就返回 ErrInvalid。在 Validate 之后、Restore 之前调用。
//
// 为什么扫输入而不是扫 Restore 的输出：Restore 会合法地写回 assetId、src、outputs[].id、
// scene.thumb、characterId 等字段，扫输出会把每份正常文件都拒掉；而 Build 产出的文件里这些字段
// 在任何层级都不存在（Build 的兜底扫描同样覆盖 params 和 scene），所以输入里出现一个就只能是手改塞进来的。
// Restore 只清理 data 顶层，嵌套在未知对象里的旧 ID 会原样落进新画布，这里是纵深防御的第二道。
//
// 放过 url：它只是访问地址，不是可以拿来指代原账号素材、任务或角色卡的身份引用，
// 而且 Restore 会重建它的已知位置（paramAssets.url、scene.thumb.url）。
func CheckDocumentClean(doc *Document) error {
	if doc == nil {
		return fmt.Errorf("%w: 文档为空", ErrInvalid)
	}
	for _, list := range [][]map[string]any{doc.Canvas.Nodes, doc.Canvas.Edges} {
		for _, item := range list {
			if key, found := findKey(item, foreignKeys); found {
				id, _ := item["id"].(string)
				return fmt.Errorf("%w: %q 里含有不该出现在导出文件里的字段 %q", ErrInvalid, id, key)
			}
		}
	}
	return nil
}
