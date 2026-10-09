import type { RecordDto } from "@/api/conversation/type";

/**
 * 把更早的一页记录并到已有列表前面。列表按时间正序保存，接口每页是新到旧，所以要翻转；
 * 重复的记录（例如翻页期间刚发了一条新记录，页边界错位）只留已有的那一份。
 * @param existing 已有记录，时间正序
 * @param olderPage 更早的一页，新到旧
 * @returns 合并后的记录，时间正序
 */
export function mergeOlderPage(existing: RecordDto[], olderPage: RecordDto[]): RecordDto[] {
  const known = new Set(existing.map((record) => record.id));
  const older = olderPage.filter((record) => !known.has(record.id)).reverse();
  return [...older, ...existing];
}

/**
 * 把一条记录追加到列表末尾；同 id 的记录已存在时原地替换（幂等重试会返回同一条记录）。
 * @param list 已有记录，时间正序
 * @param record 新记录
 * @returns 新列表
 */
export function appendRecord(list: RecordDto[], record: RecordDto): RecordDto[] {
  const index = list.findIndex((item) => item.id === record.id);
  if (index < 0) return [...list, record];
  return list.map((item, i) => (i === index ? record : item));
}

/**
 * 从列表里删掉一条记录。
 * @param list 已有记录
 * @param id 要删的记录 ID
 * @returns 新列表
 */
export const removeRecord = (list: RecordDto[], id: string): RecordDto[] =>
  list.filter((record) => record.id !== id);
