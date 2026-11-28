#!/usr/bin/env node
// video-canvas-cli：连镜的命令行入口。
// 运行时只依赖 Node 22.18+ 内置能力（技能脚本是 .ts，需要默认的类型擦除）；源码是 TypeScript，构建产物是 dist/cli.js，
// 经 GitHub Release 分发：npm i -g <Release 上的 video-canvas-cli.tgz>，或站点的一键安装脚本。

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import { insecureBaseWarning, isInsecureRemoteBase, sameBase } from "./base.ts";
import { APIError, normalizeBase } from "./client.ts";
import { assetDownload } from "./commands/asset-download.ts";
import { canvasStatus, CollaborationError } from "./commands/collaboration.ts";
import { canvasAutomation, configureAutomation } from "./commands/collaboration-automation.ts";
import {
  assetLibraryList,
  canvasAsset,
  canvasCharacterGenerate,
  canvasDirectorScene,
  canvasOutput,
  canvasStructuredNode,
} from "./commands/collaboration-content.ts";
import { canvasMedia } from "./commands/collaboration-media.ts";
import { canvasTaskList } from "./commands/collaboration-tasks.ts";
import { canvasApply, canvasOpStatus, canvasPlan } from "./commands/collaboration-plan.ts";
import {
  canvasComment,
  canvasHistory,
  canvasMention,
  canvasUndo,
} from "./commands/collaboration-review.ts";
import {
  canvasCollaborationMode,
  canvasManage,
  canvasMember,
  canvasShare,
} from "./commands/collaboration-sharing.ts";
import { canvasPresence, canvasWatch } from "./commands/collaboration-watch.ts";
import { storyboardJob, storyboardSplit } from "./commands/collaboration-storyboard.ts";
import {
  canvasEdge,
  canvasGroup,
  canvasNodeArrange,
  canvasNodeDuplicate,
  canvasNodeMove,
  canvasNodeRead,
  canvasNodeRefs,
} from "./commands/collaboration-structure.ts";
import { storyboardShot } from "./commands/collaboration-shots.ts";
import { taskCancel } from "./commands/task.ts";
import { agentControl, agentHistory, agentSessions } from "./commands/agent.ts";
import {
  agentApprove,
  agentCancel,
  agentEvents,
  agentRun,
  assetUpload,
  canvasCompose,
  canvasComposeCancel,
  canvasComposeStatus,
  canvasCreate,
  canvasExport,
  canvasGet,
  canvasGraph,
  canvasImport,
  canvasList,
  canvasNodeAdd,
  canvasNodeRm,
  canvasNodeSet,
  canvasSave,
  credits,
  dramaGet,
  dramaJobCancel,
  dramaJobs,
  dramaList,
  dramaUnit,
  dramaUnits,
  generate,
  login,
  logout,
  modelList,
  modelShow,
  parseArgs,
  skill,
  taskGet,
  taskWait,
  trace,
  whoami,
} from "./commands/index.ts";
import { credentialsPath, loadCredentials } from "./config.ts";
import type { Args, Ctx, Flags } from "./types.ts";
import { fetchLatestVersion, isNewer, upgradeHint } from "./update.ts";

