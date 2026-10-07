import type { Capabilities, Pricing, RefSpec } from "@/api/model/type";

const REF_OFF: RefSpec = { on: false, max: 0, max_mb: 0 };

/**
 * 新建模型时按种类预填的能力（运营可改，后端不依赖它，只用固定上下限校验）。
 * 数值是常见平台的合理起点，不代表某个平台的真实上限；填错了在「测试模型」里由上游报错暴露。
 * 视频、图片带「生成数量」参数（fanout）：一次提交复制出 N 个节点、拆成 N 个任务。
 */
export function defaultCapabilities(kind: string): Capabilities {
  switch (kind) {
    case "video":
      return {
        ops: ["t2v", "i2v", "omni"],
        refs: {
          image: { on: true, max: 4, max_mb: 10 },
          audio: { on: true, max: 1, max_mb: 15 },
          video: { on: true, max: 1, max_mb: 100 },
        },
        prompt: { max_length: 2000 },
        params: {
          aspect_ratio: {
            type: "enum",
            label: "比例",
            open: true,
            options: ["Auto", "16:9", "4:3", "1:1", "3:4", "9:16", "21:9"],
            default: "Auto",
          },
          resolution: {
            type: "enum",
            label: "清晰度",
            open: true,
            options: ["720P", "1080P", "4K"],
            default: "720P",
            spec: true,
          },
          duration: {
            type: "number",
            label: "视频时长",
            open: true,
            min: 5,
            max: 10,
            step: 1,
            default: 5,
            unit: "秒",
          },
          generate_audio: {
            type: "boolean",
            label: "生成音频",
            open: true,
            default: true,
            spec: true,
          },
          count: {
            type: "enum",
            label: "生成数量",
            open: true,
            options: [1, 2, 4],
            default: 1,
            fanout: true,
            unit: "个",
          },
        },
      };
    case "image":
      return {
        ops: ["t2i", "i2i"],
        refs: { image: { on: true, max: 4, max_mb: 10 }, audio: REF_OFF, video: REF_OFF },
        prompt: { max_length: 1500 },
        params: {
          aspect_ratio: {
            type: "enum",
            label: "比例",
            open: true,
            options: ["Auto", "1:1", "16:9", "9:16", "4:3", "3:4"],
            default: "1:1",
          },
          resolution: {
            type: "enum",
            label: "清晰度",
            open: true,
            options: ["1K", "2K", "4K"],
            default: "1K",
            spec: true,
          },
          count: {
            type: "enum",
            label: "生成数量",
            open: true,
            options: [1, 2, 4],
            default: 1,
            fanout: true,
            unit: "张",
          },
        },
      };
    case "audio":
      return {
        refs: { image: REF_OFF, audio: REF_OFF, video: REF_OFF },
        prompt: { max_length: 4096 },
        params: {},
      };
    case "agent":
      // 画布 Agent 的对话大模型：没有生成方式、素材、参数，只有上下文和能不能看图
      return {
        refs: { image: REF_OFF, audio: REF_OFF, video: REF_OFF },
        prompt: { max_length: 20000 },
        params: {},
        context: { window: 200000, output: 8192 },
        vision: true,
      };
    default:
      return {
        refs: { image: REF_OFF, audio: REF_OFF, video: REF_OFF },
        prompt: { max_length: 20000 },
        params: {},
        context: { window: 128000, output: 8192 },
      };
  }
}

/**
 * 新建模型时按种类预填的定价（运营可改）：视频按秒、按清晰度与音频分档；图片按次、按清晰度分档；
 * 文本按 Token；音频按次。数值只是起点，和能力模板里的参数名对应。
 */
export function defaultPricing(kind: string): Pricing {
  switch (kind) {
    case "video":
      return {
        billing: "per_second",
        per_second: 2,
        tiers: [
          { on: true, when: { generate_audio: true }, unit: 3 },
          { on: true, when: { resolution: "1080P" }, unit: 4 },
          { on: true, when: { resolution: "1080P", generate_audio: true }, unit: 5 },
          { on: true, when: { resolution: "4K" }, unit: 8 },
        ],
      };
    case "image":
      return {
        billing: "per_call",
        unit: 4,
        tiers: [
          { on: true, when: { resolution: "2K" }, unit: 8 },
          { on: true, when: { resolution: "4K" }, unit: 16 },
        ],
      };
    case "text":
      return { billing: "token", token: { in: 2, out: 8 } };
    case "agent":
      return { billing: "token", token: { in: 3, out: 15 } };
    default:
      return { billing: "per_call", unit: 2 };
  }
}
