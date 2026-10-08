import { NEWS_ITEMS } from "@/constants/creation";
import { placeholderBackground } from "@/utils/home/placeholder";

/**
 * 创作页底部的「最近上新」：三张大卡，窄屏横向滑动并吸附。
 * 内容是运营位占位（没有内容来源），卡片不可点。
 */
export function NewsRow() {
  return (
    <section data-slot="news-row" aria-label="最近上新" className="mt-14">
      <h2 className="mb-3 ml-0.5 text-[15px] font-semibold">最近上新</h2>
      <div className="-mx-4 grid snap-x snap-mandatory auto-cols-[82%] grid-flow-col gap-2.5 overflow-x-auto px-4 [scrollbar-width:none] md:mx-0 md:auto-cols-fr md:overflow-visible md:px-0">
        {NEWS_ITEMS.map((item) => (
          <div
            key={item.title}
            style={{ background: placeholderBackground(item.hue) }}
            className="text-cover-foreground relative aspect-[16/10] snap-start overflow-hidden rounded-[18px]"
          >
            <div className="from-cover-scrim absolute inset-0 bg-linear-to-t to-transparent to-60%" />
            <div className="absolute inset-x-4 bottom-3.5">
              <span className="text-[11px] opacity-70">{item.kind}</span>
              <b className="mt-0.5 block text-[17px] leading-snug font-semibold">{item.title}</b>
              <span className="mt-1 block text-xs opacity-75">{item.sub}</span>
            </div>
          </div>
        ))}
      </div>
    </section>
  );
}
