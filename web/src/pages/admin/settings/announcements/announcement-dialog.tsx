import { useMemo, useState } from "react";
import { Loader2 } from "lucide-react";

import { createAnnouncement, updateAnnouncement } from "@/api/admin/announcement";
import type { AnnouncementAdminItem } from "@/api/admin/announcement/type";
import type { AnnouncementDto } from "@/api/announcement/type";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/admin-ui/dialog";
import { FormField } from "@/components/admin-ui/form-field";
import { AnnouncementBar } from "@/components/home/announcement-bar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  TITLE_MAX,
  charCount,
  emptyAnnouncementForm,
  formFromItem,
  formToBody,
  validateAnnouncementForm,
  type AnnouncementForm,
} from "@/utils/admin/announcement";

import { useAliveRef } from "../../use-admin";

/**
 * 新建 / 编辑公告的弹窗（宽 520px，居中）：顶部是和首页同一个组件的实时预览，下面依次是
 * 标题、生效时间、启用。保存前按和后端相同的规则校验，错误显示在字段下面。
 * @param item 正在编辑的公告；null 表示新建
 * @param open 是否打开
 * @param onSaved 保存成功（调用方重新拉列表，状态取决于整表排名）
 */
export function AnnouncementDialog({
  open,
  item,
  onClose,
  onSaved,
}: {
  open: boolean;
  item: AnnouncementAdminItem | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  return (
    <Dialog open={open} onOpenChange={(next) => !next && onClose()}>
      <DialogContent className="gap-0 p-0 sm:max-w-[520px]">
        {/* 换一条公告或重新打开就换 key，表单状态整份重置 */}
        <DialogForm key={item?.id ?? "new"} item={item} onClose={onClose} onSaved={onSaved} />
      </DialogContent>
    </Dialog>
  );
}

function DialogForm({
  item,
  onClose,
  onSaved,
}: {
  item: AnnouncementAdminItem | null;
  onClose: () => void;
  onSaved: () => void;
}) {
  const aliveRef = useAliveRef();
  const [form, setForm] = useState<AnnouncementForm>(() =>
    item ? formFromItem(item) : emptyAnnouncementForm(),
  );
  const [touched, setTouched] = useState(false);
  const [saving, setSaving] = useState(false);
  const errors = validateAnnouncementForm(form);
  const set = <K extends keyof AnnouncementForm>(key: K, value: AnnouncementForm[K]) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  /** 预览用的公告：和首页拿到的结构一样，版本固定（预览里关闭不会生效） */
  const preview = useMemo<AnnouncementDto[]>(
    () => [
      {
        id: item?.id ?? 0,
        title: form.title.trim() || "公告标题",
        endsAt: null,
        version: 0,
      },
    ],
    [item?.id, form.title],
  );

  const save = async () => {
    setTouched(true);
    if (Object.keys(errors).length > 0) return;
    setSaving(true);
    try {
      const body = formToBody(form);
      if (item) await updateAnnouncement(item.id, body);
      else await createAnnouncement(body);
      if (!aliveRef.current) return;
      onSaved();
      onClose();
    } catch {
      // 全局 toast 已弹（含后端给的具体原因），弹窗保持打开
    } finally {
      if (aliveRef.current) setSaving(false);
    }
  };

  return (
    <form
      className="grid min-w-0 grid-cols-[minmax(0,1fr)]"
      onSubmit={(event) => {
        event.preventDefault();
        void save();
      }}
    >
      <DialogHeader className="px-6 pt-6 pb-4">
        <DialogTitle>{item ? "编辑公告" : "新建公告"}</DialogTitle>
        <DialogDescription className="sr-only">
          公告显示在首页顶栏正中的胶囊里，一次一条轮播。
        </DialogDescription>
      </DialogHeader>

      <div className="grid max-h-[70svh] grid-cols-[minmax(0,1fr)] content-start gap-4.5 overflow-y-auto px-6 pb-6">
        {/* 实时预览：和首页顶栏里同一个胶囊组件；只看不点。
            预览框和表单列都要允许缩小（min-w-0 / minmax(0,1fr)），否则不换行的长标题会把整列撑出弹窗 */}
        <div
          aria-hidden
          className="bg-muted/60 pointer-events-none flex min-w-0 justify-center overflow-hidden rounded-xl px-3 py-4"
        >
          <AnnouncementBar items={preview} onClose={() => undefined} />
        </div>

        <FormField
          label={
            <span className="flex w-full items-center justify-between">
              标题
              <span className="text-muted-foreground font-normal tabular-nums">
                {charCount(form.title.trim())}/{TITLE_MAX}
              </span>
            </span>
          }
          htmlFor="announcement-title"
          required
          error={touched ? errors.title : undefined}
        >
          <Input
            id="announcement-title"
            autoFocus
            value={form.title}
            aria-invalid={touched && !!errors.title}
            onChange={(event) => set("title", event.target.value)}
          />
        </FormField>

        <div className="grid grid-cols-2 gap-3 max-sm:grid-cols-1">
          <FormField label="开始时间" htmlFor="announcement-starts">
            <Input
              id="announcement-starts"
              type="datetime-local"
              value={form.startsAt}
              onChange={(event) => set("startsAt", event.target.value)}
            />
          </FormField>
          <FormField
            label="结束时间"
            htmlFor="announcement-ends"
            error={touched ? errors.endsAt : undefined}
          >
            <Input
              id="announcement-ends"
              type="datetime-local"
              value={form.endsAt}
              aria-invalid={touched && !!errors.endsAt}
              onChange={(event) => set("endsAt", event.target.value)}
            />
          </FormField>
        </div>

        <label className="flex items-center justify-between gap-3 py-1.5 text-sm font-medium">
          启用
          <Switch checked={form.enabled} onCheckedChange={(checked) => set("enabled", checked)} />
        </label>
      </div>

      <DialogFooter className="m-0 rounded-b-3xl">
        <Button type="button" variant="ghost" onClick={onClose} disabled={saving}>
          取消
        </Button>
        <Button type="submit" disabled={saving}>
          {saving && <Loader2 className="animate-spin" />}
          保存
        </Button>
      </DialogFooter>
    </form>
  );
}
