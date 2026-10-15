import { useEffect, useImperativeHandle, useRef, useState, type Ref } from "react";
import {
  Braces,
  Coins,
  FlaskConical,
  Info,
  Loader2,
  Save,
  SlidersHorizontal,
  X,
  type LucideIcon,
} from "lucide-react";

import { VendorAvatar } from "@/components/admin-ui/vendor-avatar";
import { Notice } from "@/components/admin-ui/notice";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { StatusDot } from "@/components/admin-ui/status-dot";
import { Tag } from "@/components/admin-ui/tag";
import { Button } from "@/components/ui/button";
import { DialogTitle } from "@/components/ui/dialog";
import { Skeleton } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { UnderlineTab, UnderlineTabs } from "@/components/admin-ui/underline-tabs";
import { cn } from "@/lib/utils";
import { findPathInJson } from "@/utils/admin/json";
import {
  readModelChannel,
  readModelKind,
  readModelPricing,
  readModelString,
  readModelStrings,
} from "@/utils/admin/model-body";

import type { ModelListItem } from "@/api/admin/ai/type.d";
import { publishBlockReason } from "@/utils/admin/model-channel";
import { priceLabel } from "@/utils/pricing/quote";

import { ModelBasicForm } from "./model-basic-form";
import { fieldOfPath, modelChecks, type ModelTabId } from "./model-fields";
import { ModelParamsForm } from "./model-params-form";
import { PickerPreview, PreviewFrame, PricePreview } from "./model-previews";
import { ModelPriceForm } from "./model-price-form";
import { TestNode } from "./test-node";
import type { ModelCatalog, ModelWorkspace } from "./use-model-workspace";

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
 * 模型编辑弹窗（设计稿样式）：顶部身份与上线状态、三个页签、上线前待办、左表单右预览、底部取消 / 保存。
 * JSON 是事实来源，表单只是对它的读写。
 */