const USAGE = `video-canvas-cli（beta）— 连镜命令行

用法：
  video-canvas-cli <命令> [参数]

身份与会话
  login [--base URL] [--web URL] [--name 名称] [--no-open]
                                                  设备授权登录（在浏览器里确认一次，默认 30 天免登录）
                                                  授权页与 API 不在同一个域名时，用 --web（或 VC_WEB_URL）指定网页地址
  logout                                          只清本机凭据；服务端登录态到期或改密码后才失效
  whoami                                          查看当前身份
  version                                         打印 CLI 版本（与站点版本独立）
  skill path                                      打印随包技能目录
  skill install [--to <技能目录>]                 把技能装进终端助手（默认 Claude Code，已装 Codex 的也一并装）

画布（读写）
  canvas list [--scope personal|shared|all] [--page N] [--page-size N]
  canvas get <code>
  canvas status <code>                     查看权限、协作代数和确认版本
  canvas rename <code> --title <标题> [--revision N]
  canvas archive <code> [--revision N]      确认后归档并撤销成员访问
  canvas watch <code> [--since N --epoch E] [--once | --follow 成员] [--jsonl]  监听、跟随或补取确认增量
    --comments-since <游标> --epoch E                              恢复授权评论流
    --tasks                                                     监听拆分镜和角色音色的协作摘要
  canvas task list <code> [--limit N]                             读取协作任务摘要
  canvas presence <code> [--json]                              查看在线协作者
  canvas plan <code> --input <文件> [-o <文件>]       生成协作变更计划
  canvas apply <code> --plan <文件>                    提交计划并保留可恢复的操作记录
  canvas resume <code> --run <操作编号>                对账先前提交的协作操作
  canvas op status <code> <操作编号> [--epoch N]        查询本人操作收据
  canvas node list|get <code> [节点]                  查找节点与观察版本
  canvas node move <code> <节点> --at x,y --epoch N --revision N
  canvas node arrange <code> <节点...> --mode row|column|grid --epoch N --revision N
  canvas node duplicate <code> <节点> --epoch N --revision N [--at x,y]
  canvas node refs <code> <节点> [--input 文件 --epoch N --revision N]
  canvas edge add|rm <code> <连线> [来源节点 目标节点] --epoch N --revision N
  canvas group add|set|rm <code> <分组> --epoch N --revision N
  canvas comment list|add <code> --node <节点>          查看或发表节点评论
  canvas mention list <code>                          查看提及提醒
  canvas history list|show|copy <code> [版本]           查看活动、预览或另存版本
  canvas undo|redo <code> --target <版本>               撤销或重做指定的当前个人操作
  canvas collaboration enable|disable <code>          切换协作模式
  canvas member list|role|grant <code> [成员ID]         查看成员或调整权限
  canvas share invite|revoke|leave|copy|transfer|receipt <code>
  canvas asset attach|replace|collect <code> <节点> --asset-id <素材>
  canvas output list|select <code> <节点> [--output-id <结果>]
  canvas media frame|trim <code> <节点> --epoch N --revision N -o <文件>
  canvas media grid <code> <图片节点> --epoch N --revision N -o <目录> [--rows N --columns N | --input 文件]
  canvas media annotate <code> <图片节点> --epoch N --revision N --input <笔画文件> -o <PNG>
  canvas media stage <code> <来源节点> --file <文件> --source-asset ID --epoch N --revision N --op-id ID
  canvas media attach <code> --epoch N --op-id ID
  canvas media outpaint <code> <图片节点> --epoch N --revision N [--left PX --right PX --top PX --bottom PX] -o <PNG>
  canvas media outpaint-submit <code> <底片节点> --source-asset ID --epoch N --revision N --model KEY --max-credits N --idempotency-key ID
  canvas media separate-voice <code> <音频节点> --source-asset ID --epoch N --revision N --model KEY --max-credits N --idempotency-key ID
  canvas compose <code> (--nodes 视频节点,... | --node 成片节点) --epoch E --revision N --op-id ID [--wait]
  canvas compose-status|compose-cancel <code> --job ID --epoch E
  canvas automation issue <code> --scope read|edit|generate --output <凭据文件> [--hours N] [--budget-credits N]
  canvas automation list|revoke <code> [凭据 ID]
  canvas character|director|storyboard get|set <code> <节点> [--input 文件 --revision N]
  canvas director scene get|apply <code> <节点> [--epoch E --revision N --input 文件]
  canvas character generate <code> <节点> --kind image|voice|image-and-voice --max-credits N
  canvas storyboard split <code> <节点> --script-file 文件 --mode append|replace --max-credits N
  协作生成可加 --budget-key ID --budget-credits N，跨请求共用累计积分上限
  canvas storyboard status|wait <code> <任务>
  canvas storyboard shot list|add|set|move|rm <code> <节点> [镜头]
  canvas create --title <标题> [--payload 文件]
  canvas import <导出文件.json>
  canvas export <code> [-o 文件] [--force]   -o 与 --output 等价；- 表示输出到 stdout；已有同名文件不覆盖，--force 才覆盖
  canvas graph <code> [-o 文件]               输出图与 revision（revision 是 canvas save 必填的基准版本）
  canvas save <code> --payload 文件 --revision N
                                        --revision 必填，取自 canvas graph；要 canvas graph 的输出，不是 canvas export 的导出文档；
                                        新图会让现有节点消失时，需要在终端里确认

画布节点级编辑（每次都要在终端确认；这是约定性的软闸门，不是安全边界）
  canvas node add <code> --kind <script|image|video|audio> [--label 标题] [--text 正文] [--prompt 提示词]
                        [--model 模型] [--at x,y]
  canvas node set <code> <节点id> [--label 标题] [--text 正文] [--prompt 提示词] [--model 模型]
  canvas node rm <code> <节点id> [更多节点id...]
                                        只提交差异；服务端按 revision 乐观锁写入

本地素材上传（用现有登录态，无需额外权限）
  asset download <素材ID> [--canvas <code>] -o <文件>  下载有权访问的媒体
  asset upload <文件路径>                         上传本地图片 / 视频 / 音频到素材库，
                                                  返回素材 id，可直接用于 generate 的 --ref-* 参数

模型与积分
  model list [--kind image|video|audio|text]      看有哪些模型、key 是什么
  model show <模型key>                            看这个模型的参数（--param 能填什么）与参考素材上限
  credits                                         余额 / 冻结 / 可用积分

生成（提交后产物由服务端写回节点，无需打开画布）
  generate image|video|audio|text --model <模型key> [--canvas <画布编号> --node <节点id>]
            [--prompt 提示词] [--param 名=值]... [--input '<json>'|@文件|-]
            [--ref-image 素材id]... [--ref-video 素材id]... [--ref-audio 素材id]...
            [--first-frame 素材id [--last-frame 素材id]]   锁首帧 / 首尾帧（视频）
            [--count 1] [--wait] [--timeout 600] [--interval 3] [--idempotency-key 键]
                                        幂等键提交前就打印；请求结果不确定时带同一个键重跑，不会重复扣费

生成任务
  task get <id|task_ref>
  task cancel <id|task_ref>
  task wait <id|task_ref> [--timeout 600] [--interval 3]

画布 Agent（小镜）：把要改/要生成的事交给它，需要拍板的动作停下来等你决定
  agent run <画布编号> --message "把 S03 重新生成成 10 秒视频" [--selection 节点id,节点id]
            [--attach 素材ID] [--budget 积分] [--session 会话编号] [--watch] [--interval 2] [--timeout 600]
  agent sessions <画布编号>
  agent history <会话编号> [--before 序号] [--limit N]
  agent events <会话编号> [--after 序号]      看已经发生的事（含审批条目）
  agent approve <审批编号> [--session 会话编号] [--items 0,1] [--reject] [--answer 文本]
                                        批准前会列出将批准的条目（需要 --session）；终端里还要再确认一次
  agent cancel <运行编号>
                                        只提交时打印运行编号；--watch 跟到结束或等到需要审批
  agent interject <运行编号> --message <消息> [--attach 素材ID]
  agent resume <运行编号> [--add-budget 积分]
  agent undo <运行编号>

短剧（只读与任务控制；建项目、改剧本、跑任务仍在网页里）
  drama list [--keyword 关键词] [--page N] [--page-size N]
  drama get <短剧编号> [--episodes 10]          默认只列前 10 集，0 表示全部
  drama units <短剧编号> [--from 1] [--to 20]
  drama unit <短剧编号> <单元号>
  drama jobs <短剧编号> [--active]
  drama job-cancel <短剧编号> --job 任务id

运行记录（转发 agent-trace 脚本；出于安全必须用 --script 或 VC_AGENT_TRACE 明确指定脚本）
  trace pull [--since 7d] [--feedback down] ...
  trace show <run_id>
  trace compare --by harness_key [--since 30d]

全局选项
  --json          输出一行 JSON（{ok,data} / {ok:false,error}），供脚本与 Agent 解析
  --base URL      API 地址（也可用环境变量 VC_API_BASE）；登录态只属于登录时的站点，换站点要重新 login
                  非 https 的远程地址会警告，令牌会明文传输
  --no-update-check  本次不检查 GitHub 上的新版本（也可用 VC_NO_UPDATE_CHECK=1）

退出码：0 成功，1 业务或网络错误（含任务失败），2 用法错误，
        3 等待超时（--wait / task wait），4 新建项目或节点级修改被取消

当前是 beta 渠道：命令与参数尽量稳定，但输出格式和提示语仍可能调整。
升级提示写在 stderr；非交互终端与 --json 场景不会打扰脚本。
login 的授权地址始终写在 stderr，--json 时也一样——stdout 只留最后一行 JSON。`;

