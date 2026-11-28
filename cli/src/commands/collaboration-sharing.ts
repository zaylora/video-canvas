import { randomUUID } from "node:crypto";

import { APIError } from "../client.ts";
import type { Args, Ctx, WireVersion } from "../types.ts";
import { boolFlag, stringFlag } from "./args.ts";
import { compareVersions, isWireVersion, versionFlag } from "./collaboration-version.ts";
import { fetchCanvas } from "./canvas.ts";
import {
  canvasPath,
  CollaborationError,
  collaborationRequest,
  readOwnedCanvas,
} from "./collaboration.ts";
import { askYes, emit, requireInteractive } from "./shared.ts";

function receiptKey(key: string): string {
  if (!/^[a-zA-Z0-9_-]{1,64}$/.test(key)) throw new APIError(-1, "无效的交付编号", 2);
  return key;
}

async function ownedCanvas(ctx: Ctx, code: string) {
  const canvas = await readOwnedCanvas(ctx, code);
  if (!canvas) throw new CollaborationError(-1, "该操作需要画布所有权", 403, "permission_denied");
  return canvas;
}

async function confirm(ctx: Ctx, label: string, impact: string): Promise<void> {
  ctx.stderr(impact);
  await askYes(ctx, `输入 yes 确认${label}：`, `已取消${label}`);
}

export async function canvasManage(ctx: Ctx, args: Args, action: "rename" | "archive") {
  const [code] = args.positional;
  if (!code)
    throw new APIError(-1, "用法：canvas rename <画布> --title <标题> / canvas archive <画布>", 2);
  const archived = action === "archive";
  const title = archived
    ? undefined
    : stringFlag(args.flags, "title", "请指定 --title <标题>").trim();
  if (title !== undefined && (!title || title.includes("\0") || [...title].length > 255))
    throw new APIError(-1, "画布标题需要 1–255 个字符", 2);
  if (archived) requireInteractive(ctx, "归档画布", "请确认失去访问权限的画布。");
  const canvas = await ownedCanvas(ctx, code);
  const revision = versionFlag(args.flags, "revision") ?? canvas.revision;
  if (compareVersions(revision, canvas.revision) !== 0)
    throw new CollaborationError(30002, "画布版本已变化，请重新读取", 409, "conflict");
  const path = `${canvasPath(canvas.id)}/management`;
  const preview = archived ? await confirmArchive(ctx, path, canvas.id, revision) : undefined;
  const updated = await collaborationRequest(ctx, path, {
    method: "POST",
    body: { revision, archive: archived || undefined, title, preview },
  });
  emit(ctx, updated, archived ? `已归档「${canvas.title}」` : `已重命名为「${title}」`);
}

async function confirmArchive(ctx: Ctx, path: string, id: string, revision: WireVersion) {
  const impact = await collaborationRequest<{
    title: string;
    revision: WireVersion;
    preview: string;
    editors: number;
    viewers: number;
  }>(ctx, path);
  if (!isWireVersion(impact.revision) || compareVersions(revision, impact.revision) !== 0)
    throw new CollaborationError(30002, "画布版本已变化，请重新读取", 409, "conflict");
  if (!/^[0-9a-f]{64}$/.test(impact.preview)) throw new APIError(-1, "站点未返回有效归档预览", 2);
  await confirm(
    ctx,
    "归档",
    `归档「${impact.title}」（${id}，版本 ${revision}）会从作品列表移除画布，并撤销 ${impact.editors} 位编辑者、${impact.viewers} 位查看者的访问。独立副本不受影响。`,
  );
  return impact.preview;
}

