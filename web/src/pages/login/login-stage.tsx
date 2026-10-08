import type { ReactNode } from "react";

/**
 * 登录页的全屏背景舞台：深色底 + 两团柔光 + 四周压暗的遮罩，字永远压得住。
 * 作品轮播（后台「登录页展示」配置的视频）作为 children 放在柔光和遮罩之间；
 * 没有作品、后台关了播放或加载失败时 children 为空，就是纯渐变背景。
 */
export function LoginStage({ children }: { children?: ReactNode }) {
  return (
    <div
      data-slot="login-stage"
      aria-hidden
      className="bg-stage absolute inset-0 isolate overflow-hidden"
    >
      <div className="absolute inset-0 bg-[radial-gradient(ellipse_70%_60%_at_70%_30%,var(--stage-glow-a),transparent_70%),radial-gradient(ellipse_60%_60%_at_20%_85%,var(--stage-glow-b),transparent_70%)]" />
      {children}
      <div className="pointer-events-none absolute inset-0 z-10 bg-[linear-gradient(to_bottom,var(--stage-scrim-edge)_0%,transparent_24%,transparent_58%,var(--stage-scrim-edge)_100%),radial-gradient(ellipse_58%_46%_at_50%_47%,var(--stage-scrim-center),transparent_72%)]" />
    </div>
  );
}