/** 画布子命令。 */
async function runCanvas(ctx: Ctx, positional: string[], flags: Flags): Promise<void> {
  const [sub, ...rest] = positional;
  const args: Args = { positional: rest, flags };
  switch (sub) {
    case "automation":
      return canvasAutomation(ctx, args);
    case "list":
      return canvasList(ctx, args);
    case "get":
      return canvasGet(ctx, args);
    case "status":
      return canvasStatus(ctx, args);
    case "rename":
    case "archive":
      return canvasManage(ctx, args, sub);
    case "watch":
      return canvasWatch(ctx, args);
    case "task":
      return canvasTaskList(ctx, args);
    case "presence":
      return canvasPresence(ctx, args);
    case "plan":
      return canvasPlan(ctx, args);
    case "apply":
      return canvasApply(ctx, args);
    case "resume":
      return canvasApply(ctx, args, true);
    case "op":
      if (rest[0] !== "status")
        throw new APIError(-1, "用法：canvas op status <画布> <操作编号>", 2);
      return canvasOpStatus(ctx, { positional: rest.slice(1), flags });
    case "comment":
      return canvasComment(ctx, args);
    case "mention":
      return canvasMention(ctx, args);
    case "history":
      return canvasHistory(ctx, args);
    case "undo":
      return canvasUndo(ctx, args, false);
    case "redo":
      return canvasUndo(ctx, args, true);
    case "collaboration":
      return canvasCollaborationMode(ctx, args);
    case "member":
      return canvasMember(ctx, args);
    case "share":
      return canvasShare(ctx, args);
    case "asset":
      return canvasAsset(ctx, args);
    case "output":
      return canvasOutput(ctx, args);
    case "media":
      return canvasMedia(ctx, args);
    case "character":
      if (rest[0] === "generate") return canvasCharacterGenerate(ctx, args);
      return canvasStructuredNode(ctx, args, sub);
    case "director":
      if (rest[0] === "scene")
        return canvasDirectorScene(ctx, { positional: rest.slice(1), flags });
      return canvasStructuredNode(ctx, args, sub);
    case "storyboard":
      if (rest[0] === "shot") return storyboardShot(ctx, { positional: rest.slice(1), flags });
      if (rest[0] === "split") return storyboardSplit(ctx, args);
      if (rest[0] === "status" || rest[0] === "wait") return storyboardJob(ctx, args);
      return canvasStructuredNode(ctx, args, sub);
    case "create":
      return canvasCreate(ctx, args);
    case "import":
      return canvasImport(ctx, args);
    case "export":
      return canvasExport(ctx, args);
    case "graph":
      return canvasGraph(ctx, args);
    case "save":
      return canvasSave(ctx, args);
    case "node":
      return runCanvasNode(ctx, rest, flags);
    case "edge":
      return canvasEdge(ctx, args);
    case "group":
      return canvasGroup(ctx, args);
    case "compose":
      return canvasCompose(ctx, { positional: rest, flags });
    case "compose-status":
      return canvasComposeStatus(ctx, { positional: rest, flags });
    case "compose-cancel":
      return canvasComposeCancel(ctx, { positional: rest, flags });
    default:
      throw new APIError(-1, `未知的 canvas 子命令：${sub || "（空）"}\n\n${USAGE}`, 2);
  }
}

