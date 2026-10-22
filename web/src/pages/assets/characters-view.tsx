import { useMemo, useState } from "react";
import { createPortal } from "react-dom";
import { Plus, UserRoundPlus } from "lucide-react";
import { toast } from "sonner";

import { deleteCharacter } from "@/api/character";
import type { CharacterDto } from "@/api/character/type";
import { ASSET_GRID } from "@/components/assets/asset-grid";
import { AssetSearch } from "@/components/assets/asset-search";
import { CharacterCard } from "@/components/characters/character-card";
import { CharacterEditorDialog } from "@/components/characters/character-editor-dialog";
import { EmptyMark } from "@/components/home/empty-mark";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useCharacterList } from "@/hooks/use-characters";
import { useCharactersStore } from "@/store/characters";
import { filterCharacters } from "@/utils/characters/picker-logic";

/** 编辑弹框的目标：null 是关闭，"new" 是新建，角色卡是编辑它 */
type Editing = CharacterDto | "new" | null;

/**
 * 资产页「角色卡」范围（设计 §6.4）：3:4 竖版卡片网格，第一格是虚线「新建角色卡」；
 * 右上搜索框按名称过滤（列表最多 100 张，不分页，客户端过滤），「新建角色卡」按钮；
 * 卡片 ⋯ 菜单编辑 / 删除，点封面直接编辑。保存、删除后写进角色卡缓存，已打开画布里的节点自动刷新。
 * @param searchSlot 头部右侧的容器，搜索框和新建按钮用 portal 放进去
 */
export function CharactersView({ searchSlot }: { searchSlot: HTMLElement | null }) {
  const [q, setQ] = useState("");
  const { status, items, reload } = useCharacterList();
  const [editing, setEditing] = useState<Editing>(null);
  const [deleting, setDeleting] = useState<CharacterDto | null>(null);

  const visible = useMemo(() => filterCharacters(items, q), [items, q]);
  const searching = q.trim() !== "";

  const confirmDelete = async () => {
    const target = deleting;
    if (!target) return;
    setDeleting(null);
    try {
      await deleteCharacter(target.id);
    } catch {
      // 失败提示由请求层弹出，列表不动
      return;
    }
    useCharactersStore.getState().remove(target.id);
    toast.success(`已删除「${target.name}」`);
    reload();
  };

  return (
    <div className="relative min-h-[60vh]">
      {searchSlot &&
        createPortal(
          <>
            <AssetSearch value={q} onChange={setQ} placeholder="搜索角色" />
            <Button size="sm" onClick={() => setEditing("new")}>
              <Plus />
              新建角色卡
            </Button>
          </>,
          searchSlot,
        )}

      <h2 className="mt-4 mb-4.5 flex h-8 items-center text-base font-semibold">角色卡</h2>

      {status === "loading" ? (
        <div aria-busy data-slot="character-grid-skeleton" className={ASSET_GRID}>
          {Array.from({ length: 10 }, (_, i) => (
            <Skeleton key={i} className="aspect-3/4 rounded-xl motion-reduce:animate-none" />
          ))}
        </div>
      ) : status === "error" ? (
        <EmptyMark
          title="加载失败"
          hint={
            <Button variant="outline" size="sm" onClick={reload}>
              重试
            </Button>
          }
        />
      ) : items.length === 0 ? (
        <EmptyMark
          title="还没有角色卡"
          hint={
            <span className="flex flex-col items-center gap-3">
              把一个人物的形象和声音存成角色卡，之后在画布里一步引入
              <Button size="sm" onClick={() => setEditing("new")}>
                <UserRoundPlus />
                新建角色卡
              </Button>
            </span>
          }
        />
      ) : visible.length === 0 ? (
        <EmptyMark title={`没有名字包含“${q.trim()}”的角色`} hint="换个关键词试试" />
      ) : (
        <div data-slot="character-grid" className={ASSET_GRID}>
          {!searching && (
            <button
              type="button"
              onClick={() => setEditing("new")}
              className="text-muted-foreground hover:bg-muted/60 hover:text-foreground focus-visible:ring-ring/50 flex aspect-3/4 flex-col items-center justify-center gap-2 rounded-xl border border-dashed text-[13px] transition-colors outline-none focus-visible:ring-3"
            >
              <span className="bg-muted grid size-9 place-items-center rounded-full">
                <Plus className="size-4" />
              </span>
              新建角色卡
            </button>
          )}
          {visible.map((card) => (
            <CharacterCard
              key={card.id}
              card={card}
              onEdit={() => setEditing(card)}
              onDelete={() => setDeleting(card)}
            />
          ))}
        </div>
      )}

      <CharacterEditorDialog
        open={editing !== null}
        character={editing === "new" ? null : editing}
        onClose={() => setEditing(null)}
        onSaved={reload}
      />

      <AlertDialog open={deleting !== null} onOpenChange={(open) => !open && setDeleting(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除「{deleting?.name}」？</AlertDialogTitle>
            <AlertDialogDescription>
              引用它的画布节点会显示「已失效」，不再参与生成，可以在节点上重新选择。它用到的图片和音频仍保留在资产库。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={() => void confirmDelete()}>
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
