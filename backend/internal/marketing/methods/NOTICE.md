# 营销工作室内置方法来源说明

本目录下的 `facts.md`、`reverse.md`、`follow.md`、`create.md` 是 video-canvas 项目为营销工作室四个环节写的内置方法正文，**为改写**：用本项目自己的话重写，适配本项目的环节协议（输出结构由协议层的 Schema 给出，方法正文只讲怎么想、怎么写），没有整段复制上游文件。保留原作者署名，原许可证文本见各自仓库。

不确定版权年份和持有人的，只写仓库地址，不编造。

## 参考来源

- daihuo-fanpai（作者 wangcanyu）：https://github.com/wangcanyu/daihuo-fanpai 。MIT License。
  - 参考内容：爆款拆解的分镜字段（景别、运镜、主体 + 动作写到发力过程、商品出现方式、关键颜色、前 3 秒标记、商品角色分类）；B 模式跟拍的原则（结构、节奏、钩子不动，只换商品与卖点；动作原样带入；去第三方品牌 IP；按比例压缩或拉伸时长）。
  - 用于：`reverse.md`、`follow.md`。
- HiAPIAI/awesome-ai-video-workflows：<https://github.com/HiAPIAI/awesome-ai-video-workflows>。MIT License。
  - 参考内容：商品事实卡的字段思路（外观锚点、已证实卖点、禁用说法、目标人群、行动引导）。
  - 用于：`facts.md`。
- Ethanxwang/tvc-director：<https://github.com/Ethanxwang/tvc-director>。MIT License。
  - 参考内容：商品广告片的叙事模型与段落安排，本项目把它归并为六个创意角度的写法。
  - 用于：`create.md`。
- Creatify-AI/ai-ad-prompt-guide 等 Creatify 开源资料：<https://github.com/Creatify-AI/ai-ad-prompt-guide>。MIT License。
  - 参考内容：广告钩子的类型与广告角度的划分。
  - 用于：`create.md`。
- YouMind-OpenLab/awesome-seedance-2-prompts：<https://github.com/YouMind-OpenLab/awesome-seedance-2-prompts>。CC BY 4.0（署名 4.0 国际）。
  - 参考内容：视频提示词分区块的组织思路，以及把主体与动作放在句首的写法。本项目对其做了改写，并非原文；格式化器（`format` 子包）的分区块模板同样是在此思路上自行设计。
  - 用于：`create.md` 的画面描述写法，以及 `format` 子包的 Seedance 模板。

## 许可说明

- 上述 MIT 项目的许可证要求在副本或实质性部分中保留版权声明与许可声明。本目录文件为改写，未包含其原文；原项目的版权声明与许可证文本请见各自仓库的 LICENSE 文件。
- CC BY 4.0 项目要求署名并说明是否做了修改：本项目已在此署名，并说明为改写。许可证文本：<https://creativecommons.org/licenses/by/4.0/>。
- 许可证类型按引入时提供的信息记录，未逐一核对各仓库 LICENSE 原文，也未核实版权年份与持有人；上线前请复核，并补全 daihuo-fanpai 的仓库地址。