/** 节点级编辑子命令：每次都走人工确认，没有跳过确认的开关。 */
async function runCanvasNode(ctx: Ctx, positional: string[], flags: Flags): Promise<void> {
  const [sub, code, ...rest] = positional;
  const args: Args = { positional: [code as string, ...rest], flags };
  switch (sub) {
    case "list":
    case "get":
      return canvasNodeRead(ctx, args, sub);
    case "move":
      return canvasNodeMove(ctx, args);
    case "arrange":
      return canvasNodeArrange(ctx, args);
    case "duplicate":
      return canvasNodeDuplicate(ctx, args);
    case "refs":
      return canvasNodeRefs(ctx, args);
    case "add":
      return canvasNodeAdd(ctx, args);
    case "set":
      return canvasNodeSet(ctx, args);
    case "rm":
    case "delete":
      return canvasNodeRm(ctx, args);
    default:
      throw new APIError(-1, `未知的 canvas node 子命令：${sub || "（空）"}\n\n${USAGE}`, 2);
  }
}

/** 生成任务子命令。 */
async function runTask(ctx: Ctx, positional: string[], flags: Flags): Promise<void> {
  const [sub, ...rest] = positional;
  const args: Args = { positional: rest, flags };
  switch (sub) {
    case "get":
      return taskGet(ctx, args);
    case "cancel":
      return taskCancel(ctx, args);
    case "wait":
      return taskWait(ctx, args);
    default:
      throw new APIError(-1, `未知的 task 子命令：${sub || "（空）"}\n\n${USAGE}`, 2);
  }
}

