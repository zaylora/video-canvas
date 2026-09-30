/**
 * 新建模型时编辑器里的起始内容，结构见 backend/docs/admin-ai-api.md「模型配置正文」。
 * channels 首期必须恰好一项；params 原样交给插件解释；input_schema 的书写顺序就是画布的渲染顺序。
 */

/** 文本模型（插件 endpoint 为 sync：一次请求出正文） */
export const TEXT_MODEL_TEMPLATE = {
  key: "",
  kind: "text",
  label: "",
  hint: "",
  credits: 1,
  deadline: "5m",
  enabled: false,
  sort: 100,
  channels: [{ channel: "", upstream_model: "" }],
  params: { max_tokens: 2000 },
  input_schema: {
    prompt: {
      type: "text",
      label: "提示词",
      required: true,
      max_length: 8000,
      port: "text",
    },
  },
};

/** 视频模型（插件 endpoint 为 async：提交后轮询） */
export const VIDEO_MODEL_TEMPLATE = {
  key: "",
  kind: "video",
  label: "",
  hint: "",
  credits: 10,
  deadline: "30m",
  enabled: false,
  sort: 100,
  channels: [{ channel: "", upstream_model: "" }],
  params: {},
  input_schema: {
    prompt: {
      type: "text",
      label: "提示词",
      required: true,
      max_length: 2000,
      port: "text",
    },
    image: { type: "image", label: "首帧", port: "image" },
    duration: {
      type: "enum",
      label: "时长",
      options: [
        { value: 5, label: "5 秒" },
        { value: 10, label: "10 秒" },
      ],
      default: 5,
    },
  },
};

export const MODEL_TEMPLATES = [
  { id: "text", label: "文本模型（同步）", body: TEXT_MODEL_TEMPLATE },
  { id: "video", label: "视频模型（异步）", body: VIDEO_MODEL_TEMPLATE },
] as const;
