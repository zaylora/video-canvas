/** 像场次编号的开头：1-1 / 3.2 / 第二场 / 第 3 幕 / 场景 / 场次 / INT. / EXT. */
const SCENE_START =
  /^(?:\d+\s*[-–.．]\s*\d+|第\s*[\d一二三四五六七八九十百]+\s*[场幕]|场景|场次|INT\.|EXT\.)/i;

/** 去掉行内标记（加粗、斜体、行内代码）和标题尾部的井号，空白压成一个；页面里按标题找 DOM 节点时用同一个函数比对 */
export function normalizeSceneTitle(text: string): string {
  return text
    .replace(/[*_`]/g, "")
    .replace(/\s+#+\s*$/, "")
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * 从剧本 Markdown 里找出场次标题，按出现顺序返回（剧本页右侧的场次目录用）。
 * 剧本格式由模型自由发挥，没有固定约定，所以只认两种写法：
 * 二到四级标题（一级是集名，不算）；整行加粗且以场次编号开头的行。代码围栏里的内容忽略。
 */
export function extractScenes(markdown: string): string[] {
  const scenes: string[] = [];
  let fenced = false;
  for (const raw of markdown.split("\n")) {
    const line = raw.trim();
    if (line.startsWith("```")) {
      fenced = !fenced;
      continue;
    }
    if (fenced || !line) continue;
    const heading = /^#{2,4}\s+(.+)$/.exec(line);
    if (heading) {
      const title = normalizeSceneTitle(heading[1]);
      if (title) scenes.push(title);
      continue;
    }
    const bold = /^\*\*(.+)\*\*$/.exec(line);
    if (bold) {
      const title = normalizeSceneTitle(bold[1]);
      if (SCENE_START.test(title)) scenes.push(title);
    }
  }
  return scenes;
}
