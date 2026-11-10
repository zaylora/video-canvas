import type { ReactNode } from "react";

import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/admin-ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { openDialog, type DialogControl } from "@/store/dialog";

export type ChoiceOption<T extends string> = {
  value: T;
  label: string;
  /** 主操作：实心按钮，排在最右 */
  primary?: boolean;
};

export type ChoiceOptions<T extends string> = {
  title: string;
  description?: ReactNode;
  /** 除「取消」之外的选项，按显示顺序 */
  options: ChoiceOption<T>[];
};

function ChoiceDialog<T extends string>({
  title,
  description,
  options,
  resolve,
  open,
  onClose,
  onExited,
}: ChoiceOptions<T> & DialogControl & { resolve: (value: T | null) => void }) {
  const done = (value: T | null) => {
    resolve(value);
    onClose();
  };
  return (
    <AlertDialog
      open={open}
      onOpenChange={(next) => !next && done(null)}
      onOpenChangeComplete={(next) => !next && onExited?.()}
    >
      <AlertDialogContent className="data-[size=default]:sm:max-w-md">
        <AlertDialogHeader>
          <AlertDialogTitle>{title}</AlertDialogTitle>
          {description && (
            <AlertDialogDescription render={<div />}>{description}</AlertDialogDescription>
          )}
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          {options.map((option) => (
            <Button
              key={option.value}
              variant={option.primary ? "default" : "outline"}
              onClick={() => done(option.value)}
            >
              {option.label}
            </Button>
          ))}
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}

/**
 * 多选一的确认框（「取消」之外有两个以上去向时用，两个去向用 confirm）。
 * @returns 选中的 value；取消或关掉为 null
 */
export function choose<T extends string>(options: ChoiceOptions<T>): Promise<T | null> {
  return new Promise((resolve) => {
    openDialog(ChoiceDialog<T>, { ...options, resolve });
  });
}
