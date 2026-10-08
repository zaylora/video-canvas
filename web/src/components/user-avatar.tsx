import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { cn } from "@/lib/utils";
import { avatarColor, avatarInitial } from "@/utils/profile/profile-rules";

/**
 * 全站统一的用户头像：有头像显示图片，没有或加载失败时显示首字母，
 * 底色按 user_id 哈希成固定色（同一个人在侧栏、画布右上角、身份卡上颜色一致）。
 * @param userId 用户 ID；未知时用中性底色
 * @param name 显示名（昵称或用户名），取第一个字做兜底
 * @param src 头像地址；null 表示没有头像
 * @param size 边长（像素）
 */
export function UserAvatar({
  userId,
  name,
  src,
  size = 32,
  className,
}: {
  userId?: string | null;
  name: string;
  src?: string | null;
  size?: number;
  className?: string;
}) {
  return (
    <Avatar
      className={cn("shrink-0 after:mix-blend-normal", className)}
      style={{ width: size, height: size }}
    >
      {src && <AvatarImage src={src} alt={`${name}的头像`} />}
      <AvatarFallback
        className={cn("font-semibold text-white", !userId && "bg-muted-foreground")}
        style={{
          ...(userId ? { background: avatarColor(userId) } : {}),
          fontSize: Math.round(size * 0.42),
        }}
      >
        {avatarInitial(name)}
      </AvatarFallback>
    </Avatar>
  );
}
