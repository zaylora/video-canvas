import { FlaskConical, Loader2, Rocket } from "lucide-react";
import { useEffect, useState } from "react";
import { toast } from "sonner";

import {
  checkImageProcessor,
  createImageProcessor,
  getImageProcessor,
  publishImageProcessor,
  updateImageProcessor,
} from "@/api/admin-image-processor";
import type {
  ProcessorConfig,
  ProcessorPreset,
  ProcessorVendor,
  ProcessorView,
} from "@/api/admin-image-processor/type.d";
import type { StorageView } from "@/api/admin-storage/type.d";
import { confirm } from "@/components/admin-ui/confirm-dialog";
import { ReasonTooltip } from "@/components/admin-ui/reason-tooltip";
import { Stepper, StepperItem } from "@/components/admin-ui/stepper";
import { Button } from "@/components/ui/button";
import {
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/admin-ui/sheet";
import { isProcessorVersionConflict } from "@/utils/admin/errors";
import {
  buildProcessorConfig,
  checkIsStale,
  defaultConfig,
  enableTarget,
  evaluateStorage,
  publishBlockReason,
  validateProcessorForm,
} from "@/utils/admin/image-processor";

import { ReadOnlyNotice } from "../shared";
import { useAliveRef } from "../use-admin";
import { StepCheck } from "./step-check";
import { StepParams } from "./step-params";
import { StepStorage } from "./step-storage";
import { StepVendor } from "./step-vendor";

/** 向导步骤：1 选厂商，2 绑定存储，3 参数，4 校验与发布 */
export type WizardStep = 1 | 2 | 3 | 4;

const STEP_LABELS = ["选厂商", "绑定存储", "参数", "校验与发布"] as const;

/** 向导里正在编辑的内容；vendor / storageId 在创建后不可改 */
type Draft = {
  vendor: ProcessorVendor | null;
  storageId: number | null;
  name: string;
  /** 名称被手动改过后，换存储不再自动改名 */
  nameTouched: boolean;
  config: ProcessorConfig | null;
};

const autoName = (storage: StorageView, preset: ProcessorPreset) =>
  `${storage.name} · ${preset.name}`;

/** 判断“有没有未保存修改”用的指纹：只含会提交给后端的内容 */
const draftKey = (preset: ProcessorPreset | null, draft: Draft) =>
  preset && draft.config
    ? JSON.stringify({
        name: draft.name.trim(),
        config: buildProcessorConfig(preset, draft.config),
      })
    : "";

function draftFromProcessor(processor: ProcessorView): Draft {
  return {
    vendor: processor.vendor,
    storageId: processor.storage_id,
    name: processor.name,
    nameTouched: true,
    config: { ...processor.config },
  };
}

/**
 * 抽屉里的四步向导。表单状态只在这里；父级用“打开一次一个实例”控制何时重新初始化。
 * 第三步“保存并校验”会保存草稿（新建或带 version 更新）再调 /check；已发布的处理服务保存的是草稿，发布才生效。
 * 版本冲突（52004）时重新拉取并重置表单；有 fail 或校验过期不能发布。
 * @param initial 编辑的处理服务；新建时为 null
 * @param startStorageId 从存储表“启用处理服务”进来时预选的存储
 * @param startStep 初始步骤；编辑默认第三步
 * @param presets 厂商预设
 * @param storages 全部存储
 * @param processors 全部处理服务（判定存储占用）
 * @param canWrite 是否有写权限；没有则整个向导只读
 * @param onChanged 处理服务在抽屉里变了（创建、保存、校验、冲突后重新拉取），用最新视图更新列表
 * @param onPublished 发布成功后（页面负责刷新列表并关闭抽屉）
 * @param onDirtyChange 上报是否有未保存的修改，关闭时据此确认
 * @param onClose 请求关闭
 */
export function ProcessorWizard({
  initial,
  startStorageId,
  startStep,
  presets,
  storages,
  processors,
  canWrite,
  onChanged,
  onPublished,
  onDirtyChange,
  onClose,
}: {
  initial: ProcessorView | null;
  startStorageId?: number;
  startStep?: WizardStep;
  presets: ProcessorPreset[];
  storages: StorageView[];
  processors: ProcessorView[];
  canWrite: boolean;
  onChanged: (view: ProcessorView) => void;
  onPublished: () => void;
  onDirtyChange: (dirty: boolean) => void;
  onClose: () => void;
}) {
  const aliveRef = useAliveRef();
  const readOnly = !canWrite;
  const [proc, setProc] = useState<ProcessorView | null>(initial);
  const [draft, setDraft] = useState<Draft>(() => {
    if (initial) return draftFromProcessor(initial);
    const storage = storages.find((item) => item.id === startStorageId);
    const preset = storage ? enableTarget(storage, presets, processors).preset : null;
    if (storage && preset) {
      return {
        vendor: preset.vendor,
        storageId: storage.id,
        name: autoName(storage, preset),
        nameTouched: false,
        config: defaultConfig(preset, storage),
      };
    }
    return { vendor: null, storageId: null, name: "", nameTouched: false, config: null };
  });
  const [step, setStep] = useState<WizardStep>(() =>
    initial ? (startStep ?? 3) : startStorageId && draft.vendor ? 3 : 1,
  );
  const [submitted, setSubmitted] = useState(false);
  const [busy, setBusy] = useState<"save" | "check" | null>(null);

  const preset = presets.find((item) => item.vendor === draft.vendor) ?? null;
  const storage = storages.find((item) => item.id === draft.storageId) ?? null;
  const locked = readOnly || !!proc;
  const [savedKey, setSavedKey] = useState(() => draftKey(preset, draft));
  const dirty = !readOnly && (proc ? draftKey(preset, draft) !== savedKey : draft.vendor !== null);

  useEffect(() => {
    onDirtyChange(dirty);
  }, [dirty, onDirtyChange]);
  /** 向导卸载（关闭、切换对象）时清掉脏标记，避免下次打开误提示“放弃修改” */
  useEffect(() => () => onDirtyChange(false), [onDirtyChange]);

  const errors =
    preset && draft.config
      ? validateProcessorForm(preset, { name: draft.name, config: draft.config })
      : {};
  const publishReason = proc ? publishBlockReason(proc) : "请先保存并校验";

  const pickVendor = (vendor: ProcessorVendor) => {
    if (vendor === draft.vendor) return;
    setDraft({ vendor, storageId: null, name: "", nameTouched: false, config: null });
  };

  const pickStorage = (next: StorageView) => {
    if (!preset) return;
    setDraft((prev) => ({
      ...prev,
      storageId: next.id,
      config: defaultConfig(preset, next),
      name: prev.nameTouched ? prev.name : autoName(next, preset),
    }));
  };

  /** 保存或校验之后更新本地与列表里的视图 */
  const accept = (view: ProcessorView, key: string) => {
    if (!aliveRef.current) return;
    setProc(view);
    setSavedKey(key);
    onChanged(view);
  };

  /** 版本冲突：别人先改了，重新拉取最新内容并重置表单，回到参数一步确认 */
  const resync = async () => {
    if (!proc) return;
    try {
      const fresh = await getImageProcessor(proc.id);
      if (!aliveRef.current) return;
      const next = draftFromProcessor(fresh);
      setProc(fresh);
      setDraft(next);
      setSavedKey(draftKey(presets.find((item) => item.vendor === fresh.vendor) ?? null, next));
      setStep(3);
      onChanged(fresh);
      toast.warning("该处理服务刚被其他人修改，已刷新为最新内容，请确认后重新保存");
    } catch {
      // 全局 toast 已弹；保留当前表单
    }
  };

  /** 校验并更新视图；key 是此刻已保存内容的指纹（闭包里的 savedKey 可能还是保存前的） */
  const runCheck = async (target: ProcessorView, key: string) => {
    setBusy("check");
    try {
      const view = await checkImageProcessor(target.id);
      accept(view, key);
      return view;
    } finally {
      if (aliveRef.current) setBusy(null);
    }
  };

  /** 第三步：保存草稿（有改动或还没创建时），再校验（没校验过或已过期时），然后进入第四步 */
  const saveAndCheck = async () => {
    setSubmitted(true);
    if (!preset || !draft.config || !draft.storageId || !draft.vendor || busy) return;
    if (Object.keys(errors).length > 0) return;
    const key = draftKey(preset, draft);
    const body = { name: draft.name.trim(), config: buildProcessorConfig(preset, draft.config) };
    let current = proc;
    setBusy("save");
    try {
      if (!current) {
        current = await createImageProcessor({
          ...body,
          vendor: draft.vendor,
          storage_id: draft.storageId,
        });
        accept(current, key);
      } else if (key !== savedKey) {
        current = await updateImageProcessor(current.id, { ...body, version: current.version });
        accept(current, key);
      }
    } catch (error) {
      if (aliveRef.current) setBusy(null);
      if (isProcessorVersionConflict(error)) await resync();
      return;
    }
    if (aliveRef.current) setStep(4);
    if (!current || !checkIsStale(current)) {
      if (aliveRef.current) setBusy(null);
      return;
    }
    try {
      await runCheck(current, key);
    } catch {
      // 全局 toast 已弹；保存已成功，可在第四步重试校验
    }
  };

  const next = () => {
    if (step === 3 && !readOnly) void saveAndCheck();
    else if (step < 4) setStep((step + 1) as WizardStep);
  };

  const retryCheck = async () => {
    if (!proc || busy) return;
    try {
      await runCheck(proc, savedKey);
    } catch (error) {
      if (isProcessorVersionConflict(error)) await resync();
    }
  };

  const publish = () => {
    if (!proc || !storage) return;
    const target = proc;
    const note = preset ? evaluateStorage(preset, storage, processors, target.id).note : null;
    void confirm({
      title: `发布「${target.name}」？`,
      confirmLabel: "发布",
      description: (
        <>
          发布后，「{target.storage_name}」存储里的素材开始使用处理 URL。
          {note && <b className="text-foreground block">{note}。</b>}
        </>
      ),
      onConfirm: async () => {
        try {
          await publishImageProcessor(target.id, { version: target.version });
        } catch (error) {
          if (isProcessorVersionConflict(error)) await resync();
          throw error;
        }
        toast.success("已发布");
        onDirtyChange(false);
        onPublished();
      },
    });
  };

  const title = readOnly ? "查看处理服务" : proc ? "编辑处理服务" : "新建处理服务";
  const canNext = step === 1 ? !!draft.vendor : step === 2 ? !!draft.storageId : true;
  const working = busy !== null;

  return (
    <>
      <SheetHeader className="border-b">
        <SheetTitle>{title}</SheetTitle>
        <SheetDescription>
          为素材所在的存储配置缩略图 / 视频封面的处理服务，只对绑定的那套存储生效。
        </SheetDescription>
        <Stepper className="mt-2" aria-label="向导步骤">
          {STEP_LABELS.map((label, index) => (
            <StepperItem
              key={label}
              index={index + 1}
              state={step === index + 1 ? "current" : step > index + 1 ? "done" : "todo"}
            >
              {label}
            </StepperItem>
          ))}
        </Stepper>
      </SheetHeader>

      <div className="flex min-h-0 flex-1 flex-col overflow-y-auto px-4 py-4">
        {readOnly && <ReadOnlyNotice what="新建、编辑、校验、发布处理服务" className="mb-4" />}

        {step === 1 && (
          <StepVendor
            presets={presets}
            value={draft.vendor}
            disabled={locked}
            onChange={pickVendor}
          />
        )}
        {step === 2 && preset && (
          <StepStorage
            preset={preset}
            storages={storages}
            processors={processors}
            value={draft.storageId}
            selfId={proc?.id ?? null}
            disabled={locked}
            onChange={pickStorage}
          />
        )}
        {step === 3 && preset && storage && draft.config && (
          <StepParams
            preset={preset}
            storageName={storage.name}
            name={draft.name}
            config={draft.config}
            errors={submitted ? errors : {}}
            published={proc?.status === "published"}
            disabled={readOnly}
            onNameChange={(name) => setDraft((prev) => ({ ...prev, name, nameTouched: true }))}
            onConfigChange={(patch) =>
              setDraft((prev) =>
                prev.config ? { ...prev, config: { ...prev.config, ...patch } } : prev,
              )
            }
          />
        )}
        {step === 4 && <StepCheck processor={proc} running={busy === "check"} />}
      </div>

      <SheetFooter className="flex-row items-center border-t">
        {step > 1 && (
          <Button
            type="button"
            variant="outline"
            disabled={working}
            onClick={() => setStep((step - 1) as WizardStep)}
          >
            上一步
          </Button>
        )}
        <div className="ml-auto flex items-center gap-2">
          <Button type="button" variant="outline" onClick={onClose}>
            {readOnly ? "关闭" : "取消"}
          </Button>
          {step < 4 && (
            <Button type="button" disabled={!canNext || working} onClick={next}>
              {working && <Loader2 className="animate-spin" />}
              {step === 3 && !readOnly ? "保存并校验" : "下一步"}
            </Button>
          )}
          {step === 4 && !readOnly && (
            <>
              <Button
                type="button"
                variant="outline"
                disabled={!proc || working}
                onClick={() => void retryCheck()}
              >
                {busy === "check" ? <Loader2 className="animate-spin" /> : <FlaskConical />}
                {proc?.check ? "重新校验" : "运行校验"}
              </Button>
              <ReasonTooltip reason={publishReason}>
                <Button type="button" disabled={!!publishReason || working} onClick={publish}>
                  <Rocket />
                  发布
                </Button>
              </ReasonTooltip>
            </>
          )}
        </div>
      </SheetFooter>
    </>
  );
}
