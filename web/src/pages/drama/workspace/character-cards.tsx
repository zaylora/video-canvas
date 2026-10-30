import { UserRound } from "lucide-react";

import { cardDisplay, type CharacterCard } from "@/utils/drama/parse-characters";

/** 按分组归拢，分组按第一次出现的顺序 */
function byGroup(cards: CharacterCard[]) {
  const groups = new Map<string, CharacterCard[]>();
  for (const card of cards) groups.set(card.group, [...(groups.get(card.group) ?? []), card]);
  return [...groups];
}

/**
 * 人物传记的卡片视图：每个人物同一个模样（同一个头像、同样的排版），只有名字和资料不同；
 * 固定两列铺满内容区（人少的分组也不在右边留空列），资料的标签和内容分两列，按原文顺序；
 * 别称放在名字下面，和名字一样的「正式名称」不重复列出。分组有名字时在组上方标出。
 */
export function CharacterCards({ cards }: { cards: CharacterCard[] }) {
  return (
    <div className="mt-5 flex flex-col gap-7">
      {byGroup(cards).map(([group, members]) => (
        <section key={group || "_"} className="flex flex-col gap-3">
          {group && (
            <h3 className="text-muted-foreground px-1 text-xs font-medium tracking-[0.12em]">
              {group}
            </h3>
          )}
          <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
            {members.map((card, index) => (
              <CharacterCardView key={`${card.name}-${index}`} card={card} />
            ))}
          </div>
        </section>
      ))}
    </div>
  );
}

function CharacterCardView({ card }: { card: CharacterCard }) {
  const { name, subtitle, fields } = cardDisplay(card);
  return (
    <article
      data-slot="character-card"
      className="bg-chrome ring-chrome-border flex min-w-0 flex-col gap-4 rounded-3xl p-5 ring-1"
    >
      <header className="flex items-center gap-3">
        <span className="bg-chrome-hover ring-chrome-border text-muted-foreground grid size-11 shrink-0 place-items-center rounded-full ring-1">
          <UserRound className="size-5" aria-hidden />
        </span>
        <div className="min-w-0">
          <h4 className="font-display truncate text-lg font-black tracking-wide">{name}</h4>
          {subtitle && (
            <p
              className="text-muted-foreground line-clamp-2 text-xs leading-relaxed"
              title={subtitle}
            >
              {subtitle}
            </p>
          )}
        </div>
      </header>
      {fields.length > 0 && (
        <dl className="grid grid-cols-[minmax(3.5em,max-content)_minmax(0,1fr)] gap-x-4 gap-y-2.5 text-sm leading-relaxed">
          {fields.map((field, index) => (
            <div key={`${field.label}-${index}`} className="contents">
              <dt className="text-muted-foreground text-xs leading-6 whitespace-nowrap">
                {field.label}
              </dt>
              <dd className="m-0 min-w-0 break-words">{field.value}</dd>
            </div>
          ))}
        </dl>
      )}
      {card.notes.map((note, index) => (
        <p key={index} className="text-sm leading-relaxed break-words">
          {note}
        </p>
      ))}
    </article>
  );
}
