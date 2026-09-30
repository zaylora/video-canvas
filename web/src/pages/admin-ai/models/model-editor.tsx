import { useEffect, useImperativeHandle, useRef, useState, type Ref } from "react";
import { Link } from "react-router";
import {
  Braces,
  CheckCheck,
  FlaskConical,
  History,
  Loader2,
  Play,
  Rocket,
  Save,
  Undo2,
} from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Switch } from "@/components/ui/switch";
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { cn } from "@/lib/utils";
import { findPathInJson } from "@/utils/admin/json";

import { Notice, Tag } from "../shared";
import type { AdminCatalog } from "../use-admin";
import { fieldIdForPath, ModelForm } from "./model-form";
import type { EditorMode, ModelWorkspace } from "./use-model-workspace";

/** 编辑器对外暴露的动作：点击问题条目时定位到字段 / JSON 行 */
export type ModelEditorHandle = {
  /** 按校验问题的路径定位 */
  locate: (path: string) => void;
};

/** JSON 文本框的行高（与 leading-5 一致），用来把命中行滚到可见位置 */
const LINE_HEIGHT = 20;

/**
 * 模型页中栏：顶部标题与上下架、表单 / JSON 视图切换、操作按钮，中间是编辑区，底部是示例输入。
 * JSON 是事实来源：表单只是对它的读写；JSON 有语法错误时禁止切回表单并提示行列。
 */
