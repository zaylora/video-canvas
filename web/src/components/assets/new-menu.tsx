import { FolderPlus, Plus, Upload } from "lucide-react";

import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

/**
 * 个人资产库右上角的「＋ 新建」下拉：新建文件夹、上传资产。
 * 文件夹只有一层，进入文件夹后不再出现「新建文件夹」。
 * @param canCreateFolder 当前位置能不能新建文件夹（只有根目录能）
 * @param onCreateFolder 新建文件夹
 * @param onUpload 上传资产（由调用方打开文件选择）
 */
export function NewMenu({
  canCreateFolder,
  onCreateFolder,
  onUpload,
}: {
  canCreateFolder: boolean;
  onCreateFolder: () => void;
  onUpload: () => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger render={<Button size="sm" aria-label="新建" />}>
        <Plus />
        新建
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" sideOffset={6} className="w-40">
        {canCreateFolder && (
          <DropdownMenuItem onClick={onCreateFolder}>
            <FolderPlus />
            新建文件夹
          </DropdownMenuItem>
        )}
        <DropdownMenuItem onClick={onUpload}>
          <Upload />
          上传资产
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
