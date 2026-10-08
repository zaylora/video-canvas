import type { Conversation } from "@/types";
import { formatDayTitle } from "@/utils/home/placeholder";

/**
 * 对话页的样例数据：对话接口还没有，页面先用它把样式铺出来。
 * 接口就绪后删掉这个文件，侧栏和对话页改读接口；这里的内容只是占位，不代表真实用户数据。
 * @param now 当前时间，决定「今天」的日期标题
 * @returns 两段样例对话：置顶的「默认创作」和一段 Agent 对话
 */
export const buildSampleConversations = (now: Date = new Date()): Conversation[] => {
  const today = formatDayTitle(now);
  const earlier = formatDayTitle(new Date(now.getTime() - 2 * 24 * 60 * 60 * 1000));

  return [
    {
      id: "default",
      title: "默认创作",
      pinned: true,
      hue: null,
      records: [
        {
          id: "default-1",
          day: earlier,
          mode: "image",
          prompt: "冬日雪地里，苍白妆容的女人缓缓回头，冷色调时尚大片",
          meta: ["自动选模型", "1:1", "高清"],
          refHues: [],
          results: [
            { kind: "image", shape: "square", hue: 215 },
            { kind: "image", shape: "square", hue: 245 },
          ],
          progress: null,
        },
        {
          id: "default-2",
          day: today,
          mode: "video",
          prompt: "粉发女孩倚在旧楼门框边，红裙，冷灰墙面，镜头缓慢推近",
          meta: ["自动选模型", "5s", "720p"],
          refHues: [340, 20],
          results: [{ kind: "video", shape: "wide", hue: 345, duration: "0:05" }],
          progress: null,
        },
        {
          id: "default-3",
          day: today,
          mode: "image",
          prompt: "霓虹灯牌下的雨夜街角，湿漉漉的地面反射着紫色和青色的光",
          meta: ["自动选模型", "16:9", "高清"],
          refHues: [],
          results: [
            { kind: "image", shape: "square", hue: 285 },
            { kind: "image", shape: "square", hue: 190 },
          ],
          progress: 46,
        },
      ],
    },
    {
      id: "neon-storyboard",
      title: "雨夜霓虹分镜",
      hue: 275,
      records: [
        {
          id: "neon-1",
          day: today,
          mode: "agent",
          prompt: "/分镜脚本 雨夜的城市，一个外卖骑手在霓虹里穿行，最后停在一扇亮着灯的窗前",
          meta: ["自动选模型", "4 个分镜"],
          refHues: [],
          reply:
            "拆成了 4 个分镜：俯瞰车流 → 骑手穿过路口 → 霓虹倒影 → 停在窗前。先出了分镜图，确认后我再逐个生成视频。",
          results: [275, 310, 200, 40].map((hue, index) => ({
            kind: "image" as const,
            shape: "wide" as const,
            hue,
            small: true,
            shot: index + 1,
          })),
          next: "生成 4 段视频",
          progress: null,
        },
      ],
    },
  ];
};
