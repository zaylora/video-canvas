import { useState } from "react";

import type { MCPProjectItem } from "@/api/admin/mcp-project/type";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/admin-ui/dialog";
import { FormField } from "@/components/admin-ui/form-field";
import { DialogSession } from "@/components/dialog-session";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

import { ScopeChecklist } from "./scope-fields";
import { emptyProjectForm, type MCPProjectsState } from "./use-mcp-projects";

/** 弹窗打开的对象：null 表示关闭；kind 决定是新建还是改已有项目。 */
export type ProjectDialogTarget = { kind: "new" } | { kind: "edit"; item: MCPProjectItem };

/**
 * 接入项目弹窗：新建与编辑共用一个表单，和后台其他配置页一致（列表页「新建」按钮 + 弹窗）。
 *
 * 新建时只填名称与能力：归属就是当前登录用户，默认画布留空——那两项不是新建时该做的决定。
 * 编辑时多给一个「默认画布」，因为它是「配一次、以后每次调用都用」的东西。
 *
 * 弹窗常驻挂载，正文交给 DialogSession：关闭后留着播退出动画，每次打开从头开始（规范 6.3）。
 *
 * @param target 打开的对象，null 表示关闭
 * @param state 页面数据层，保存走 state.create / state.update
 * @param onClose 关闭（保存成功也走这里）
 */
export function ProjectDialog({
  target,
  state,
  onClose,
}: {
  target: ProjectDialogTarget | null;
  state: MCPProjectsState;
  onClose: () => void;
}) {
  const open = target !== null;
  // 关闭时 target 变 null，正文要留到退出动画播完，所以记住最后一次打开的对象
  const [shown, setShown] = useState<ProjectDialogTarget | null>(target);
  if (target && target !== shown) setShown(target);
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="flex max-h-[min(88svh,760px)] flex-col sm:max-w-lg">
        <DialogSession open={open}>
          {shown && <ProjectForm target={shown} state={state} onClose={onClose} />}
        </DialogSession>
      </DialogContent>
    </Dialog>
  );
}

/** 弹窗正文：标题、字段和底部按钮；字段区自己滚动，别把弹窗撑出屏幕。 */
function ProjectForm({
  target,
  state,
  onClose,
}: {
  target: ProjectDialogTarget;
  state: MCPProjectsState;
  onClose: () => void;
}) {
  const editing = target.kind === "edit" ? target.item : null;
  const [form, setForm] = useState(() =>
    editing
      ? {
          name: editing.name,
          ownerUserId: String(editing.owner_user_id ?? ""),
          canvasCode: editing.canvas_code ?? "",
          scopes: editing.scopes,
        }
      : emptyProjectForm(),
  );

  const submit = async () => {
    const ok = editing ? await state.update(editing, form) : await state.create(form);
    if (ok) onClose();
  };

  return (
    <>
      <DialogHeader>
        <DialogTitle>{editing ? `编辑「${editing.name}」` : "新建接入项目"}</DialogTitle>
        <DialogDescription>
          {editing
            ? "改完立刻生效，已签发的令牌不用换。"
            : "归属就是你本人；默认画布留空，调用时自己带。"}
        </DialogDescription>
      </DialogHeader>
      <form
        noValidate
        className="flex min-h-0 flex-1 flex-col gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          void submit();
        }}
      >
        <div className="grid min-h-0 flex-1 gap-4 overflow-y-auto">
          <FormField label="名称" htmlFor="mcp-project-name">
            <Input
              id="mcp-project-name"
              value={form.name}
              maxLength={64}
              placeholder="如：本机 Codex"
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
          </FormField>
          {editing && (
            <FormField
              label="默认画布"
              htmlFor="mcp-project-canvas"
              hint="可留空；填了之后外部调用没指定画布时就用它。只能填自己名下的画布"
            >
              <Input
                id="mcp-project-canvas"
                value={form.canvasCode}
                maxLength={32}
                spellCheck={false}
                className="font-mono"
                placeholder="画布地址末尾那串 32 位十六进制，可空"
                onChange={(e) => setForm({ ...form, canvasCode: e.target.value })}
              />
            </FormField>
          )}
          <ScopeChecklist
            scopes={form.scopes}
            onChange={(scopes) => setForm({ ...form, scopes })}
          />
        </div>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" disabled={state.busy}>
            {editing ? "保存" : "创建"}
          </Button>
        </DialogFooter>
      </form>
    </>
  );
}
