import { defaultCapabilities, defaultPricing } from "@/utils/admin/model-template";

/**
 * 新建模型时编辑器里的起始内容，结构见 backend/docs/admin-ai-api.md「模型配置正文」。
 * channels 首期必须恰好一项；params 原样交给插件解释；capabilities 是模型能力（生成方式、参考素材、
 * 提示词上限、生成参数，参数的书写顺序就是画布参数面板的显示顺序）与 pricing（定价）按种类预填，运营可改。
 */

/** 文本模型（插件 endpoint 为 sync：一次请求出正文） */
export const TEXT_MODEL_TEMPLATE = {
  key: "",
  kind: "text",
  label: "",
  hint: "",
  deadline: "5m",
  enabled: false,
  sort: 100,
  channels: [{ channel: "", upstream_model: "" }],
  params: {},
  capabilities: defaultCapabilities("text"),
  pricing: defaultPricing("text"),
};

/** 视频模型（插件 endpoint 为 async：提交后轮询） */
export const VIDEO_MODEL_TEMPLATE = {
  key: "",
  kind: "video",
  label: "",
  hint: "",
  deadline: "30m",
  enabled: false,
  sort: 100,
  channels: [{ channel: "", upstream_model: "" }],
  params: {},
  capabilities: defaultCapabilities("video"),
  pricing: defaultPricing("video"),
};

/** 图片模型（插件 endpoint 为 sync：一次请求出图） */
export const IMAGE_MODEL_TEMPLATE = {
  key: "",
  kind: "image",
  label: "",
  hint: "",
  deadline: "10m",
  enabled: false,
  sort: 100,
  channels: [{ channel: "", upstream_model: "" }],
  params: {},
  capabilities: defaultCapabilities("image"),
  pricing: defaultPricing("image"),
};

/** 画布 Agent 的对话大模型（不走插件钩子：后端网关直接请求渠道的 OpenAI 兼容接口，渠道插件需要 Bearer 鉴权） */
export const AGENT_MODEL_TEMPLATE = {
  key: "",
  kind: "agent",
  label: "",
  hint: "",
  deadline: "5m",
  enabled: false,
  sort: 100,
  channels: [{ channel: "", upstream_model: "" }],
  capabilities: defaultCapabilities("agent"),
  pricing: defaultPricing("agent"),
};

export const MODEL_TEMPLATES = [
  { id: "text", label: "文本模型（同步）", body: TEXT_MODEL_TEMPLATE },
  { id: "video", label: "视频模型（异步）", body: VIDEO_MODEL_TEMPLATE },
  { id: "image", label: "图片模型（同步）", body: IMAGE_MODEL_TEMPLATE },
  { id: "agent", label: "Agent 模型（画布对话）", body: AGENT_MODEL_TEMPLATE },
] as const;
