import { useMemo, useState } from "react";
import { AudioLines, Check, Plus, VolumeX } from "lucide-react";

import type { CharacterDto } from "@/api/character/type";
import { AssetGridSkeleton } from "@/components/assets/asset-grid";
import { AssetSearch } from "@/components/assets/asset-search";
import { EmptyMark } from "@/components/home/empty-mark";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { useCharacterList } from "@/hooks/use-characters";
import { cn } from "@/lib/utils";
import {
  characterPickerSummary,
  filterCharacters,
  toggleCharacter,
} from "@/utils/characters/picker-logic";

import { CharacterCover } from "./character-card";
import { CharacterEditorDialog } from "./character-editor-dialog";

/** 选择框里的卡片网格：竖版卡片，手机两列，桌面按最小 150px 自适应 */
const PICK_GRID = "grid grid-cols-2 gap-2.5 sm:grid-cols-[repeat(auto-fill,minmax(150px,1fr))]";

/**
 * 选择框里的一张卡片：3:4 封面 + 勾选框 + 名称与「有声音 · N 张图」；
 * 已在当前画布上的标「画布中已有」（仍可再放）。
 * @param card 角色卡
 * @param selected 是否已选
 * @param placed 当前画布上已经有这张卡的节点
 * @param onToggle 点击勾选 / 取消
 */
export function CharacterPickTile({
  card,
  selected,
  placed,
  onToggle,
}: {
  card: CharacterDto;
  selected: boolean;
  placed: boolean;
  onToggle: () => void;
}) {
  return (
    <button
      type="button"
      data-slot="character-pick"
      data-selected={selected || undefined}
      aria-pressed={selected}
      onClick={onToggle}
      className={cn(
        "bg-muted focus-visible:ring-ring/60 relative aspect-3/4 min-w-0 overflow-hidden rounded-xl text-left outline-none focus-visible:ring-3",
        selected && "ring-foreground ring-2",
      )}
    >
      <CharacterCover card={card} />
      <span
        aria-hidden
        className={cn(
          "absolute top-2 left-2 z-10 grid size-5 place-items-center rounded-full transition-colors duration-120 motion-reduce:transition-none",
          selected
            ? "bg-foreground text-background"
            : "bg-cover-scrim ring-cover-foreground/70 ring-1",
        )}
      >
        <Check className={cn("size-3.5", selected ? "opacity-100" : "opacity-0")} />
      </span>
      {placed && (
        <span
          data-slot="placed-badge"
          className="bg-cover-scrim text-cover-foreground absolute top-2 right-2 z-10 rounded-md px-1.5 py-0.5 text-[11px]"
        >
          画布中已有
        </span>
      )}
      <span className="from-cover-scrim text-cover-foreground absolute inset-x-0 bottom-0 bg-linear-to-t to-transparent px-2.5 pt-8 pb-2">
        <span className="block truncate text-[13px] font-medium">{card.name}</span>
        <span className="flex items-center gap-1 text-[11px] opacity-80">
          {card.voice ? <AudioLines className="size-3" /> : <VolumeX className="size-3" />}
          {card.voice ? "有声音" : "无声音"} · {card.extras.length + (card.avatar ? 1 : 0)} 张图
        </span>
      </span>
    </button>
  );
}

/**
 * 画布双击后的「角色卡」选择框（设计 §6.4）：竖版卡片网格，多选；右上「新建角色卡」打开同一个编辑弹框，
 * 创建完回到选择框并自动选中。单选模式（replace）用于已失效节点「重新选择」，只能选一张。
 * 每次打开都从初始状态开始。
 * @param open 是否打开
 * @param onOpenChange 打开状态变化
 * @param replace 单选替换模式
 * @param placedIds 当前画布上已有节点的角色卡 ID（标「画布中已有」）
 * @param onConfirm 确认：拿到选中的角色卡（按勾选顺序）；调用后弹框自动关闭
 */
