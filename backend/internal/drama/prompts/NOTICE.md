# 短剧提示词与风格资源来源说明

本目录提示词及 `../styles.json` 的中文描述由 video-canvas 项目重新撰写，适配本项目的 JSON 字段、校验、短剧流水线与画布节点，提示词正文不包含第三方原文（内置技能见下一段）。以下开源项目提供流程与字段设计参考；题材和画风分类名称参考 Toonflow，描述为原创改写。确认许可证日期：2026-10-28。

2026-11-01 起，`../skills/` 下原样内置了以下开源技能（含子技能与 references，各目录保留原 LICENSE / NOTICE，内容未改动），流水线按步骤和目标模型挑选其中文件拼进提示词作为参考资料，输出格式仍以本目录提示词为准：

- `skills/zenstory-drama-skills/`：<https://github.com/zenstory-ai/drama-skills> 的 `skills/` 全部 11 个技能；MIT License，Copyright (c) 2026 drama-skills contributors。
- `skills/0xsline-short-drama/`：<https://github.com/0xsline/short-drama> 的 SKILL.md 与 references；MIT License，Copyright (c) 2025 0xsline。
- `skills/smixs-visual-skills/`：<https://github.com/smixs/visual-skills> 的 `image/` 与 `video/`；CC BY 4.0（<https://creativecommons.org/licenses/by/4.0/>），署名：Serge Shima — https://t.me/aimastersme · https://sergeshima.com · https://aimasters.me，Source: https://github.com/smixs/visual-skills。本项目只摘取其中章节作为提示词参考资料，未修改原文件。
- `skills/higgsfield-ai-prompt-skill/`：<https://github.com/OSideMedia/higgsfield-ai-prompt-skill> 的 `image-models.md`（仅此一个文件）；MIT License，Copyright (c) 2026 O-Side Media。

- Toonflow-app：<https://github.com/HBAI-Ltd/Toonflow-app>；流程、题材和画风分类参考；MIT License。版权声明：Copyright (c) 2026 HBAI-Ltd。许可证原文：<https://github.com/HBAI-Ltd/Toonflow-app/blob/master/LICENSE>。
- drama-skills：<https://github.com/zenstory-ai/drama-skills>；原著研读、资产身份与变体、跨集连续性、资产图片提示词的职责划分，以及 short-drama-storyboard 的镜头职责 / 动作落实 / 空间连续性与 short-drama-video-prompts 的起止状态 / 对白时机 / 稳定资产绑定规则参考；MIT License。storyboard.md 按本项目片段 JSON 契约独立重写。版权声明：Copyright (c) 2026 drama-skills contributors。许可证原文：<https://github.com/zenstory-ai/drama-skills/blob/main/LICENSE>。
- shuohao-skills：<https://github.com/eternityspring/shuohao-skills>；大纲、分集剧本、人物与音色字段设计参考；Apache License, Version 2.0。许可证原文：<https://github.com/eternityspring/shuohao-skills/blob/main/LICENSE>，标准许可文本：<https://www.apache.org/licenses/LICENSE-2.0>。本项目重写了相关表达，并改用本项目结构化输入输出；未复制上游文件或模板。
- short-drama：<https://github.com/0xsline/short-drama>；scripts.md 的单集文学剧本格式（场次标题、场景与出场人物行、△ 画面加景别、对白语气标注、音乐提示、本集钩子）参考；MIT License。版权声明：Copyright (c) 2025 0xsline。许可证原文：<https://github.com/0xsline/short-drama/blob/main/LICENSE>。本项目按自身 JSON 输出契约重写格式说明与示例，未复制上游模板。确认许可证日期：2026-11-01。

huobao-drama 使用 CC BY-NC-SA 4.0；本目录不使用、不改编、不复制该项目的任何文本。

## MIT 许可文本

以下许可分别适用于上述 Toonflow-app、drama-skills、short-drama 与 higgsfield-ai-prompt-skill 的版权主体，保留其版权声明与许可条件：

```text
MIT License

Copyright (c) 2026 HBAI-Ltd
Copyright (c) 2026 drama-skills contributors
Copyright (c) 2025 0xsline
Copyright (c) 2026 O-Side Media

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