export function ModelDialog({
  ws,
  catalog,
  ref,
  seed,
}: {
  ws: ModelWorkspace;
  catalog: ModelCatalog;
  /** 列表行里已有的信息：详情还在加载时先用它渲染头部 */
  seed?: ModelListItem;
  ref?: Ref<ModelDialogHandle>;
}) {
  const [tab, setTab] = useState<ModelTabId>("basic");
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

  const label = body ? readModelString(body, "label") : (seed?.label ?? "");
  const kind = body ? readModelKind(body) : "";
  const pricing = body ? readModelPricing(body) : null;
  const upstream = body ? readModelChannel(body).upstreamModel : "";
  const channelsReady = catalog.pluginsStatus === "ready";
  const checks = modelChecks(body, ws.info, ws.issues, channelsReady);
  const disabled = ws.working || ws.loadingDetail;
  const online = ws.detail ? !!ws.detail.enabled : !!seed?.enabled;
  const spinner = (name: string, icon: React.ReactNode) =>
    ws.busy === name ? <Loader2 className="animate-spin" /> : icon;
  // 启用与测试都基于服务端已保存的配置：新建的、有未保存修改的先保存
  const saveFirst = ws.isNew ? "先保存模型" : ws.dirty ? "有未保存的修改，先保存" : null;
  const enableDeny = online
    ? null
    : (saveFirst ?? publishBlockReason(ws.info, catalog.pluginsStatus === "ready"));
  const testDeny =
    saveFirst ??
    (!kind
      ? "还没有选择模型能力，先选择能力和协议并保存"
      : kind === "agent"
        ? "Agent 模型暂不支持试跑"
        : !ws.info.protocol
          ? "还没有选择调用协议，先选择协议并保存"
          : null);

  const preview = !body ? null : tab === "basic" ? (
    <PickerPreview
      modelKey={ws.modelKey}
      label={label}
      kind={kind}
      vendor={readModelString(body, "vendor")}
      tags={readModelStrings(body, "tags")}
      price={priceLabel(pricing ?? undefined)}
      hint={readModelString(body, "hint")}
    />
  ) : tab === "params" ? (
    <PreviewFrame title="画布节点预览">
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
      />
    </PreviewFrame>
  ) : (
    <PricePreview pricing={pricing} caps={ws.capabilities} />
  );

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* 顶部：头像、标题与上线状态、渠道 · 上游 ID、关闭 */}
      <div className="flex items-start gap-4 border-b px-6 py-4">
        <VendorAvatar
          vendor={body ? readModelString(body, "vendor") : (seed?.vendor ?? "")}
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
              (online ? (
                <Tag tone="success">
                  <StatusDot tone="success" />
                  已上线
                </Tag>
              ) : (
                <Tag>未上线</Tag>
              ))}
            {ws.dirty && <Tag tone="warning">有未保存修改</Tag>}
          </div>
          <p className="text-muted-foreground mt-0.5 truncate text-sm">
            {ws.info.channel?.name ?? (body ? "未选渠道" : seed?.channel_name || "未选渠道")} ·{" "}
            <span className="font-mono">{upstream || ws.modelKey || "未填写"}</span>
          </p>
        </div>
        <Button variant="ghost" size="icon-sm" aria-label="关闭" onClick={ws.closeEditor}>
          <X />
        </Button>
      </div>

      {/* 页签（JSON 视图下隐藏） */}
      {mode === "form" ? (
        <UnderlineTabs>
          {TABS.map((item) => (
            <UnderlineTab key={item.id} selected={tab === item.id} onClick={() => setTab(item.id)}>
              <item.icon />
              {item.label}
              {checks.some((check) => check.tab === item.id) && <StatusDot tone="warning" />}
            </UnderlineTab>
          ))}
        </UnderlineTabs>
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

      {ws.modeError && (
        <div className="border-b px-6 py-3">
          <Notice tone="danger" title="无法切回表单">
            {ws.modeError}
          </Notice>
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
              <ModelFormSkeleton />
            ) : !body ? (
              <Notice tone="danger" title="正文不是合法的 JSON 对象">
                从“更多 → 编辑 JSON”修复后再回来。
              </Notice>
            ) : tab === "basic" ? (
              <ModelBasicForm
                body={body}
                isNew={ws.isNew}
                catalog={catalog}
                issues={ws.issues}
                onChange={ws.editBody}
              />
            ) : (
              <>
                {tab === "params" ? (
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
              </>
            )}
          </div>
          <aside className="bg-muted/20 hidden min-h-0 overflow-y-auto border-l p-5 lg:block">
            {ws.loadingDetail ? <Skeleton className="h-40 w-full" /> : preview}
          </aside>
        </div>
      )}

      {/* 底栏：启用开关与测试（都基于已保存的配置）、取消 / 保存（已上线的模型保存即生效） */}
      <div className="flex flex-wrap items-center gap-3 border-t px-6 py-3.5">
        <div className="flex items-center gap-2.5 text-sm">
          <ReasonTooltip reason={enableDeny}>
            <Switch
              checked={online}
              disabled={disabled || !!enableDeny}
              aria-label="模型上线"
              onCheckedChange={(checked) => void ws.toggleEnabled(checked)}
            />
          </ReasonTooltip>
          <span>
            <b className="font-medium">{online ? "已上线" : "未上线"}</b>
            <span className="text-muted-foreground block text-xs">
              开关立即生效；上线前要过启用检查
            </span>
          </span>
        </div>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <ReasonTooltip reason={testDeny}>
            <Button
              variant="outline"
              disabled={disabled || !!testDeny}
              onClick={() => ws.selectModel(ws.modelKey, { test: true })}
            >
              <FlaskConical />
              测试模型
            </Button>
          </ReasonTooltip>
          <Button variant="outline" onClick={ws.closeEditor}>
            取消
          </Button>
          <Button disabled={disabled} onClick={() => void ws.save(false, true)}>
            {spinner("save", <Save />)}
            保存
          </Button>
        </div>
      </div>
    </div>
  );
}

/** 详情还没回来时的表单骨架：几组“标签 + 输入框”，版式接近基本信息页签 */
function ModelFormSkeleton() {
  return (
    <div className="flex flex-col gap-5" role="status" aria-busy="true" aria-label="正在加载">
      {Array.from({ length: 5 }, (_, index) => (
        <div key={index} className="flex flex-col gap-2">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-9 w-full" />
        </div>
      ))}
    </div>
  );
}
