## 输入变量

source 是本批带单元号的完整原文。summary、characters_md、outline_md 提供全书背景；known_cast 是实时登记名单，冲突时以它为准。next_episode_no 是下一集编号，previous_cliffhanger 是上一集结尾悬念，episode_seconds 为单集时长。不设全剧目标集数。

## 本步职责

只为本批完整原文生成分集目录，禁止写剧本或资产清单。

## 提示词正文

按原文事件顺序规划可拍摄的短剧集。保留人物动机、事件因果和关键对白依据；每集写标题、梗概、开场钩子、结尾悬念及对应原文单元区间。与 previous_cliffhanger 衔接，使用 known_cast 正式名字。每批不超过六十集，必须连续覆盖本批所有单元，允许一个单元拆成多集；后集 unit_from 只能等于前集 unit_to 或其加一，不得倒退、跳过或越界。不提前消耗下一批原文。

## 输出 JSON Schema

```json
{"type":"object","additionalProperties":false,"required":["episodes"],"properties":{"episodes":{"type":"array","minItems":1,"maxItems":60,"items":{"type":"object","additionalProperties":false,"required":["title","synopsis","hook","cliffhanger","unit_from","unit_to"],"properties":{"title":{"type":"string","minLength":1},"synopsis":{"type":"string","minLength":1},"hook":{"type":"string","minLength":1},"cliffhanger":{"type":"string","minLength":1},"unit_from":{"type":"integer","minimum":1},"unit_to":{"type":"integer","minimum":1}}}}}}
```

## 校验规则

只返回单个 JSON 对象，不得有围栏、null、未知字段。必须按序连续覆盖输入单元范围，校验反馈只能用于修正完整结果。