export async function canvasShare(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, target] = args.positional;
  const usage = "用法：canvas share invite|revoke|leave|copy|transfer|receipt <画布>";
  if (
    !code ||
    !action ||
    !["invite", "revoke", "leave", "copy", "transfer", "receipt"].includes(action)
  ) {
    throw new APIError(-1, usage, 2);
  }
  if (action === "receipt") {
    if (!target) throw new APIError(-1, "用法：canvas share receipt <画布> <交付编号>", 2);
    const result = await collaborationRequest<{ status: string }>(
      ctx,
      `${canvasPath(code)}/deliveries/${receiptKey(target)}`,
    );
    return emit(ctx, result, `交付 ${target} ${result.status}`);
  }
  requireInteractive(ctx, "共享变更", "请在终端确认目标和影响。");
  if (action === "leave") {
    const shared = await fetchCanvas(ctx, code);
    if (shared.collaboration_role !== "viewer" && shared.collaboration_role !== "editor") {
      throw new APIError(-1, "只有受邀成员可以退出共享", 2);
    }
    await confirm(
      ctx,
      "退出共享",
      `退出「${shared.title}」后，你将失去查看和编辑权限。画布及其他成员的权限保留。再次访问需要所有者重新邀请。`,
    );
    await collaborationRequest(ctx, `${canvasPath(shared.id)}/shares/me`, { method: "DELETE" });
    return emit(ctx, { canvasId: shared.id }, "已退出共享");
  }
  const canvas = await ownedCanvas(ctx, code);
  const base = canvasPath(canvas.id);
  if (action === "revoke") {
    if (!target || target === "me")
      throw new APIError(-1, "用法：canvas share revoke <画布> <成员ID>", 2);
    const member = await findMember(ctx, base, target);
    await confirm(ctx, "撤销访问", `将撤销 ${member.username} 对「${canvas.title}」的访问。`);
    await collaborationRequest(ctx, `${base}/shares/${encodeURIComponent(member.id)}`, {
      method: "DELETE",
    });
    return emit(ctx, { canvasId: canvas.id, member: member.username }, "已撤销访问");
  }
  const email = stringFlag(args.flags, "email", usage).trim();
  if (!email || !email.includes("@")) throw new APIError(-1, "请输入接收者邮箱", 2);
  if (action === "invite") {
    const role = args.flags.role === undefined ? "viewer" : stringFlag(args.flags, "role", usage);
    return setMemberRole(
      ctx,
      base,
      canvas.title,
      email,
      role,
      Boolean(canvas.collaboration_active),
    );
  }
  const revision = versionFlag(args.flags, "revision") ?? canvas.revision;
  if (revision !== canvas.revision)
    throw new CollaborationError(30002, "确认的交付版本已变化，请重新查看画布", 409, "conflict");
  const key = receiptKey(
    args.flags["op-id"] === undefined ? randomUUID() : stringFlag(args.flags, "op-id", usage),
  );
  const transfer = action === "transfer";
  const label = transfer ? "转移所有权" : "发送副本";
  await confirm(
    ctx,
    label,
    transfer
      ? `将把「${canvas.title}」版本 ${revision} 转移给 ${email}，原共享访问将失效，你将失去对画布的访问。`
      : `将把「${canvas.title}」版本 ${revision} 的独立副本发到 ${email} 的个人画布。收件人可继续编辑。`,
  );
  ctx.stderr(`交付编号 ${key}。可用 canvas share receipt ${canvas.id} ${key} 查询结果。`);
  await collaborationRequest(ctx, `${base}/shares`, {
    method: "POST",
    body: { email, mode: action, request_key: key, revision },
  });
  emit(ctx, { requestKey: key, canvasId: canvas.id, revision: String(revision) }, `已${label}`);
}

interface Member {
  id: string;
  username: string;
  role: "viewer" | "editor";
  email: string;
}

async function findMember(ctx: Ctx, base: string, target: string): Promise<Member> {
  const rows = await collaborationRequest<Member[]>(ctx, `${base}/shares`);
  const found = rows.find((member) => member.id === target);
  if (!found) throw new APIError(-1, "该画布没有这个成员", 404);
  return found;
}

