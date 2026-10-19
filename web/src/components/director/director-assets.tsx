import { useEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from "react";
import { motion } from "motion/react";
import { toast } from "sonner";
import { Aperture, Box, PanelLeftClose, Search, UserRound, Video } from "lucide-react";

import { DURATION, EASE_OUT, TAP_CARD } from "@/lib/motion";
import { cn } from "@/lib/utils";
import {
  useDirectorStore,
  type AddOptions,
  type AssetTab,
  type DirectorAddKind,
} from "@/store/director";
import type { FigureModel, PropShape } from "@/types";
import { assetThumbs, characterThumbKey, propThumbKey } from "@/utils/director/asset-thumbs";
import { FIGURE_MODELS } from "@/utils/director/figure";
import {
  PROP_CATEGORIES,
  type PropCategory,
  propLabel,
  shapesOf,
} from "@/utils/director/prop-presets";
import { FIGURE_LABELS, canAdd, limitMessage } from "@/utils/director/scene";

/** 一张资产卡片：点一下就往场景里加一个 */
type Card = {
  id: string;
  name: string;
  /** 分类名，搜索也认它 */
  category: string;
  /** 卡片上的小标签，预设资产标「预设」 */
  tag: string;
  kind: DirectorAddKind;
  options?: AddOptions;
  /** 点击时的提示，缺省是「添加某某」 */
  hint?: string;
  /** 不是往场景里加对象的卡片（当前视角机位）：点击时执行这个 */
  run?: () => void;
  /** 预览图的 key；没有就画图标 */
  thumbKey?: string;
  icon: ReactNode;
};

const TABS: { id: AssetTab; label: string; icon: ReactNode }[] = [
  { id: "character", label: "人物", icon: <UserRound className="size-4" /> },
  { id: "prop", label: "物体", icon: <Box className="size-4" /> },
  { id: "camera", label: "机位", icon: <Video className="size-4" /> },
];

/** 分类小标签：全部 + 各分类 */
const ALL = "all";

const characterCards = (): Card[] =>
  FIGURE_MODELS.map((model: FigureModel) => ({
    id: model,
    name: FIGURE_LABELS[model],
    category: model,
    tag: "预设",
    kind: "character",
    options: { model },
    thumbKey: characterThumbKey(model),
    icon: <UserRound className="size-8" />,
  }));

const propCard = (shape: PropShape, category: string): Card => ({
  id: shape,
  name: propLabel(shape),
  category,
  tag: "预设",
  kind: "prop",
  options: { shape },
  thumbKey: propThumbKey(shape),
  icon: <Box className="size-8" />,
});

/**
 * 资产面板（设计稿 docs/design/3D导演台）：左侧可折叠，三个标签页——人物、物体、机位；
 * 每页有搜索和分类小标签，点卡片往场景里加一个并选中。预览图在浏览器里现拍。
 * @param props.onCameraFromView 「当前视角机位」：把导演视角直接变成一台机位
 */
export function DirectorAssets({ onCameraFromView }: { onCameraFromView: () => void }) {
  const tab = useDirectorStore((state) => state.assetTab);
  const scene = useDirectorStore((state) => state.scene);
  const [query, setQuery] = useState("");
  /** 每个标签页各记各的分类选择 */
  const [filters, setFilters] = useState<Record<AssetTab, string>>({
    character: ALL,
    prop: ALL,
    camera: ALL,
  });
  const filter = filters[tab];
  const text = query.trim().toLowerCase();

  const sections = useMemo(() => {
    const matches = (card: Card) =>
      !text || card.name.toLowerCase().includes(text) || card.category.toLowerCase().includes(text);
    if (tab === "character") {
      const cards = characterCards().filter(
        (card) => (filter === ALL || card.id === filter) && matches(card),
      );
      return cards.length ? [{ title: null as string | null, cards }] : [];
    }
    if (tab === "prop") {
      return PROP_CATEGORIES.filter((c) => filter === ALL || c.id === filter)
        .map((c) => ({
          title: filter === ALL ? c.label : null,
          cards: shapesOf(c.id)
            .map((shape) => propCard(shape, c.label))
            .filter(matches),
        }))
        .filter((section) => section.cards.length > 0);
    }
    const cameraCards: Card[] = [
      {
        id: "new",
        name: "新建机位",
        category: "机位",
        tag: "预设",
        kind: "camera",
        icon: <Video className="size-8" />,
      },
      {
        id: "view",
        name: "当前视角机位",
        category: "机位",
        tag: "预设",
        kind: "camera",
        hint: "把当前导演视角变成一台机位",
        run: onCameraFromView,
        icon: <Aperture className="size-8" />,
      },
    ];
    const cards = cameraCards.filter(matches);
    return cards.length ? [{ title: null as string | null, cards }] : [];
  }, [filter, onCameraFromView, tab, text]);

  const chips: { id: string; label: string }[] =
    tab === "character"
      ? [
          { id: ALL, label: "全部" },
          ...FIGURE_MODELS.map((m) => ({ id: m, label: FIGURE_LABELS[m] })),
        ]
      : tab === "prop"
        ? [
            { id: ALL, label: "全部" },
            ...PROP_CATEGORIES.map((c: { id: PropCategory; label: string }) => ({
              id: c.id,
              label: c.label,
            })),
          ]
        : [];

  const add = (card: Card) => {
    if (card.run) return card.run();
    const message = useDirectorStore.getState().addObject(card.kind, card.options);
    if (message) toast(message);
  };

  return (
    <motion.aside
      aria-label="资产"
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      transition={{ duration: DURATION.base, ease: EASE_OUT }}
      className="border-border bg-card/60 flex min-h-0 w-full min-w-0 flex-col border-r"
    >
      <div className="flex h-11 shrink-0 items-center gap-2 px-3">
        <Box className="text-muted-foreground size-4" />
        <span className="text-sm font-semibold">资产</span>
        <button
          type="button"
          title="折叠资产面板"
          aria-label="折叠资产面板"
          onClick={() => useDirectorStore.getState().toggleAssets()}
          className="text-muted-foreground hover:bg-muted hover:text-foreground focus-visible:ring-ring ml-auto inline-flex size-7 items-center justify-center rounded-md outline-none focus-visible:ring-2"
        >
          <PanelLeftClose className="size-4" />
        </button>
      </div>

      <div
        role="tablist"
        aria-label="资产类型"
        className="border-border grid shrink-0 grid-cols-3 border-b"
      >
        {TABS.map((item) => (
          <button
            key={item.id}
            type="button"
            role="tab"
            aria-selected={tab === item.id}
            title={item.label}
            onClick={() => useDirectorStore.getState().setAssetTab(item.id)}
            className={cn(
              "text-muted-foreground hover:text-foreground focus-visible:ring-ring relative flex h-10 items-center justify-center gap-1.5 text-xs outline-none focus-visible:ring-2",
              tab === item.id && "text-foreground",
            )}
          >
            {item.icon}
            {item.label}
            {tab === item.id && (
              <motion.span
                layoutId="director-asset-tab"
                className="bg-foreground absolute right-3 bottom-0 left-3 h-0.5 rounded-full"
              />
            )}
          </button>
        ))}
      </div>

      <div className="shrink-0 space-y-2.5 p-3">
        <label className="border-input bg-background focus-within:ring-ring flex h-9 items-center gap-2 rounded-lg border px-2.5 focus-within:ring-2">
          <Search className="text-muted-foreground size-4 shrink-0" />
          <input
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="搜索素材"
            className="placeholder:text-muted-foreground min-w-0 flex-1 bg-transparent text-xs outline-none"
          />
        </label>
        {chips.length > 0 && (
          <div className="flex flex-wrap gap-1.5">
            {chips.map((chip) => (
              <button
                key={chip.id}
                type="button"
                aria-pressed={filter === chip.id}
                onClick={() => setFilters((prev) => ({ ...prev, [tab]: chip.id }))}
                className={cn(
                  "text-muted-foreground hover:text-foreground focus-visible:ring-ring h-7 rounded-md px-2.5 text-xs outline-none focus-visible:ring-2",
                  filter === chip.id && "bg-muted text-foreground",
                )}
              >
                {chip.label}
              </button>
            ))}
          </div>
        )}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-3 pb-3">
        {sections.length === 0 ? (
          <p className="text-muted-foreground py-8 text-center text-xs">没有匹配的素材</p>
        ) : (
          sections.map((section) => (
            <section key={section.title ?? "list"} className="mb-3">
              {section.title && (
                <h3 className="text-muted-foreground mb-1.5 text-xs">{section.title}</h3>
              )}
              <div className="grid grid-cols-2 gap-2">
                {section.cards.map((card) => {
                  const blocked = !!scene && !canAdd(scene, card.kind);
                  return (
                    <AssetCard
                      key={card.id}
                      card={card}
                      disabled={blocked}
                      title={blocked ? limitMessage(card.kind) : (card.hint ?? `添加${card.name}`)}
                      onClick={() => add(card)}
                    />
                  );
                })}
              </div>
            </section>
          ))
        )}
      </div>
    </motion.aside>
  );
}

function AssetCard({
  card,
  disabled,
  title,
  onClick,
}: {
  card: Card;
  disabled: boolean;
  title: string;
  onClick: () => void;
}) {
  return (
    <motion.button
      type="button"
      title={title}
      disabled={disabled}
      whileTap={disabled ? undefined : TAP_CARD}
      onClick={onClick}
      className="border-border bg-card hover:bg-muted/50 focus-visible:ring-ring flex flex-col gap-2 rounded-xl border p-2 text-left outline-none focus-visible:ring-2 disabled:cursor-not-allowed disabled:opacity-40"
    >
      <div className="bg-muted/60 text-muted-foreground relative grid aspect-square place-items-center overflow-hidden rounded-lg">
        {card.thumbKey ? <Thumb thumbKey={card.thumbKey} fallback={card.icon} /> : card.icon}
      </div>
      <div className="min-w-0 px-0.5">
        <div className="truncate text-xs font-medium">{card.name}</div>
        <div className="text-muted-foreground text-[11px]">{card.tag}</div>
      </div>
    </motion.button>
  );
}

/** 预览图：现拍的缩略图，没拍出来前是占位，拍失败退回图标 */
function Thumb({ thumbKey, fallback }: { thumbKey: string; fallback: ReactNode }) {
  const state = useSyncExternalStore(
    assetThumbs.subscribe,
    () => assetThumbs.get(thumbKey),
    () => assetThumbs.get(thumbKey),
  );
  useEffect(() => {
    void assetThumbs.request(thumbKey);
  }, [thumbKey]);
  if (state.status === "ready") {
    return <img src={state.url} alt="" draggable={false} className="size-full object-contain" />;
  }
  if (state.status === "failed") return fallback;
  return <span className="bg-muted-foreground/10 size-full animate-pulse" />;
}
