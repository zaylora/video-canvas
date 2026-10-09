import { motion } from "motion/react";
import { SlidersHorizontal } from "lucide-react";

import type { GenerationOp, ModelInfo } from "@/api/model/type";
import { OpTabs } from "@/components/canvas/op-tabs";
import { VideoParamPanel } from "@/components/canvas/video-param-panel";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { TAP } from "@/lib/motion";
import type { ComposerRef } from "@/utils/conversation/submission";
import {
  currentOp,
  isAutoOp,
  OP_LABEL,
  openParams,
  opDisabledHint,
  paramSummary,
} from "@/utils/tasks/capabilities";

/** 参数面板用不到上游连线，素材口为空 */
const NO_BINDINGS = { images: [], videos: [], audios: [] };

/** 参考素材由输入卡片自己管，参数面板里不会触发这些 */
const NOOP = () => undefined;
const NO_ASSETS = () => [];

/**
 * 输入卡片里的参数按钮：显示参数摘要（如「16:9 · 5秒 · 1080P · 2个」），点开按模型能力画出
 * 比例、时长、清晰度、生成数量等开放给用户的参数，与画布节点的参数面板是同一套控件。
 * 顶上是生成方式（文生 / 图生 / 全能参考）：和画布节点同一个选择器、同一套置灰规则，
 * 有参考素材时不引用素材的方式灰掉，没有参考素材时要引用素材的方式灰掉；
 * 加参考素材时生成方式会自动切到收它的方式（见 use-composer-refs）。
 * 模型既没有开放参数、也没有可选的生成方式时不显示。
 * @param model 当前模型
 * @param params 已设置的参数取值，没设的取模型默认值
 * @param refs 输入卡片里的参考素材：生成方式的置灰规则要用
 * @param onChange 改一个参数
 * @param side 弹层方向
 */
export function ParamsPopover({
  model,
  params,
  refs,
  onChange,
  side = "bottom",
}: {
  model: ModelInfo;
  params: Record<string, unknown>;
  refs: ComposerRef[];
  onChange: (name: string, value: unknown) => void;
  side?: "top" | "bottom";
}) {
  const caps = model.capabilities;
  const ops = caps.ops ?? [];
  const hasRefs = refs.length > 0;
  const op = currentOp(
    caps,
    params,
    refs.some((ref) => ref.kind === "image"),
  );
  const showOps = ops.length > 1 && !isAutoOp(caps);
  if (openParams(caps).length === 0 && !showOps) return null;
  return (
    <Popover>
      <PopoverTrigger
        render={<motion.button type="button" whileTap={TAP} />}
        aria-label="生成参数"
        className="text-foreground hover:bg-muted focus-visible:ring-ring/50 inline-flex h-8 max-w-56 items-center gap-1.5 rounded-[10px] px-2.5 text-[13px] outline-none focus-visible:ring-3"
      >
        <SlidersHorizontal className="size-4 shrink-0" />
        <span className="truncate">
          {[showOps && op ? OP_LABEL[op] : "", paramSummary(caps, params)]
            .filter(Boolean)
            .join(" · ")}
        </span>
      </PopoverTrigger>
      <PopoverContent side={side} align="start" sideOffset={6} className="w-80">
        {showOps && (
          <OpTabs
            id="composer"
            value={op}
            options={ops.map((item) => ({
              value: item,
              label: OP_LABEL[item],
              disabledHint: opDisabledHint(caps, item, hasRefs),
            }))}
            onValueChange={(next: GenerationOp) => onChange("op", next)}
          />
        )}
        <VideoParamPanel
          section="params"
          caps={caps}
          params={params}
          bindings={NO_BINDINGS}
          errors={{}}
          showErrors={false}
          onChange={onChange}
          onAddRef={NOOP}
          onRemoveRef={NOOP}
          listAssets={NO_ASSETS}
        />
      </PopoverContent>
    </Popover>
  );
}
