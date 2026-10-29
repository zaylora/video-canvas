import { useRef, type ChangeEvent } from "react";
import { FileUp } from "lucide-react";

import { Button } from "@/components/ui/button";

/**
 * 「导入画布」按钮：点击打开系统文件选择，选中后把文件交给 onPick。
 * 选完立刻清空 input，再次选同一个文件也能触发。
 * @param importing 导入中，按钮禁用（转圈反馈由进度弹窗承担）
 * @param onPick 选中文件
 */
export function ImportCanvasButton({
  importing,
  onPick,
}: {
  importing: boolean;
  onPick: (file: File) => void;
}) {
  const inputRef = useRef<HTMLInputElement>(null);

  const handleChange = (event: ChangeEvent<HTMLInputElement>) => {
    const input = event.currentTarget;
    const file = input.files?.[0];
    input.value = "";
    if (file) onPick(file);
  };

  return (
    <>
      <input
        ref={inputRef}
        type="file"
        accept=".json,application/json"
        className="hidden"
        aria-label="选择画布文件"
        onChange={handleChange}
      />
      <Button
        variant="ghost"
        className="hover:bg-chrome-hover h-9 rounded-full px-3.5"
        disabled={importing}
        onClick={() => inputRef.current?.click()}
      >
        <FileUp />
        导入画布
      </Button>
    </>
  );
}
