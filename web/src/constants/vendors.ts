import type { IconType } from "@lobehub/icons/es/types";
import Alibaba from "@lobehub/icons/es/Alibaba/components/Color";
import Baidu from "@lobehub/icons/es/Baidu/components/Color";
import ByteDance from "@lobehub/icons/es/ByteDance/components/Color";
import Claude from "@lobehub/icons/es/Claude/components/Color";
import ComfyUI from "@lobehub/icons/es/ComfyUI/components/Color";
import DeepSeek from "@lobehub/icons/es/DeepSeek/components/Color";
import Doubao from "@lobehub/icons/es/Doubao/components/Color";
import ElevenLabs from "@lobehub/icons/es/ElevenLabs/components/Mono";
import Flux from "@lobehub/icons/es/Flux/components/Mono";
import Gemini from "@lobehub/icons/es/Gemini/components/Color";
import Grok from "@lobehub/icons/es/Grok/components/Mono";
import Hailuo from "@lobehub/icons/es/Hailuo/components/Color";
import Hunyuan from "@lobehub/icons/es/Hunyuan/components/Color";
import Ideogram from "@lobehub/icons/es/Ideogram/components/Mono";
import Jimeng from "@lobehub/icons/es/Jimeng/components/Color";
import Kimi from "@lobehub/icons/es/Kimi/components/Color";
import Kling from "@lobehub/icons/es/Kling/components/Color";
import Luma from "@lobehub/icons/es/Luma/components/Color";
import Meta from "@lobehub/icons/es/Meta/components/Color";
import Midjourney from "@lobehub/icons/es/Midjourney/components/Mono";
import Minimax from "@lobehub/icons/es/Minimax/components/Color";
import Mistral from "@lobehub/icons/es/Mistral/components/Color";
import NanoBanana from "@lobehub/icons/es/NanoBanana/components/Color";
import NewAPI from "@lobehub/icons/es/NewAPI/components/Color";
import OpenAI from "@lobehub/icons/es/OpenAI/components/Mono";
import OpenRouter from "@lobehub/icons/es/OpenRouter/components/Color";
import Pika from "@lobehub/icons/es/Pika/components/Mono";
import PixVerse from "@lobehub/icons/es/PixVerse/components/Color";
import Qwen from "@lobehub/icons/es/Qwen/components/Color";
import Recraft from "@lobehub/icons/es/Recraft/components/Mono";
import Runway from "@lobehub/icons/es/Runway/components/Mono";
import Sora from "@lobehub/icons/es/Sora/components/Color";
import Spark from "@lobehub/icons/es/Spark/components/Color";
import Stability from "@lobehub/icons/es/Stability/components/Color";
import Suno from "@lobehub/icons/es/Suno/components/Mono";
import Tencent from "@lobehub/icons/es/Tencent/components/Color";
import Vidu from "@lobehub/icons/es/Vidu/components/Color";
import Volcengine from "@lobehub/icons/es/Volcengine/components/Color";
import Wenxin from "@lobehub/icons/es/Wenxin/components/Color";
import Zhipu from "@lobehub/icons/es/Zhipu/components/Color";

/** 一个厂商：slug 存进模型配置的 vendor 字段，Icon 是它的 logo 组件 */
export type Vendor = { name: string; Icon: IconType };

/**
 * 内置厂商清单：slug -> 名称与 logo。
 * 只深引入每个厂商的 Color / Mono 组件（不走厂商目录的 index，它会连带引入 antd），新增厂商在这里加一行。
 * 有彩色版的用 Color，只有单色版的用 Mono。
 */
export const VENDORS: Record<string, Vendor> = {
  kling: { name: "可灵", Icon: Kling },
  jimeng: { name: "即梦", Icon: Jimeng },
  doubao: { name: "豆包", Icon: Doubao },
  hailuo: { name: "海螺", Icon: Hailuo },
  vidu: { name: "Vidu", Icon: Vidu },
  runway: { name: "Runway", Icon: Runway },
  luma: { name: "Luma", Icon: Luma },
  pika: { name: "Pika", Icon: Pika },
  pixverse: { name: "PixVerse", Icon: PixVerse },
  sora: { name: "Sora", Icon: Sora },
  midjourney: { name: "Midjourney", Icon: Midjourney },
  flux: { name: "Flux", Icon: Flux },
  stability: { name: "Stability AI", Icon: Stability },
  ideogram: { name: "Ideogram", Icon: Ideogram },
  recraft: { name: "Recraft", Icon: Recraft },
  nanobanana: { name: "Nano Banana", Icon: NanoBanana },
  openai: { name: "OpenAI", Icon: OpenAI },
  claude: { name: "Claude", Icon: Claude },
  gemini: { name: "Gemini", Icon: Gemini },
  deepseek: { name: "DeepSeek", Icon: DeepSeek },
  qwen: { name: "通义千问", Icon: Qwen },
  kimi: { name: "Kimi", Icon: Kimi },
  zhipu: { name: "智谱", Icon: Zhipu },
  minimax: { name: "MiniMax", Icon: Minimax },
  hunyuan: { name: "混元", Icon: Hunyuan },
  wenxin: { name: "文心", Icon: Wenxin },
  spark: { name: "讯飞星火", Icon: Spark },
  grok: { name: "Grok", Icon: Grok },
  mistral: { name: "Mistral", Icon: Mistral },
  meta: { name: "Meta", Icon: Meta },
  suno: { name: "Suno", Icon: Suno },
  elevenlabs: { name: "ElevenLabs", Icon: ElevenLabs },
  volcengine: { name: "火山引擎", Icon: Volcengine },
  bytedance: { name: "字节跳动", Icon: ByteDance },
  alibaba: { name: "阿里巴巴", Icon: Alibaba },
  tencent: { name: "腾讯", Icon: Tencent },
  baidu: { name: "百度", Icon: Baidu },
  newapi: { name: "New API", Icon: NewAPI },
  openrouter: { name: "OpenRouter", Icon: OpenRouter },
  comfyui: { name: "ComfyUI", Icon: ComfyUI },
};

/** 按 slug 取厂商；slug 为空或不在清单里返回 undefined（调用方回退首字头像） */
export const vendorOf = (slug: string | undefined): Vendor | undefined =>
  slug && Object.hasOwn(VENDORS, slug) ? VENDORS[slug] : undefined;