/** 模型子命令。 */
async function runModel(ctx: Ctx, positional: string[], flags: Flags): Promise<void> {
  const [sub, ...rest] = positional;
  const args: Args = { positional: rest, flags };
  switch (sub) {
    case "list":
      return modelList(ctx, args);
    case "show":
      return modelShow(ctx, args);
    default:
      throw new APIError(-1, `未知的 model 子命令：${sub || "（空）"}\n\n${USAGE}`, 2);
  }
}

/** 小镜子命令：run / events / approve / cancel。 */
async function runAgent(ctx: Ctx, positional: string[], flags: Flags): Promise<void> {
  const [sub, ...rest] = positional;
  const args: Args = { positional: rest, flags };
  switch (sub) {
    case "run":
      return agentRun(ctx, args);
    case "events":
      return agentEvents(ctx, args);
    case "sessions":
      return agentSessions(ctx, args);
    case "history":
      return agentHistory(ctx, args);
    case "approve":
      return agentApprove(ctx, args);
    case "cancel":
      return agentCancel(ctx, args);
    case "interject":
    case "resume":
    case "undo":
      return agentControl(ctx, args, sub);
    default:
      throw new APIError(-1, `未知的 agent 子命令：${sub || "（空）"}\n\n${USAGE}`, 2);
  }
}

/** 短剧子命令。 */
async function runDrama(ctx: Ctx, positional: string[], flags: Flags): Promise<void> {
  const [sub, ...rest] = positional;
  const args: Args = { positional: rest, flags };
  switch (sub) {
    case "list":
      return dramaList(ctx, args);
    case "get":
      return dramaGet(ctx, args);
    case "units":
      return dramaUnits(ctx, args);
    case "unit":
      return dramaUnit(ctx, args);
    case "jobs":
      return dramaJobs(ctx, args);
    case "job-cancel":
      return dramaJobCancel(ctx, args);
    default:
      throw new APIError(-1, `未知的 drama 子命令：${sub || "（空）"}\n\n${USAGE}`, 2);
  }
}

