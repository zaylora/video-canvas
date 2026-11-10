# 短剧提示词与风格资源来源说明

本目录提示词及 `../styles.json` 的中文描述由 video-canvas 项目重新撰写，适配本项目的 JSON 字段、校验、短剧流水线与画布节点，不包含第三方提示词原文。以下开源项目提供流程与字段设计参考；题材和画风分类名称参考 Toonflow，描述为原创改写。确认许可证日期：2026-10-28。

- Toonflow-app：<https://github.com/HBAI-Ltd/Toonflow-app>；流程、题材和画风分类参考；MIT License。版权声明：Copyright (c) 2026 HBAI-Ltd。许可证原文：<https://github.com/HBAI-Ltd/Toonflow-app/blob/master/LICENSE>。
- drama-skills：<https://github.com/zenstory-ai/drama-skills>；原著研读、资产身份与变体、跨集连续性、资产图片提示词的职责划分，以及 short-drama-storyboard 的镜头职责 / 动作落实 / 空间连续性与 short-drama-video-prompts 的起止状态 / 对白时机 / 稳定资产绑定规则参考；MIT License。storyboard.md 按本项目片段 JSON 契约独立重写。版权声明：Copyright (c) 2026 drama-skills contributors。许可证原文：<https://github.com/zenstory-ai/drama-skills/blob/main/LICENSE>。
- shuohao-skills：<https://github.com/eternityspring/shuohao-skills>；大纲、分集剧本、人物与音色字段设计参考；Apache License, Version 2.0。许可证原文：<https://github.com/eternityspring/shuohao-skills/blob/main/LICENSE>，标准许可文本：<https://www.apache.org/licenses/LICENSE-2.0>。本项目重写了相关表达，并改用本项目结构化输入输出；未复制上游文件或模板。

- manju-laoli-skill：<https://github.com/lixiaoxiao9888-create/manju-laoli-skill>；短剧剧本格式与对白口语化、角色 / 场景 / 道具三表提取与复用判定、资产图构图，以及 Seedance 站位声明、光影单一基准、禁比喻修辞、对白语气位和 MiniMax H3 字段与硬约束的思路参考（`storyboard.md`、`adapt_h3.md`、`adapt_generic.md`、`scripts.md`、`breakdown.md`、`asset_prompts.md`）；MIT License。版权声明：Copyright (c) 2026 Short-Drama Director Suite contributors。许可证原文：<https://github.com/lixiaoxiao9888-create/manju-laoli-skill/blob/main/LICENSE>。本项目用自己的话重写，改用本项目 JSON 契约，未复制上游文件。确认许可证日期：2026-11-04。
- 另有本机 h3-prompt-writing 技能与 dexhunter/seedance2-skill 仅用于对照字段结构和写法思路，许可证未确认，未复制其文本。

huobao-drama 使用 CC BY-NC-SA 4.0；本目录不使用、不改编、不复制该项目的任何文本。

## MIT 许可文本

以下许可分别适用于上述 Toonflow-app 与 drama-skills 的版权主体，保留其版权声明与许可条件：

```text
MIT License

Copyright (c) 2026 HBAI-Ltd
Copyright (c) 2026 drama-skills contributors

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
