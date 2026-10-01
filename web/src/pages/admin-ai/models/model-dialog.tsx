import { useEffect, useImperativeHandle, useRef, useState, type Ref } from "react";
import { useSearchParams } from "react-router";
import {
  Braces,
  CheckCheck,
  CircleAlert,
  Coins,
  Ellipsis,
  FlaskConical,
  History,
  Info,
  Loader2,
  Rocket,
  Save,
  SlidersHorizontal,
  X,
  type LucideIcon,
} from "lucide-react";

import { VendorAvatar } from "@/components/admin-ui/vendor-avatar";
import { Notice } from "@/components/admin-ui/notice";
import { StatusDot } from "@/components/admin-ui/status-dot";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { DialogTitle } from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Switch } from "@/components/ui/switch";
import { cn } from "@/lib/utils";
import { findPathInJson } from "@/utils/admin/json";
import {
  readModelChannel,
  readModelKind,
  readModelPricing,
  readModelString,
  readModelStrings,
} from "@/utils/admin/model-body";

import type { AdminCatalog } from "../use-admin";
import { priceLabel } from "@/utils/pricing/quote";

import { ModelBasicForm } from "./model-basic-form";
import { fieldOfPath, modelChecks, type ModelTabId } from "./model-fields";
import { ModelParamsForm } from "./model-params-form";
import { PickerPreview, PreviewFrame, PricePreview } from "./model-previews";
import { ModelPriceForm } from "./model-price-form";
import { ModelTestDialog } from "./model-test-dialog";
import { TestNode } from "./test-node";
import type { ModelWorkspace } from "./use-model-workspace";

/** 弹窗对外暴露的动作：按校验问题的路径定位到字段 / JSON 行 */
export type ModelDialogHandle = { locate: (path: string) => void };

const TABS: Array<{ id: ModelTabId; label: string; icon: LucideIcon }> = [
  { id: "basic", label: "基本信息", icon: Info },
  { id: "params", label: "能力与参数", icon: SlidersHorizontal },
  { id: "price", label: "积分定价", icon: Coins },
];

/** JSON 文本框的行高（与 leading-5 一致），用来把命中行滚到可见位置 */
const LINE_HEIGHT = 20;

/**
 * 模型编辑弹窗（设计稿样式）：顶部身份与版本、三个页签、发布前待办、左表单右预览、底部上架与操作。
 * JSON 是事实来源，表单只是对它的读写；“更多”里可以切到 JSON 直接编辑、校验、回滚。
 */
