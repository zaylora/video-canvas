import type { Capabilities, RefSpec } from "@/api/model/type";

const REF_OFF: RefSpec = { on: false, max: 0, max_mb: 0 };

/**
 * 新建模型时按种类预填的能力（运营可改，后端不依赖它，只用固定上下限校验）。
 * 数值是常见平台的合理起点，不代表某个平台的真实上限；填错了在「测试模型」里由上游报错暴露。
 * 「生成数量」参数（fanout）随第三期的复制节点一起上线，这里先不预填。
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
        },
      };
    case "audio":
      return {
        refs: { image: REF_OFF, audio: REF_OFF, video: REF_OFF },
        prompt: { max_length: 4096 },
        params: {},
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
