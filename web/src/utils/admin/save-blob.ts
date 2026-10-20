/**
 * 把 blob 存成文件：下载接口要带登录凭证，不能直接用链接，所以先用请求实例取到 blob 再触发浏览器保存
 * @param blob 文件内容
 * @param filename 保存的文件名
 */
export function saveBlob(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}
