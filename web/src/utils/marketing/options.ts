import type {
  AngleOption,
  MarketingAngle,
  MarketingStep,
  PlatformOption,
  ProductRole,
} from "@/api/marketing/type";

/**
 * 投放平台预设的兜底：/marketing/options 没回来（接口失败、后端还没上线）时生成框照样能用。
 * 取值与设计 §6.5 一致，以后端返回为准。
 */
export const DEFAULT_PLATFORMS: PlatformOption[] = [
  {
    key: "douyin",
    name: "抖音",
    aspectRatio: "9:16",
    language: "zh",
    style: "前 3 秒强钩子，节奏快，口语化",
  },
  {
    key: "xhs",
    name: "小红书",
    aspectRatio: "3:4",
    language: "zh",
    style: "种草口吻、生活方式、质感",
  },
  {
    key: "tmall",
    name: "淘宝天猫",
    aspectRatio: "3:4",
    language: "zh",
    style: "卖点直给，商品居中",
  },
  {
    key: "tiktok",
    name: "TikTok",
    aspectRatio: "9:16",
    language: "en",
    style: "原生 UGC 感",
  },
  {
    key: "amazon",
    name: "亚马逊",
    aspectRatio: "16:9",
    language: "en",
    style: "产品演示为主",
  },
  {
    key: "reels",
    name: "Instagram Reels",
    aspectRatio: "9:16",
    language: "en",
    style: "质感、氛围，旁白少",
  },
  { key: "generic", name: "通用", aspectRatio: "9:16", language: "zh", style: "" },
];

/** 内置创意角度的兜底，同上 */
export const DEFAULT_ANGLES: AngleOption[] = [
  { key: "pain", name: "痛点-解决", desc: "痛点钩子 → 商品解决 → 效果证明 → 促单" },
  { key: "feature", name: "卖点拆解", desc: "特写逐个展示主卖点 + 2 个辅助卖点" },
  { key: "lifestyle", name: "生活方式种草", desc: "真实使用场景，口吻像朋友推荐" },
  { key: "compare", name: "前后对比", desc: "用之前 / 用之后对照，不点名竞品" },
  { key: "unbox", name: "开箱体验", desc: "手持第一视角拆包、看细节、上手" },
  { key: "mood", name: "质感大片", desc: "光影、材质、慢动作，旁白极少" },
];

/** 角度的展示名；跟拍脚本的角度固定为 follow */
export function angleName(key: MarketingAngle | "follow", angles: AngleOption[]): string {
  if (key === "follow") return "跟拍";
  return angles.find((item) => item.key === key)?.name ?? key;
}

/** 环节的短名（环节进度条的小标签） */
export const STEP_NAME: Record<MarketingStep, string> = {
  facts: "事实卡",
  reverse: "拆解",
  follow: "跟拍",
  create: "创意",
};

/** 商品出现方式的展示名 */
export const PRODUCT_ROLE_NAME: Record<ProductRole, string> = {
  none: "不出现",
  dynamic: "动态出镜",
  hero_real: "真实质感特写",
  package_text: "包装文字清晰",
};

/** 平台图标：用 lucide 图标名映射在组件里做，这里只放展示名 */
export const platformName = (key: string, platforms: PlatformOption[]): string =>
  platforms.find((item) => item.key === key)?.name ?? key;
