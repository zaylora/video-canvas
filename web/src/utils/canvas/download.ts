/**
 * 把素材原文件存到本地。
 * 为什么先 fetch 成 blob：跨域地址（S3 兼容存储）会让 `<a download>` 退化成直接打开，
 * 拿到 blob 再下载才能保证存盘；跨域拿不到时退回新标签页打开，至少用户还能另存。
 * @param src 素材地址
 * @param filename 保存的文件名
 */
export async function downloadMedia(src: string, filename: string): Promise<void> {
  const save = (href: string) => {
    const link = document.createElement("a");
    link.href = href;
    link.download = filename;
    link.rel = "noopener";
    document.body.append(link);
    link.click();
    link.remove();
  };
  try {
    const response = await fetch(src);
    if (!response.ok) throw new Error(`HTTP ${response.status}`);
    const url = URL.createObjectURL(await response.blob());
    save(url);
    setTimeout(() => URL.revokeObjectURL(url), 10_000);
  } catch {
    window.open(src, "_blank", "noopener");
  }
}

/**
 * 下载文件名：优先用上传时的原文件名，否则用节点名加地址里的扩展名。
 * @param label 节点名
 * @param src 素材地址
 * @param fileName 上传素材的原文件名
 */
export function downloadName(label: string, src: string, fileName?: string): string {
  if (fileName) return fileName;
  const path = src.split(/[?#]/)[0] ?? "";
  const ext = /\.([a-z0-9]{2,5})$/i.exec(path)?.[1];
  return ext ? `${label}.${ext}` : label;
}