export function ModelDialog({
  ws,
  catalog,
  ref,
}: {
  ws: ModelWorkspace;
  catalog: AdminCatalog;
  ref?: Ref<ModelDialogHandle>;
}) {
  const [tab, setTab] = useState<ModelTabId>("basic");
  // 从列表“更多 → 测试模型”进来时带 ?test=1，直接打开测试弹窗
  const [params] = useSearchParams();
  const [testOpen, setTestOpen] = useState(() => params.get("test") === "1");
  const [pendingLocate, setPendingLocate] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const { mode, body } = ws;

  const locate = (path: string) => {
    const field = fieldOfPath(path);
    if (mode === "form" && field) {
      setTab(field.tab);
      // 切页签后控件下一帧才渲染出来，再聚焦
      requestAnimationFrame(() => {
        const element = document.getElementById(field.id);
        element?.focus();
        element?.scrollIntoView({ block: "center" });
      });
      return;
    }
    if (mode !== "json") ws.switchMode("json");
    setPendingLocate(path);
  };
  useImperativeHandle(ref, () => ({ locate }));

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

  const label = body ? readModelString(body, "label") : "";
  const kind = body ? readModelKind(body) : "";
  const pricing = body ? readModelPricing(body) : null;
  const upstream = body ? readModelChannel(body).upstreamModel : "";
  const checks = modelChecks(body, ws.info, ws.issues);
  const disabled = ws.working || ws.loadingDetail;
  const published = ws.detail?.published;
  const draft = ws.detail?.draft;
  const rollbackable = ws.revisions?.filter((revision) => revision.status !== "draft") ?? [];
  const spinner = (name: string, icon: React.ReactNode) =>
    ws.busy === name ? <Loader2 className="animate-spin" /> : icon;
  const channelsReady = catalog.channelsStatus === "ready" && catalog.pluginsStatus === "ready";

  const preview = !body ? null : tab === "basic" ? (
    <PickerPreview
      modelKey={ws.modelKey}
      label={label}
      kind={kind}
      vendor={readModelString(body, "vendor")}
      tags={readModelStrings(body, "tags")}
      price={priceLabel(pricing ?? undefined)}
      hint={readModelString(body, "hint")}
      models={ws.models}
    />
  ) : tab === "params" ? (
    <PreviewFrame
      title="画布节点预览"
      note="左边每改一项，这里立即变化。可以直接在节点里选参数，点按钮用这些参数测试。"
    >
      <TestNode
        modelKey={ws.modelKey}
        vendor={readModelString(body, "vendor")}
        label={label}
        kind={kind}
        pricing={pricing ?? undefined}
        caps={ws.capabilities}
        params={ws.testParams}
        assets={ws.testAssets}
        errors={ws.testInput.errors}
        showErrors={ws.showTestErrors}
        onChange={ws.setTestParam}
        onAddRef={ws.addTestRef}
        onRemoveRef={ws.removeTestRef}
        action={{ label: "测试", onClick: () => setTestOpen(true) }}
      />
    </PreviewFrame>
  ) : (
    <PricePreview pricing={pricing} caps={ws.capabilities} />
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* 顶部：头像、标题与版本、渠道 · 上游 ID、更多、关闭 */}
      <div className="flex items-start gap-4 border-b px-6 py-4">
        <VendorAvatar
          vendor={body ? readModelString(body, "vendor") : ""}
          name={label || "?"}
          seed={ws.modelKey}
          className="size-10 text-base"
        />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <DialogTitle className="text-lg font-semibold">
              {ws.isNew ? "新建模型" : "编辑模型"}
            </DialogTitle>
            {!ws.isNew &&
              (published ? (
                <Tag tone="success">
                  <StatusDot tone="success" />
                  已发布 v{published.revision_no}
                </Tag>
              ) : (
                <Tag>未发布</Tag>
              ))}
            {draft && draft.id !== published?.id && (
              <Tag tone="info">草稿 v{draft.revision_no} 待发布</Tag>
            )}
            {ws.dirty && <Tag tone="warning">有未保存修改</Tag>}
          </div>
          <p className="text-muted-foreground mt-0.5 truncate text-sm">
            {ws.info.channel?.name ?? "未选渠道"} ·{" "}
            <span className="font-mono">{upstream || ws.modelKey || "未填写"}</span>
          </p>
        </div>
        {/* 发布前待办：放在头部空白处 */}
        {checks.length > 0 && (
          <div className="flex max-w-[45%] flex-wrap items-center justify-end gap-x-2 gap-y-1.5 self-center rounded-lg bg-amber-500/10 px-3 py-1.5 text-sm text-amber-700 dark:text-amber-400">
            <span className="inline-flex items-center gap-1.5 font-medium">
              <CircleAlert className="size-4" />
              发布前还需处理 {checks.length} 项
            </span>
            {checks.map((check, index) => (
              <button
                key={`${check.text}-${index}`}
                type="button"
                className="bg-background/60 hover:bg-background rounded-md px-2 py-0.5 text-xs"
                onClick={() => (check.path ? locate(check.path) : setTab(check.tab))}
              >
                {check.text}
              </button>
            ))}
          </div>
        )}
        <DropdownMenu
          modal={false}
          onOpenChange={(open) => {
            if (open && ws.key) void ws.loadRevisions();
          }}
        >
          <DropdownMenuTrigger
            render={<Button variant="ghost" size="icon-sm" aria-label="更多操作" />}
          >
            <Ellipsis />
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-56">
            <DropdownMenuGroup>
              <DropdownMenuItem onClick={() => ws.switchMode(mode === "json" ? "form" : "json")}>
                <Braces />
                {mode === "json" ? "回到表单" : "编辑 JSON"}
              </DropdownMenuItem>
              <DropdownMenuItem disabled={disabled} onClick={() => void ws.validate()}>
                <CheckCheck />
                校验配置
              </DropdownMenuItem>
            </DropdownMenuGroup>
            {!ws.isNew && (
              <>
                <DropdownMenuSeparator />
                <DropdownMenuSub>
                  <DropdownMenuSubTrigger disabled={disabled}>
                    <History />
                    回滚到历史版本
                  </DropdownMenuSubTrigger>
                  <DropdownMenuSubContent className="w-72">
                    <DropdownMenuGroup>
                      <DropdownMenuLabel>回滚到历史版本</DropdownMenuLabel>
                      {ws.revisions === null && (
                        <p className="text-muted-foreground px-1.5 py-2 text-xs">加载中…</p>
                      )}
                      {ws.revisions !== null && rollbackable.length === 0 && (
                        <p className="text-muted-foreground px-1.5 py-2 text-xs">
                          没有可回滚的历史版本
                        </p>
                      )}
                      {rollbackable.map((revision) => (
                        <DropdownMenuItem
                          key={revision.id}
                          onClick={() => ws.requestRollback(revision)}
                        >
                          <span className="flex min-w-0 flex-1 flex-col">
                            <span>
                              第 {revision.revision_no} 版 ·{" "}
                              {revision.status === "published" ? "当前发布" : "历史"}
                            </span>
                            <span className="text-muted-foreground truncate text-xs">
                              {new Date(revision.created_at).toLocaleString()}
                              {revision.note ? ` · ${revision.note}` : ""}
                            </span>
                          </span>
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuGroup>
                  </DropdownMenuSubContent>
                </DropdownMenuSub>
              </>
            )}
          </DropdownMenuContent>
        </DropdownMenu>
        <Button variant="ghost" size="icon-sm" aria-label="关闭" onClick={ws.closeEditor}>
          <X />
        </Button>
      </div>

      {/* 页签（JSON 视图下隐藏） */}
      {mode === "form" ? (
        <div className="flex items-center gap-1 border-b px-4" role="tablist">
          {TABS.map((item) => (
            <button
              key={item.id}
              type="button"
              role="tab"
              aria-selected={tab === item.id}
              onClick={() => setTab(item.id)}
              className={cn(
                "-mb-px inline-flex items-center gap-2 border-b-2 px-3 py-3 text-sm font-medium",
                tab === item.id
                  ? "border-foreground text-foreground"
                  : "text-muted-foreground hover:text-foreground border-transparent",
              )}
            >
              <item.icon className="size-4" />
              {item.label}
              {checks.some((check) => check.tab === item.id) && <StatusDot tone="warning" />}
            </button>
          ))}
        </div>
      ) : (
        <div className="flex items-center gap-2 border-b px-6 py-2.5 text-sm">
          <Braces className="size-4" />
          <span className="font-medium">JSON 编辑</span>
          <span className="text-muted-foreground text-xs">正文是事实来源，表单只是它的读写。</span>
          <Button size="xs" variant="ghost" className="ml-auto" onClick={ws.format}>
            格式化
          </Button>
          <Button size="xs" variant="outline" onClick={() => ws.switchMode("form")}>
            回到表单
          </Button>
        </div>
      )}

      {(ws.modeError || (ws.queue.length > 0 && ws.isNew)) && (
        <div className="space-y-2 border-b px-6 py-3">
          {ws.modeError && (
            <Notice tone="danger" title="无法切回表单">
              {ws.modeError}
            </Notice>
          )}
          {ws.queue.length > 0 && ws.isNew && (
            <Notice
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
              这是从渠道导入的草稿，<b>不会自动上架</b>
              。逐个确认、保存草稿、测试，再发布。
            </Notice>
          )}
        </div>
      )}

      {/* 主体：左表单右预览；JSON 视图占满 */}
      {mode === "json" ? (
        <div className="flex min-h-0 flex-1 flex-col">
          <textarea
            ref={textareaRef}
            value={ws.text}
            onChange={(event) => ws.setText(event.target.value)}
            spellCheck={false}
            wrap="off"
            aria-label="配置 JSON"
            aria-invalid={!ws.parsed.ok}
            className="bg-background min-h-64 flex-1 resize-none overflow-auto px-6 py-4 font-mono text-xs leading-5 outline-none"
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
            className={cn(
              "border-t px-6 py-1.5 text-xs",
              ws.parsed.ok ? "text-muted-foreground" : "text-destructive",
            )}
            role={ws.parsed.ok ? undefined : "alert"}
          >
            {ws.parsed.ok
              ? "JSON 语法正确（结构与取值由“校验配置”检查）"
              : `JSON 语法错误${ws.parsed.line ? `（第 ${ws.parsed.line} 行 第 ${ws.parsed.column} 列）` : ""}：${ws.parsed.message}`}
          </div>
        </div>
      ) : (
        <div className="grid min-h-0 flex-1 lg:grid-cols-[minmax(0,1fr)_22rem]">
          <div className="min-h-0 overflow-y-auto px-6 py-5">
            {ws.loadingDetail ? (
              <p className="text-muted-foreground text-sm" role="status">
                正在加载…
              </p>
            ) : !body ? (
              <Notice tone="danger" title="正文不是合法的 JSON 对象">
                从“更多 → 编辑 JSON”修复后再回来。
              </Notice>
            ) : tab === "basic" ? (
              <ModelBasicForm
                body={body}
                isNew={ws.isNew}
                channels={catalog.channels}
                plugins={catalog.plugins}
                channelsReady={channelsReady}
                info={ws.info}
                issues={ws.issues}
                onChange={ws.editBody}
              />
            ) : tab === "params" ? (
              <ModelParamsForm
                body={body}
                issues={ws.issues}
                epoch={ws.epoch}
                onChange={ws.editBody}
              />
            ) : (
              <ModelPriceForm
                body={body}
                kind={kind}
                caps={ws.capabilities}
                issues={ws.issues}
                onChange={ws.editBody}
              />
            )}
          </div>
          <aside className="bg-muted/20 hidden min-h-0 overflow-y-auto border-l p-5 lg:block">
            {preview}
          </aside>
        </div>
      )}

      {/* 底栏：上架开关 + 测试 / 取消 / 保存草稿 / 保存并发布 */}
      <div className="flex flex-wrap items-center gap-3 border-t px-6 py-3.5">
        <div className="flex items-center gap-3">
          <Switch
            checked={!!ws.detail?.enabled}
            disabled={disabled || !published || !ws.key}
            aria-label="在画布中上架"
            onCheckedChange={(checked) => void ws.toggleEnabled(checked)}
          />
          <div className="leading-tight">
            <div className="text-sm font-medium">在画布中上架</div>
            <div className="text-muted-foreground text-xs">
              {published ? "立即生效，不需要重新发布" : "发布过一个版本后才能上架"}
            </div>
          </div>
        </div>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <Button variant="outline" disabled={disabled} onClick={() => setTestOpen(true)}>
            <FlaskConical />
            测试模型
          </Button>
          <Button variant="outline" onClick={ws.closeEditor}>
            取消
          </Button>
          <Button variant="outline" disabled={disabled} onClick={() => void ws.save()}>
            {spinner("save", <Save />)}
            保存草稿
          </Button>
          <Button
            disabled={disabled || checks.length > 0}
            title={checks.length > 0 ? "先处理上方的待办项" : undefined}
            onClick={() => void ws.requestPublish()}
          >
            {spinner("publish", <Rocket />)}
            保存并发布
          </Button>
        </div>
      </div>

      <ModelTestDialog open={testOpen} ws={ws} onClose={() => setTestOpen(false)} />
    </div>
  );
}