export function ModelEditor({
  ws,
  catalog,
  ref,
}: {
  ws: ModelWorkspace;
  catalog: AdminCatalog;
  ref?: Ref<ModelEditorHandle>;
}) {
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const [pendingLocate, setPendingLocate] = useState<string | null>(null);
  const { mode, parsed, body } = ws;

  useImperativeHandle(ref, () => ({
    locate: (path: string) => {
      const id = fieldIdForPath(path);
      if (mode === "form" && id) {
        const element = document.getElementById(id);
        if (element) {
          element.focus();
          element.scrollIntoView({ block: "center" });
          return;
        }
      }
      // 没有对应表单控件（或本来就在 JSON 视图）：切到 JSON，渲染后再选中命中的行
      if (mode !== "json") ws.switchMode("json");
      setPendingLocate(path);
    },
  }));

  useEffect(() => {
    if (!pendingLocate || mode !== "json") return;
    const textarea = textareaRef.current;
    const hit = findPathInJson(ws.text, pendingLocate);
    setPendingLocate(null);
    if (!textarea || !hit) return;
    textarea.focus();
    textarea.setSelectionRange(hit.index, hit.index + hit.length);
    textarea.scrollTop = Math.max(0, (hit.line - 3) * LINE_HEIGHT);
  }, [pendingLocate, mode, ws.text]);

  if (ws.selection === "none") {
    return (
      <section className="text-muted-foreground grid min-h-96 min-w-0 flex-1 place-items-center p-6 text-sm">
        选择或新建一个模型。
      </section>
    );
  }

  const disabled = ws.working || ws.loadingDetail;
  const published = ws.detail?.published;
  const rollbackable = ws.revisions?.filter((revision) => revision.status !== "draft") ?? [];

  const spinner = (name: string, icon: React.ReactNode) =>
    ws.busy === name ? <Loader2 className="animate-spin" /> : icon;

  return (
    <section className="flex min-h-96 min-w-0 flex-1 flex-col">
      <div className="flex flex-wrap items-center gap-2 border-b p-2">
        <span className="mr-1 text-sm font-medium">
          模型 · <span className="font-mono">{ws.key ?? "（新建，未保存）"}</span>
        </span>
        {ws.dirty && <Tag tone="warning">有未保存修改</Tag>}
        {ws.detail && ws.key && (
          <label className="text-muted-foreground flex items-center gap-1.5 text-xs">
            <Switch
              size="sm"
              checked={!!ws.detail.enabled}
              disabled={disabled || !published}
              aria-label="上下架"
              onCheckedChange={(checked) => void ws.toggleEnabled(checked)}
            />
            {ws.detail.enabled ? "已上架" : "未上架"}
            {!published && <span>（发布后才能上下架）</span>}
          </label>
        )}
        <Tabs
          className="ml-auto"
          value={mode}
          onValueChange={(value) => ws.switchMode(value as EditorMode)}
        >
          <TabsList aria-label="编辑视图">
            <TabsTrigger value="form">表单</TabsTrigger>
            <TabsTrigger value="json">JSON</TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      <div className="flex flex-wrap items-center gap-1.5 border-b p-2">
        {mode === "json" && (
          <Button size="xs" variant="ghost" onClick={ws.format}>
            <Braces />
            格式化
          </Button>
        )}
        <div className="ml-auto flex flex-wrap items-center gap-1.5">
          <Button size="xs" variant="outline" disabled={disabled} onClick={() => void ws.save()}>
            {spinner("save", <Save />)}
            保存草稿
          </Button>
          <Button size="xs" variant="outline" disabled={disabled} onClick={() => void ws.validate()}>
            {spinner("validate", <CheckCheck />)}
            校验
          </Button>
          <Button size="xs" variant="outline" disabled={disabled} onClick={() => void ws.doDryRun()}>
            {spinner("dry-run", <FlaskConical />)}
            dry-run
          </Button>
          <Button size="xs" variant="outline" disabled={disabled} onClick={() => void ws.doTestRun()}>
            {spinner("test-run", <Play />)}
            试跑
          </Button>
          <Button
            size="xs"
            disabled={disabled || ws.isNew || !!ws.publishBlock}
            title={ws.publishBlock ?? undefined}
            onClick={() => void ws.requestPublish()}
          >
            {spinner("publish", <Rocket />)}
            发布
          </Button>
          <DropdownMenu
            modal={false}
            onOpenChange={(open) => {
              if (open) void ws.loadRevisions();
            }}
          >
            <DropdownMenuTrigger
              disabled={disabled || ws.isNew}
              className="border-border bg-background hover:bg-muted inline-flex h-6 items-center gap-1 rounded-[min(var(--radius-md),10px)] border px-2 text-xs font-medium disabled:opacity-50"
            >
              <Undo2 className="size-3" />
              回滚
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-72" sideOffset={6}>
              <DropdownMenuGroup>
                <DropdownMenuLabel>回滚到历史版本</DropdownMenuLabel>
                {ws.revisions === null && <p className="text-muted-foreground px-1.5 py-2 text-xs">加载中…</p>}
                {ws.revisions !== null && rollbackable.length === 0 && (
                  <p className="text-muted-foreground px-1.5 py-2 text-xs">没有可回滚的历史版本</p>
                )}
                {rollbackable.map((revision) => (
                  <DropdownMenuItem key={revision.id} onClick={() => ws.requestRollback(revision)}>
                    <History />
                    <span className="flex min-w-0 flex-1 flex-col">
                      <span>
                        第 {revision.revision_no} 版 · {revision.status === "published" ? "当前发布" : "历史"}
                      </span>
                      <span className="text-muted-foreground truncate text-xs">
                        {new Date(revision.created_at).toLocaleString()}
                        {revision.note ? ` · ${revision.note}` : ""}
                      </span>
                    </span>
                  </DropdownMenuItem>
                ))}
              </DropdownMenuGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
      </div>

      {ws.queue.length > 0 && ws.isNew && (
        <div className="border-b p-2">
          <Notice
            tone="info"
            title={`待处理草稿 ${ws.queue.length} 个`}
            action={
              <div className="flex shrink-0 gap-1.5">
                <Button size="xs" variant="outline" onClick={ws.skipDraft}>
                  跳过当前
                </Button>
                <Button size="xs" variant="ghost" onClick={ws.discardDrafts}>
                  放弃全部
                </Button>
              </div>
            }
          >
            这是从渠道导入的草稿，<b>不会自动上架</b>。逐个确认、保存草稿、试跑，再发布。
          </Notice>
        </div>
      )}

      {ws.publishBlock && !ws.isNew && (
        <div className="border-b p-2">
          <Notice
            tone="warning"
            title="暂时不能发布"
            action={
              ws.info.channel && ws.info.keyMissing ? (
                <Link
                  className="text-xs underline"
                  to={`/admin/ai/channels?edit=${encodeURIComponent(ws.info.channel.key)}`}
                >
                  去渠道页
                </Link>
              ) : undefined
            }
          >
            {ws.publishBlock}
          </Notice>
        </div>
      )}

      {ws.modeError && (
        <div className="border-b p-2">
          <Notice tone="danger" title="无法切回表单">
            {ws.modeError}
          </Notice>
        </div>
      )}

      {ws.loadingDetail && (
        <p className="text-muted-foreground border-b px-3 py-1.5 text-xs" role="status">
          正在加载…
        </p>
      )}

      {mode === "form" ? (
        <div className="min-h-0 flex-1 overflow-y-auto">
          {body ? (
            <ModelForm
              body={body}
              isNew={ws.isNew}
              channels={catalog.channels}
              plugins={catalog.plugins}
              channelsReady={catalog.channelsStatus === "ready" && catalog.pluginsStatus === "ready"}
              issues={ws.issues}
              epoch={ws.epoch}
              onChange={ws.editBody}
            />
          ) : (
            <div className="p-4">
              <Notice tone="danger" title="正文不是合法的 JSON 对象">
                切到 JSON 视图修复后再回来。
              </Notice>
            </div>
          )}
        </div>
      ) : (
        <>
          <textarea
            ref={textareaRef}
            value={ws.text}
            onChange={(event) => ws.setText(event.target.value)}
            spellCheck={false}
            wrap="off"
            aria-label="配置 JSON"
            aria-invalid={!parsed.ok}
            className="bg-background min-h-64 flex-1 resize-none overflow-auto p-3 font-mono text-xs leading-5 outline-none"
            onKeyDown={(event) => {
              // Tab 插入两个空格，别把焦点跳走
              if (event.key !== "Tab" || event.shiftKey) return;
              event.preventDefault();
              const el = event.currentTarget;
              const { selectionStart, selectionEnd } = el;
              ws.setText(`${ws.text.slice(0, selectionStart)}  ${ws.text.slice(selectionEnd)}`);
              requestAnimationFrame(() => {
                el.selectionStart = el.selectionEnd = selectionStart + 2;
              });
            }}
          />
          <div
            className={cn("border-t px-3 py-1.5 text-xs", parsed.ok ? "text-muted-foreground" : "text-destructive")}
            role={parsed.ok ? undefined : "alert"}
          >
            {parsed.ok
              ? "JSON 语法正确（结构与取值由“校验”检查）"
              : `JSON 语法错误${parsed.line ? `（第 ${parsed.line} 行 第 ${parsed.column} 列）` : ""}：${parsed.message}`}
          </div>
        </>
      )}

      <div className="flex flex-col gap-1.5 border-t p-2">
        <label htmlFor="model-sample" className="text-muted-foreground text-xs">
          示例输入（dry-run / 试跑用，按 input_schema 字段名，媒体字段填素材 id）
        </label>
        <textarea
          id="model-sample"
          value={ws.sample}
          onChange={(event) => ws.setSample(event.target.value)}
          spellCheck={false}
          className="border-input focus-visible:border-ring h-20 resize-none rounded-lg border bg-transparent p-2 font-mono text-xs outline-none"
        />
      </div>
    </section>
  );
}