export function CharacterPickerDialog({
  open,
  onOpenChange,
  replace = false,
  placedIds,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  replace?: boolean;
  placedIds: ReadonlySet<string>;
  onConfirm: (cards: CharacterDto[]) => void;
}) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        data-slot="character-picker"
        className="flex max-h-[min(40rem,calc(100dvh-4rem))] flex-col gap-0 overflow-hidden p-0 sm:max-w-3xl"
      >
        <DialogTitle className="px-4 pt-4 text-base">
          {replace ? "重新选择角色卡" : "添加角色卡"}
        </DialogTitle>
        <DialogDescription className="sr-only">选择要放到画布上的角色卡</DialogDescription>
        {open && (
          <PickerBody
            replace={replace}
            placedIds={placedIds}
            onConfirm={(cards) => {
              onConfirm(cards);
              onOpenChange(false);
            }}
          />
        )}
      </DialogContent>
    </Dialog>
  );
}

function PickerBody({
  replace,
  placedIds,
  onConfirm,
}: {
  replace: boolean;
  placedIds: ReadonlySet<string>;
  onConfirm: (cards: CharacterDto[]) => void;
}) {
  const { status, items: loaded, reload } = useCharacterList();
  /** 刚在这里新建的卡：列表重新加载回来之前也要能选、能确认 */
  const [created, setCreated] = useState<CharacterDto[]>([]);
  const items = useMemo(
    () => [...created.filter((card) => !loaded.some((item) => item.id === card.id)), ...loaded],
    [created, loaded],
  );
  const [q, setQ] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [creating, setCreating] = useState(false);

  const visible = useMemo(() => filterCharacters(items, q), [items, q]);
  const summary = characterPickerSummary(selected, replace);
  const create = () => setCreating(true);

  return (
    <>
      <div className="flex items-center gap-2 px-4 py-3">
        <AssetSearch value={q} onChange={setQ} placeholder="搜索角色" />
        <Button variant="outline" size="sm" className="ml-auto" onClick={create}>
          <Plus />
          新建角色卡
        </Button>
      </div>

      <div className="min-h-56 flex-1 overflow-y-auto px-4 pb-3">
        {status === "loading" ? (
          <AssetGridSkeleton count={8} />
        ) : status === "error" ? (
          <EmptyMark
            className="min-h-56"
            title="加载失败"
            hint={
              <Button variant="outline" size="sm" onClick={reload}>
                重试
              </Button>
            }
          />
        ) : items.length === 0 ? (
          <EmptyMark
            className="min-h-56"
            title="还没有角色卡"
            hint={
              <Button size="sm" onClick={create}>
                <Plus />
                新建角色卡
              </Button>
            }
          />
        ) : visible.length === 0 ? (
          <EmptyMark
            className="min-h-56"
            title={`没有名字包含“${q.trim()}”的角色`}
            hint="换个关键词试试"
          />
        ) : (
          <div className={PICK_GRID}>
            {visible.map((card) => (
              <CharacterPickTile
                key={card.id}
                card={card}
                selected={selected.includes(card.id)}
                placed={placedIds.has(card.id)}
                onToggle={() => setSelected((prev) => toggleCharacter(prev, card.id, replace))}
              />
            ))}
          </div>
        )}
      </div>

      <div className="bg-muted/50 flex items-center gap-3 border-t px-4 py-3">
        <span className="text-muted-foreground text-xs">
          {replace ? "单选，替换这个节点" : "可多选，放在双击的位置"}
        </span>
        <Button
          className="ml-auto"
          disabled={summary.disabled}
          onClick={() => {
            // 按勾选顺序交出；列表里已经没有的（别处刚删）直接略过
            const byId = new Map(items.map((card) => [card.id, card]));
            onConfirm(selected.flatMap((id) => byId.get(id) ?? []));
          }}
        >
          {summary.confirmLabel}
        </Button>
      </div>

      {/* 新建完回到选择框并自动选中：单选替换选它，多选追加 */}
      <CharacterEditorDialog
        open={creating}
        character={null}
        onClose={() => setCreating(false)}
        onSaved={(card) => {
          setCreated((prev) => [card, ...prev]);
          setSelected((prev) =>
            prev.includes(card.id) ? prev : toggleCharacter(prev, card.id, replace),
          );
          reload();
        }}
      />
    </>
  );
}