async function setMemberRole(
  ctx: Ctx,
  base: string,
  title: string,
  email: string,
  role: string,
  active: boolean,
): Promise<void> {
  if (!["viewer", "editor"].includes(role) || (role === "editor" && !active)) {
    throw new APIError(-1, "角色须为 viewer 或 editor。设置编辑者需要先启用实时协作。", 2);
  }
  await confirm(
    ctx,
    "设置共享权限",
    `将设置 ${email} 在「${title}」的权限为${role === "editor" ? "可编辑" : "仅查看"}。`,
  );
  if (active) {
    await collaborationRequest(ctx, `${base}/collaboration/member`, {
      method: "PUT",
      body: { email, role },
    });
  } else {
    await collaborationRequest(ctx, `${base}/shares`, {
      method: "POST",
      body: { email, mode: "view" },
    });
  }
  emit(ctx, { role }, "已设置共享权限");
}

export async function canvasMember(ctx: Ctx, args: Args): Promise<void> {
  const [action, code, target] = args.positional;
  const usage = "用法：canvas member list <画布>，或 canvas member role|grant <画布> <成员ID>";
  if (!code || !["list", "role", "grant"].includes(action ?? "")) throw new APIError(-1, usage, 2);
  const canvas = await ownedCanvas(ctx, code);
  const base = canvasPath(canvas.id);
  if (action === "list") {
    const rows = await collaborationRequest<Member[]>(ctx, `${base}/shares`);
    return emit(
      ctx,
      rows,
      rows.map((row) => `${row.username}\t${row.role}\t${row.id}`).join("\n") || "当前没有共享成员",
    );
  }
  if (!target) throw new APIError(-1, usage, 2);
  requireInteractive(ctx, "修改成员授权", "请在终端确认目标成员。");
  const member = await findMember(ctx, base, target);
  if (action === "role")
    return setMemberRole(
      ctx,
      base,
      canvas.title,
      member.email,
      stringFlag(args.flags, "role", usage),
      Boolean(canvas.collaboration_active),
    );
  const permission = stringFlag(args.flags, "permission", usage);
  const state = stringFlag(args.flags, "enabled", usage);
  if (
    !canvas.collaboration_active ||
    member.role !== "editor" ||
    !["generation", "library"].includes(permission) ||
    !["true", "false"].includes(state)
  ) {
    throw new APIError(
      -1,
      "请为实时协作编辑者指定 --permission generation|library 和 --enabled true|false",
      2,
    );
  }
  const enabled = boolFlag(args.flags, "enabled");
  const label = permission === "generation" ? "生成" : "收藏到个人资产库";
  await confirm(
    ctx,
    "修改授权",
    `将${enabled ? "授予" : "收回"} ${member.username} 在「${canvas.title}」的${label}权限。`,
  );
  await collaborationRequest(
    ctx,
    `${base}/collaboration/member/${encodeURIComponent(member.id)}/${permission}`,
    { method: "PUT", body: { enabled } },
  );
  emit(ctx, { member: member.username, permission, enabled }, "已修改授权");
}

export async function canvasCollaborationMode(ctx: Ctx, args: Args): Promise<void> {
  const [action, code] = args.positional;
  if (!code || !["enable", "disable"].includes(action ?? ""))
    throw new APIError(-1, "用法：canvas collaboration enable|disable <画布>", 2);
  requireInteractive(ctx, "切换协作模式", "请在终端确认画布版本。");
  const canvas = await ownedCanvas(ctx, code);
  const active = action === "enable";
  const label = active ? "开启实时协作" : "关闭实时协作";
  const endpoint = `${canvasPath(canvas.id)}/collaboration/mode`;
  const impact = await collaborationRequest<{
    title: string;
    revision: WireVersion;
    preview: string;
    editors: number;
    viewers: number;
  }>(ctx, endpoint, { query: { active } });
  const members = `${impact.editors} 位编辑者、${impact.viewers} 位查看者`;
  const consequences = active
    ? `${members}将按原授权访问，原有编辑者可重新加入。`
    : `${members}仍可查看。当前编辑会话和未提交草稿将失效。`;
  await confirm(
    ctx,
    label,
    `将为「${impact.title}」版本 ${impact.revision} ${label}。${consequences}`,
  );
  const result = await collaborationRequest(ctx, endpoint, {
    method: "PUT",
    body: { active, revision: impact.revision, preview: impact.preview },
  });
  emit(ctx, result, `已${label}`);
}
