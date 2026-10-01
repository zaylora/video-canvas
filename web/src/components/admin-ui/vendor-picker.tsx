import { useState } from "react";
import { Check, ChevronDown } from "lucide-react";

import { VendorAvatar } from "@/components/admin-ui/vendor-avatar";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { VENDORS, vendorOf } from "@/constants/vendors";
import { cn } from "@/lib/utils";

/**
 * 厂商选择器：一个显示当前 logo + 厂商名的按钮，点开是带搜索的 logo 网格。
 * 第一项「不选」表示用首字头像；value 是厂商 slug，清单外的 slug 当作没选。
 */
function VendorPicker({
  id,
  value,
  name,
  seed,
  onChange,
}: {
  id?: string;
  /** 当前厂商 slug，空串表示没选 */
  value: string;
  /** 模型名，没选厂商时首字头像用 */
  name: string;
  seed?: string;
  onChange: (slug: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const current = vendorOf(value);
  const keyword = query.trim().toLowerCase();
  const list = Object.entries(VENDORS).filter(
    ([slug, vendor]) =>
      !keyword || slug.includes(keyword) || vendor.name.toLowerCase().includes(keyword),
  );
  const pick = (slug: string) => {
    onChange(slug);
    setOpen(false);
  };

  return (
    <Popover
      open={open}
      onOpenChange={(next) => {
        setOpen(next);
        if (!next) setQuery("");
      }}
    >
      <PopoverTrigger
        render={<Button id={id} variant="outline" className="w-full justify-start gap-2.5" />}
      >
        <VendorAvatar
          vendor={value}
          name={name}
          seed={seed}
          className="size-6 rounded-md text-xs"
        />
        <span className="truncate">{current?.name ?? "不选（用首字头像）"}</span>
        <ChevronDown className="ml-auto opacity-60" />
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 gap-2 p-2">
        <Input
          autoFocus
          value={query}
          placeholder="搜索厂商"
          aria-label="搜索厂商"
          onChange={(event) => setQuery(event.target.value)}
        />
        <div className="max-h-64 overflow-y-auto">
          {!keyword && (
            <button
              type="button"
              className={cn(
                "hover:bg-accent mb-1 flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm",
                !current && "bg-accent",
              )}
              onClick={() => pick("")}
            >
              <VendorAvatar name={name} seed={seed} className="size-6 rounded-md text-xs" />
              不选（用首字头像）
              {!current && <Check className="ml-auto size-4" />}
            </button>
          )}
          <div className="grid grid-cols-3 gap-1">
            {list.map(([slug, vendor]) => (
              <button
                key={slug}
                type="button"
                title={vendor.name}
                className={cn(
                  "hover:bg-accent flex min-w-0 flex-col items-center gap-1 rounded-md p-2 text-xs",
                  slug === value && "bg-accent ring-foreground/20 ring-1",
                )}
                onClick={() => pick(slug)}
              >
                <VendorAvatar vendor={slug} name={vendor.name} className="size-8" />
                <span className="w-full truncate text-center">{vendor.name}</span>
              </button>
            ))}
          </div>
          {list.length === 0 && (
            <p className="text-muted-foreground py-6 text-center text-xs">没有匹配的厂商</p>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

export { VendorPicker };