function buildContext(flags: Flags): Ctx {
  const env = process.env;
  const creds = loadCredentials(env);
  let base: string | null = null;
  let baseError: unknown = null;
  try {
    if (flags.base || env.VC_API_BASE || creds?.base) {
      base = normalizeBase(flags.base || env.VC_API_BASE || creds?.base);
    }
  } catch (err) {
    // 不要吞掉原因：--base 少了 http:// 时要让用户看到「必须以 http:// 开头」，
    // 而不是后面拼出 `null/canvas` 报一句「无法连接 null」。
    baseError = err;
  }
  // 令牌绑定登录时的站点：--base / VC_API_BASE 指向别处时不把令牌带过去（login 自己不用令牌）
  let token = creds?.token;
  let baseMismatch: Ctx["baseMismatch"] = null;
  if (token && base && !(creds?.base && sameBase(creds.base, base))) {
    baseMismatch = { saved: creds?.base ? normalizeBase(creds.base) : null, requested: base };
    token = undefined;
  }
  return {
    env,
    creds,
    credsPath: credentialsPath(env),
    base,
    baseError,
    baseMismatch,
    token,
    json: flags.json === true || flags.json === "true",
    stdout: (line: string) => process.stdout.write(`${line}\n`),
    stderr: (line: string) => process.stderr.write(`${line}\n`),
    fetchImpl: fetch,
  };
}

async function main(argv: string[]): Promise<void> {
  const [command, ...rest] = argv;
  if (!command || command === "help" || command === "-h" || command === "--help") {
    process.stdout.write(`${USAGE}\n`);
    return;
  }
  if (command === "version" || command === "--version" || command === "-v") {
    // 版本只认 package.json：安装包、站点界面、这里三处不会各写一个数（以前就漂过）
    const data = {
      name: "video-canvas-cli",
      version: cliVersion(),
      channel: CLI_CHANNEL,
      node: process.version,
      os: process.platform,
      arch: process.arch,
    };
    if (rest.includes("--json")) {
      process.stdout.write(`${JSON.stringify({ ok: true, data })}\n`);
    } else {
      process.stdout.write(`${data.name} ${data.version} (${CLI_CHANNEL})\n`);
    }
    return;
  }

  const { positional, flags } = parseArgs(rest);
  const ctx = buildContext(flags);
  if (flags.help) {
    process.stdout.write(`${USAGE}\n`);
    return;
  }
  configureAutomation(ctx, flags);
  if (ctx.automation && command !== "canvas" && command !== "generate" && command !== "task")
    throw new APIError(-1, "自动化凭据只能访问授权的协作画布", 403);
  // 明文 http 的远程站点：令牌与内容都会裸奔，每次都提醒（本机调试地址不提醒）
  if (ctx.base && command !== "trace" && command !== "skill" && isInsecureRemoteBase(ctx.base)) {
    ctx.stderr(insecureBaseWarning(ctx.base));
  }

  // 升级提醒在后台查，命令照跑；提示只写 stderr，且命令结束时最多等 400ms（见 startUpdateCheck）
  const updater = startUpdateCheck(ctx, rest);
  try {
    switch (command) {
      case "login":
        return await login(ctx, { positional, flags });
      case "logout":
        return await logout(ctx);
      case "whoami":
        return await whoami(ctx);
      case "asset": {
        const [sub, ...rest] = positional;
        if (sub === "upload") return await assetUpload(ctx, { positional: rest, flags });
        if (sub === "download") return await assetDownload(ctx, { positional: rest, flags });
        if (sub === "library" && rest[0] === "list")
          return await assetLibraryList(ctx, { positional: rest.slice(1), flags });
        throw new APIError(-1, `asset 子命令支持 upload、download 和 library list\n\n${USAGE}`, 2);
      }
      case "canvas":
        return await runCanvas(ctx, positional, flags);
      case "task":
        return await runTask(ctx, positional, flags);
      case "model":
        return await runModel(ctx, positional, flags);
      case "drama":
        return await runDrama(ctx, positional, flags);
      case "agent":
        return await runAgent(ctx, positional, flags);
      case "generate":
        return await generate(ctx, { positional, flags });
      case "credits":
        return await credits(ctx);
      case "trace":
        return trace(ctx, { positional, flags });
      case "skill":
        return await skill(ctx, { positional, flags });
      default:
        throw new APIError(-1, `未知命令：${command}\n\n${USAGE}`, 2);
    }
  } finally {
    await updater.finish();
  }
}

