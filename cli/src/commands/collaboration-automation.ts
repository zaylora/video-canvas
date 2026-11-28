import fs from "node:fs";
import path from "node:path";

import { isInsecureRemoteBase } from "../base.ts";
import { APIError, normalizeBase } from "../client.ts";
import type { Args, Ctx, Flags, WireVersion } from "../types.ts";
import { intFlag, stringFlag } from "./args.ts";
import { canvasPath, collaborationRequest, getCollaboration } from "./collaboration.ts";
import { askYes, baseOf, emit, requireInteractive } from "./shared.ts";

interface AutomationKey {
  id: string;
  canvasId: string;
  scope: string;
  budgetCredits?: number;
  expiresAt: string;
  revokedAt?: string;
  token?: string;
}

export function configureAutomation(ctx: Ctx, flags: Flags): void {
  const file = ctx.env.VC_AUTOMATION_CREDENTIALS;
  if (!file) return;
  let descriptor: number | undefined;
  try {
    descriptor = fs.openSync(file, fs.constants.O_RDONLY | (fs.constants.O_NOFOLLOW ?? 0));
    const stat = fs.fstatSync(descriptor);
    if (!stat.isFile() || stat.size > 4096 || (process.platform !== "win32" && stat.mode & 0o077))
      throw new Error();
    const { base, canvasId, token } = JSON.parse(fs.readFileSync(descriptor, "utf8"));
    const canonical = automationBase(base);
    const requested = flags.base || ctx.env.VC_API_BASE;
    if (requested && automationBase(requested) !== canonical) throw new Error();
    if (
      typeof canvasId !== "string" ||
      !/^[a-zA-Z0-9_-]{1,128}$/.test(canvasId) ||
      !/^vca_[a-f0-9]{64}$/.test(token)
    )
      throw new Error();
    ctx.automation = { base: canonical, canvasId, token };
    ctx.base = canonical;
    ctx.creds = null;
    ctx.token = undefined;
    ctx.baseMismatch = null;
  } catch {
    throw new APIError(-1, "自动化凭据文件、文件权限或站点绑定无效", 2);
  } finally {
    if (descriptor !== undefined) fs.closeSync(descriptor);
  }
}

function automationBase(value: unknown): string {
  const url = new URL(normalizeBase(value));
  if (url.username || url.password || url.search || url.hash || isInsecureRemoteBase(url.href))
    throw new APIError(-1, "自动化凭据需要 HTTPS 站点或本机地址", 2);
  return url.href;
}

export async function canvasAutomation(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, id] = args.positional;
  const usage = "canvas automation issue|list|revoke <画布>";
  if (!code) throw new APIError(-1, `用法：${usage}`, 2);
  const route = `${canvasPath(code)}/collaboration/automation-keys`;
  if (action === "list") {
    const keys = await collaborationRequest<AutomationKey[]>(ctx, route);
    emit(
      ctx,
      keys,
      keys.map((key) => `${key.id}\t${key.scope}\t${key.revokedAt ?? key.expiresAt}`).join("\n"),
    );
    return;
  }
  if (action === "revoke" && id) {
    await collaborationRequest(ctx, `${route}/${encodeURIComponent(id)}`, { method: "DELETE" });
    emit(ctx, { id, revoked: true }, `已撤销凭据 ${id}`);
    return;
  }
  if (action !== "issue") throw new APIError(-1, `用法：${usage}`, 2);
  const scope = stringFlag(args.flags, "scope", usage);
  if (scope !== "read" && scope !== "edit" && scope !== "generate") {
    throw new APIError(-1, "--scope 需要 read、edit 或 generate", 2);
  }
  const budgetCredits = intFlag(args.flags, "budget-credits", undefined, { min: 1, max: 100_000 });
  if ((scope === "generate") !== (budgetCredits !== undefined)) {
    throw new APIError(-1, "--scope generate 必须指定 --budget-credits，其他权限不接受付费预算", 2);
  }
  const hours = intFlag(args.flags, "hours", 24, { min: 1, max: 168 });
  const file = path.resolve(stringFlag(args.flags, "output", usage));
  const base = automationBase(baseOf(ctx));
  requireInteractive(ctx, "签发自动化凭据", "脚本通过私有凭据文件使用现有授权");
  const snapshot = await getCollaboration(ctx, code);
  if (snapshot.role === "viewer") throw new APIError(-1, "当前账号没有签发权限", 403);
  if (scope === "generate" && !snapshot.canGenerate)
    throw new APIError(-1, "当前账号没有此画布的生成权限", 403);
  const budget = budgetCredits === undefined ? "" : `，累计最多花费 ${budgetCredits} 积分`;
  await askYes(
    ctx,
    `为 ${base} 的 ${snapshot.canvas.title}（${code}）签发 ${scope} 凭据，有效期 ${hours} 小时${budget}，保存到 ${file}。输入 yes 确认：`,
    "已取消签发",
  );
  await writeAutomationKey(ctx, file, route, {
    base,
    hours,
    scope,
    epoch: snapshot.epoch,
    budgetCredits,
  });
}

async function writeAutomationKey(
  ctx: Ctx,
  file: string,
  route: string,
  request: {
    base: string;
    hours: number;
    scope: string;
    epoch: WireVersion;
    budgetCredits?: number;
  },
): Promise<void> {
  const { base, ...body } = request;
  const fd = fs.openSync(file, "wx", 0o600);
  let key: AutomationKey | undefined;
  try {
    key = await collaborationRequest<AutomationKey>(ctx, route, { method: "POST", body });
    if (!key.token || !key.canvasId) throw new APIError(-1, "服务端未返回有效凭据", 502);
    fs.writeFileSync(fd, `${JSON.stringify({ base, canvasId: key.canvasId, token: key.token })}\n`);
  } catch (err) {
    if (key?.id)
      await collaborationRequest(ctx, `${route}/${encodeURIComponent(key.id)}`, {
        method: "DELETE",
      }).catch(() => undefined);
    fs.unlinkSync(file);
    throw err;
  } finally {
    fs.closeSync(fd);
  }
  const { id, scope, expiresAt, budgetCredits } = key;
  emit(
    ctx,
    { id, scope, expiresAt, budgetCredits, file },
    `凭据 ${id} 已保存到 ${file}，到期时间 ${expiresAt}`,
  );
}
