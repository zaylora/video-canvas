import { useEffect, useState, type FormEvent } from "react";
import { useDropzone, type FileRejection } from "react-dropzone";
import { LoaderCircle, Lock, Trash2, Upload } from "lucide-react";
import { toast } from "sonner";

import { UserAvatar } from "@/components/user-avatar";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { cn } from "@/lib/utils";
import { useMeStore } from "@/store/me";
import {
  AVATAR_TYPES,
  displayName,
  nicknameChanged,
  nicknameState,
  validateAvatarFile,
} from "@/utils/profile/profile-rules";
import { ApiError } from "@/utils/requests/request";

import { AvatarCropDialog, type CropSource } from "./avatar-crop-dialog";

/** 业务错误码：参数错误（昵称超长或含控制字符） */
const INVALID_PARAM_CODE = 10001;

/** 卡片外框 */
const CARD = "bg-card ring-foreground/10 grid content-start gap-3 rounded-xl p-4 ring-1";

/**
 * 昵称表单：预填当前昵称，placeholder 是用户名；去掉首尾空白后 0–32 字，不能有控制字符；
 * 值有变化才能保存，保存中禁用并转圈。成功后身份卡与所有头像菜单同步刷新。
 */
function NicknameCard() {
  const me = useMeStore((state) => state.me);
  const updateNickname = useMeStore((state) => state.updateNickname);
  const [value, setValue] = useState(me?.nickname ?? "");
  const [saving, setSaving] = useState(false);
  const [serverError, setServerError] = useState("");
  const [prevNickname, setPrevNickname] = useState(me?.nickname);

  /** 资料晚到或保存成功后，输入框跟着刷新 */
  if (prevNickname !== me?.nickname) {
    setPrevNickname(me?.nickname);
    setValue(me?.nickname ?? "");
  }

  if (!me) return <Skeleton className="h-56 rounded-xl" />;

  const state = nicknameState(value, me.username);
  const message = serverError || state.message;
  const invalid = !!serverError || !state.ok;
  const canSave = state.ok && !saving && nicknameChanged(value, me.nickname);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (!canSave) return;
    setSaving(true);
    try {
      await updateNickname(value.trim());
      toast.success("昵称已更新");
    } catch (error) {
      if (error instanceof ApiError && error.code === INVALID_PARAM_CODE) {
        setServerError(error.message);
      }
    } finally {
      setSaving(false);
    }
  };

  return (
    <form className={CARD} onSubmit={submit} noValidate>
      <div className="grid gap-1.5">
        <Label htmlFor="profile-nickname">昵称</Label>
        <Input
          id="profile-nickname"
          autoComplete="nickname"
          placeholder={me.username}
          value={value}
          disabled={saving}
          aria-invalid={invalid}
          aria-describedby="profile-nickname-hint"
          onChange={(event) => {
            setValue(event.target.value);
            setServerError("");
          }}
        />
        <p
          id="profile-nickname-hint"
          aria-live="polite"
          className={cn(
            "text-xs tabular-nums",
            invalid ? "text-destructive" : "text-muted-foreground",
          )}
        >
          {message}
        </p>
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="profile-username">用户名</Label>
        <Input id="profile-username" value={me.username} disabled readOnly />
        <p className="text-muted-foreground flex items-center gap-1 text-xs">
          <Lock className="size-3" />
          用户名是登录凭据，不能修改
        </p>
      </div>
      <Button type="submit" className="justify-self-start" disabled={!canSave}>
        {saving && <LoaderCircle className="animate-spin" />}
        {saving ? "保存中" : "保存"}
      </Button>
    </form>
  );
}

/**
 * 头像：点「上传新头像」或把图片拖进来；前端先校验类型和大小（不符合时就地报错、不打开裁剪框），
 * 再弹圆形裁剪框。已有头像时可以移除（二次确认）。
 */
function AvatarCard() {
  const me = useMeStore((state) => state.me);
  const removeAvatar = useMeStore((state) => state.removeAvatar);
  const [fileError, setFileError] = useState("");
  const [source, setSource] = useState<CropSource | null>(null);
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [removing, setRemoving] = useState(false);

  /** 关掉裁剪框或换图时回收上一张的 object URL */
  useEffect(() => {
    if (!source) return;
    return () => URL.revokeObjectURL(source.url);
  }, [source]);

  const pick = (file: File | undefined) => {
    if (!file) return;
    const error = validateAvatarFile(file);
    setFileError(error ?? "");
    if (error) return;
    setSource({ url: URL.createObjectURL(file), gif: file.type === "image/gif" });
  };

  const { getRootProps, getInputProps, isDragActive, open } = useDropzone({
    noClick: true,
    noKeyboard: true,
    multiple: false,
    accept: Object.fromEntries(AVATAR_TYPES.map((type) => [type, []])),
    /** 被 accept 拒掉的文件也拿回来，用自己的文案提示格式不对 */
    onDrop: (accepted: File[], rejected: FileRejection[]) => pick(accepted[0] ?? rejected[0]?.file),
  });

  if (!me) return <Skeleton className="h-56 rounded-xl" />;

  const confirmRemove = async () => {
    setRemoving(true);
    try {
      await removeAvatar();
      toast.success("头像已移除");
      setConfirmOpen(false);
    } finally {
      setRemoving(false);
    }
  };

  return (
    <section className={CARD}>
      <div>
        <h2 className="text-sm font-semibold">头像</h2>
        <p className="text-muted-foreground text-xs">
          支持 PNG / JPEG / WebP / GIF，原图不超过 10MB。上传前先裁剪成圆形；动图会保存为静态图。
        </p>
      </div>
      <div
        {...getRootProps({
          className: cn(
            "flex items-center gap-4 rounded-xl border border-dashed p-4 transition-colors",
            isDragActive ? "border-primary bg-muted" : "border-border",
          ),
        })}
      >
        <input {...getInputProps({ "aria-label": "选择头像图片" })} />
        <UserAvatar userId={me.id} name={displayName(me)} src={me.avatarUrl} size={72} />
        <div className="grid min-w-0 gap-2">
          <Button variant="outline" size="sm" className="justify-self-start" onClick={open}>
            <Upload />
            上传新头像
          </Button>
          {me.avatarUrl ? (
            <Button
              variant="destructive"
              size="sm"
              className="justify-self-start"
              onClick={() => setConfirmOpen(true)}
            >
              <Trash2 />
              移除头像
            </Button>
          ) : (
            <span className="text-muted-foreground text-xs">也可以把图片拖到这里</span>
          )}
        </div>
      </div>
      {fileError && (
        <p role="alert" className="text-destructive text-xs">
          {fileError}
        </p>
      )}

      <AvatarCropDialog source={source} onClose={() => setSource(null)} />

      <AlertDialog open={confirmOpen} onOpenChange={(next) => !removing && setConfirmOpen(next)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>移除头像？</AlertDialogTitle>
            <AlertDialogDescription>
              移除后显示首字母头像，可以随时重新上传。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={removing}>取消</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              disabled={removing}
              onClick={() => void confirmRemove()}
            >
              {removing && <LoaderCircle className="animate-spin" />}
              移除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  );
}

/** 资料 tab：昵称 + 头像 */
export function ProfileTab() {
  return (
    <div className="grid items-start gap-3 md:grid-cols-2">
      <NicknameCard />
      <AvatarCard />
    </div>
  );
}
