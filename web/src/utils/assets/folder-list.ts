import type { FolderDto } from "@/api/asset-library/type";

/**
 * 新建文件夹后追加到列表末尾（后端按创建时间排，新的在最后）
 * @param folders 当前列表
 * @param folder 新文件夹
 */
export const addFolder = (folders: FolderDto[], folder: FolderDto): FolderDto[] => [
  ...folders,
  folder,
];

/**
 * 重命名后就地替换名称
 * @param folders 当前列表
 * @param id 文件夹 ID
 * @param name 新名称
 */
export const renameFolder = (folders: FolderDto[], id: string, name: string): FolderDto[] =>
  folders.map((folder) => (folder.id === id ? { ...folder, name } : folder));

/**
 * 删除文件夹
 * @param folders 当前列表
 * @param id 文件夹 ID
 */
export const removeFolder = (folders: FolderDto[], id: string): FolderDto[] =>
  folders.filter((folder) => folder.id !== id);

/**
 * 调整某个文件夹的素材数，最低到 0；根目录（null）没有计数，原样返回
 * @param folders 当前列表
 * @param id 文件夹 ID，null 是根目录
 * @param delta 增减量
 */
export const adjustFolderCount = (
  folders: FolderDto[],
  id: string | null,
  delta: number,
): FolderDto[] =>
  id === null
    ? folders
    : folders.map((folder) =>
        folder.id === id ? { ...folder, itemCount: Math.max(0, folder.itemCount + delta) } : folder,
      );
