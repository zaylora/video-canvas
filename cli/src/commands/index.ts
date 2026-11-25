// 命令注册入口：cli.ts 用 COMMANDS 做分发；子命令按领域拆在同目录的各文件里。
import { agentApprove, agentCancel, agentEvents, agentRun } from "./agent.ts";
import { assetUpload } from "./asset.ts";
import { login, logout, whoami } from "./auth.ts";
import {
  canvasCreate,
  canvasExport,
  canvasGet,
  canvasGraph,
  canvasImport,
  canvasList,
  canvasSave,
} from "./canvas.ts";
import { canvasCompose, canvasComposeCancel, canvasComposeStatus } from "./compose.ts";
import { dramaGet, dramaJobCancel, dramaJobs, dramaList, dramaUnit, dramaUnits } from "./drama.ts";
import { generate } from "./generate.ts";
import { credits, modelList, modelShow } from "./model.ts";
import { canvasNodeAdd, canvasNodeRm, canvasNodeSet } from "./node.ts";
import { skill } from "./skill.ts";
import { taskGet, taskWait } from "./task.ts";
import { trace } from "./trace.ts";

/** 命令表：bin 用它做分发与 help。 */
export const COMMANDS = {
  login,
  logout,
  whoami,
  assetUpload,
  canvasList,
  canvasGet,
  canvasCreate,
  canvasImport,
  canvasExport,
  canvasGraph,
  canvasNodeAdd,
  canvasNodeRm,
  canvasNodeSet,
  canvasSave,
  canvasCompose,
  canvasComposeStatus,
  canvasComposeCancel,
  modelList,
  modelShow,
  credits,
  generate,
  dramaList,
  dramaGet,
  dramaUnits,
  dramaUnit,
  dramaJobs,
  dramaJobCancel,
  taskGet,
  taskWait,
  trace,
  skill,
  agentRun,
  agentEvents,
  agentApprove,
  agentCancel,
};

// 各命令对外导出：cli.ts 直接调用，测试也从这里引用。
export {
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
  skill,
  taskGet,
  taskWait,
  trace,
  whoami,
};
// 参数解析与几个共用函数对外也要能用（测试与 cli.ts 引用）。
export { boolFlag, intFlag, parseArgs, stringFlag, toInt } from "./args.ts";
export { askYes } from "./shared.ts";
export type { AskIO } from "./shared.ts";
export { deriveComposeAssets } from "./compose.ts";