/**
 * CLI 目前是 beta：命令名与参数会尽量稳定，但输出格式、提示语、子命令仍可能调整。
 * 这个标记同时出现在 --help、--version 与站点文档里，不要只在其中一处改。
 */
const CLI_CHANNEL = "beta";

interface Updater {
  finish(): Promise<void>;
}

/**
 * 自动升级提醒：启动时向 npm registry 查一次最新版本，命令结束时最多等 400ms 再提示。
 *
 * 刻意做成「不落盘、不阻塞、失败静默」：
 * - 不写缓存文件，避免在只读 HOME / 容器里报错；一次请求只有几百字节；
 * - 非交互（stderr 不是终端）不查，脚本与 CI 的日志里不会突然多一行；
 * - --json 时提示仍走 stderr，不影响 stdout 的单行 JSON；
 * - VC_NO_UPDATE_CHECK=1 或 --no-update-check 可彻底关掉。
 */
function startUpdateCheck(ctx: Ctx, argv: string[]): Updater {
  const skip =
    process.env.VC_NO_UPDATE_CHECK === "1" ||
    argv.includes("--no-update-check") ||
    !process.stderr.isTTY;
  if (skip) return { finish: async () => {} };

  const local = cliVersion();
  const pending = fetchLatestVersion();

  return {
    finish: async () => {
      const timeout = new Promise<null>((resolve) => {
        const timer = setTimeout(() => resolve(null), 400);
        // 不要因为这条提示把进程多留 400ms
        if (typeof timer.unref === "function") timer.unref();
      });
      const latest = await Promise.race([pending, timeout]);
      if (!latest || !isNewer(String(latest), local)) return;
      ctx.stderr("");
      ctx.stderr(upgradeHint(latest, local, CLI_CHANNEL));
      ctx.stderr("  不想每次都查：VC_NO_UPDATE_CHECK=1");
    },
  };
}

/**
 * CLI 自己的版本号，与站点版本无关：命令行可以单独发版、单独修 bug。
 * 安装包会把 package.json 一起带上，所以装完之后读到的就是安装时的那个版本。
 *
 * 源码在 src/、产物在 dist/，两种位置的上一级都是 cli/（package.json 所在）。
 */
function cliVersion(): string {
  try {
    const here = path.dirname(fileURLToPath(import.meta.url));
    const raw = fs.readFileSync(path.join(here, "..", "package.json"), "utf8");
    const parsed = JSON.parse(raw) as { version?: string };
    return typeof parsed.version === "string" && parsed.version ? parsed.version : "0.0.0";
  } catch {
    // 源码被单独拷走、package.json 不在旁边：不报错，给一个显然不是正式版的值
    return "0.0.0";
  }
}

const wantsJson = process.argv.slice(2).some((arg) => arg === "--json" || arg === "--json=true");

main(process.argv.slice(2)).catch((err: unknown) => {
  if (err instanceof APIError) {
    if (wantsJson) {
      process.stdout.write(
        `${JSON.stringify({ ok: false, error: { code: err.code, message: err.message, ...(err instanceof CollaborationError && { reason: err.reason, details: err.details }) } })}\n`,
      );
    } else {
      process.stderr.write(`错误：${err.message}\n`);
    }
    // exitCode 优先：task wait 超时用 3，与「任务失败」的 1 区分开
    process.exitCode = err.exitCode ?? (err.status === 2 ? 2 : 1);
    return;
  }
  const message = err instanceof Error ? err.message : String(err);
  if (wantsJson) {
    process.stdout.write(`${JSON.stringify({ ok: false, error: { code: -1, message } })}\n`);
  } else {
    process.stderr.write(`错误：${err instanceof Error ? err.stack : message}\n`);
  }
  process.exitCode = 1;
});
