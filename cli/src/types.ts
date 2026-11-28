/**
 * 命令行里流转的数据形状。
 *
 * 这些类型只描述「CLI 真正读到的字段」，接口多给的字段一律放过（索引签名），
 * 目的是让读错字段名在编译期就被拦下来，同时不因为后端加字段就崩掉类型。
 */

/** 参数值：字符串、没有取值的开关（`--foo`）、或可重复 flag 的数组。 */
export type FlagValue = string | true | Array<string | true>;
export type Flags = Record<string, FlagValue | undefined>;

/** 一条命令的入参（子命令已经把 command 摘掉）。 */
export interface Args {
  positional: string[];
  flags: Flags;
}

/** 保存在 ~/.config/video-canvas/credentials.json 的内容。 */
export interface Credentials {
  base?: string;
  token?: string;
  expire_at?: number;
  role?: string;
  client_name?: string;
  [key: string]: unknown;
}

/** 所有命令共用的执行上下文。 */
export interface Ctx {
  automation?: { base: string; canvasId: string; token: string };
  env: NodeJS.ProcessEnv;
  creds: Credentials | null;
  credsPath: string;
  /** 已规范化的 API 根地址；地址不合法时为 null，原因在 baseError。 */
  base: string | null;
  baseError: unknown;
  /**
   * 已保存的令牌不属于当前要访问的站点：令牌只在登录时的 base 上有效，换了 --base / VC_API_BASE 就不能带过去。
   * 有值时 token 一定是 undefined，requireToken 会据此给出「重新登录」的提示。
   */
  baseMismatch: { saved: string | null; requested: string } | null;
  token?: string;
  json: boolean;
  stdout(line: string): void;
  stderr(line: string): void;
  fetchImpl: typeof fetch;
}

// ---------------------------------------------------------------------------
// 画布
// ---------------------------------------------------------------------------

/** 节点上一版产物。 */
export interface NodeOutput {
  stem?: "vocals" | "accompaniment";
  id?: string | number;
  assetId?: string;
  [key: string]: unknown;
}

/** 分镜行里与节点关联的两个字段（导入素材时写的就是它们）。 */
export interface StoryboardShot {
  videoNodeId?: string;
  imageNodeId?: string;
  [key: string]: unknown;
}

export interface CanvasNodeData {
  kind?: string;
  label?: string;
  text?: string;
  prompt?: string;
  model?: string;
  /** 老节点只有 assetId；新节点用 activeOutputId 指向 outputs 里的某一版。 */
  assetId?: string;
  activeOutputId?: string | number | null;
  outputs?: NodeOutput[];
  composeOrder?: string[];
  storyboard?: { shots?: StoryboardShot[]; [key: string]: unknown };
  [key: string]: unknown;
}

export interface CanvasNode {
  id: string;
  /** 组节点是 "group"，不能删。 */
  type?: string;
  position?: { x?: number; y?: number; [key: string]: unknown };
  data?: CanvasNodeData;
  [key: string]: unknown;
}

export interface CanvasEdge {
  id?: string;
  source: string;
  target: string;
  [key: string]: unknown;
}

export interface Graph {
  nodes?: CanvasNode[];
  edges?: CanvasEdge[];
  viewport?: { x?: number; y?: number; zoom?: number; [key: string]: unknown };
  [key: string]: unknown;
}

export type WireVersion = number | string;

export interface Canvas {
  id: string;
  title: string;
  revision: WireVersion;
  updated_at?: string;
  payload_json?: Graph;
  collaboration_active?: boolean;
  collaboration_epoch?: WireVersion;
  collaboration_role?: "owner" | "editor" | "viewer";
  [key: string]: unknown;
}

/** 分页信封：列表接口有的返回 {list,total}，有的返回 {items}，还有裸数组。 */
export interface Page<T> {
  list?: T[];
  items?: T[];
  total?: number;
  page?: number;
  page_size?: number;
}

/** 节点级编辑服务端返回的差异。 */
export interface OpsChange {
  op: "create" | "update" | "delete";
  kind?: "node" | "edge" | string;
  id: string;
  after?: Record<string, unknown>;
}

export interface OpsResult {
  revision: number;
  changes?: OpsChange[];
  warnings?: string[];
}

// ---------------------------------------------------------------------------
// 生成任务
// ---------------------------------------------------------------------------

export interface TaskOutput {
  stem?: "vocals" | "accompaniment";
  url?: string;
  [key: string]: unknown;
}

export interface Task {
  id: number;
  task_ref?: string;
  status: string;
  progress?: number | null;
  kind?: string;
  model_id?: string;
  credits?: number;
  charged_credits?: number | null;
  error_code?: number | string | null;
  error_message?: string | null;
  outputs?: Array<TaskOutput | string>;
  [key: string]: unknown;
}

/** POST /generation-tasks 的逐项结果：成功给 task，失败给 error。 */
export interface GenerateItem {
  node_id?: string;
  task?: Task;
  error?: { code?: number; status?: number | string; message?: string };
}

export interface GenerateResponse {
  items?: GenerateItem[];
}

// ---------------------------------------------------------------------------
// 模型与积分
// ---------------------------------------------------------------------------

export interface ModelParamField {
  type?: "enum" | "number" | "bool" | string;
  label?: string;
  options?: string[];
  min?: number;
  max?: number;
  unit?: string;
  default?: unknown;
  /** false 表示画布上不显示、按默认值发送。 */
  open?: boolean;
  [key: string]: unknown;
}

export interface ModelRefSpec {
  on?: boolean;
  max?: number;
  max_mb?: number;
  [key: string]: unknown;
}

export interface ModelCaps {
  ops?: string[];
  /** 接口里是「名字 -> 字段」的对象，键序就是参数面板顺序。 */
  params?: Record<string, ModelParamField>;
  prompt?: { max?: number; [key: string]: unknown };
  refs?: Record<string, ModelRefSpec>;
  [key: string]: unknown;
}

export interface Model {
  key: string;
  kind?: string;
  label?: string;
  hint?: string;
  vendor?: string;
  capabilities?: ModelCaps;
  pricing?: unknown;
  [key: string]: unknown;
}

export interface Credits {
  balance: number;
  frozen: number;
  available: number;
  [key: string]: unknown;
}

/** GET /me */
export interface Me {
  username: string;
  nickname?: string;
  role?: string;
  [key: string]: unknown;
}

// ---------------------------------------------------------------------------
// 合成成片
// ---------------------------------------------------------------------------

export interface ComposeJob {
  id: number | string;
  status: string;
  segment_count?: number;
  duration_ms?: number;
  result_asset_id?: number | string;
  result_asset_url?: string;
  result_node?: string;
  error?: string | null;
  [key: string]: unknown;
}

/** 推导出来的一段素材。 */
export interface ComposeRow {
  seq: number;
  node: string;
  label: string;
  assetId: string;
}

// ---------------------------------------------------------------------------
// 短剧
// ---------------------------------------------------------------------------

export interface DramaSummary {
  id: string;
  title: string;
  status: string;
  phase?: string;
  episode_count?: number;
  locked_count?: number;
  updated_at?: string;
  [key: string]: unknown;
}

export interface DramaEpisode {
  no: number;
  title?: string;
  unit_from?: number;
  unit_to?: number;
  script_status?: string;
  [key: string]: unknown;
}

export interface DramaView extends DramaSummary {
  source_chars?: number;
  units_total?: number;
  unit_kind?: string;
  target_episodes?: number;
  episode_seconds?: number;
  ratio?: string;
  canvas_id?: string;
  pause_reason?: string;
  pause_code?: string;
  episodes?: DramaEpisode[];
}

export interface DramaUnit {
  no: number;
  title?: string;
  chars?: number;
  text?: string;
  status?: string;
  [key: string]: unknown;
}

export interface DramaJob {
  id: number | string;
  step?: string;
  status: string;
  progress?: number;
  episodes?: number[];
  lane?: string;
  credits?: number;
  [key: string]: unknown;
}

// ---------------------------------------------------------------------------
// 登录（设备授权）
// ---------------------------------------------------------------------------

export interface DeviceStart {
  device_code: string;
  user_code: string;
  /** 相对站点根的路径，如 /cli/device?code=XXXX */
  verification_uri_complete: string;
  interval?: number;
  expires_in?: number;
}

export interface DevicePoll {
  status: "pending" | "approved" | "denied" | "expired" | "consumed" | "slow_down" | string;
  token?: string;
  expire_at?: number;
  role?: string;
}

// ---------------------------------------------------------------------------
// 导入 / 导出
// ---------------------------------------------------------------------------

export interface ImportReport {
  assets?: { copied?: number; total?: number; missing?: unknown[] };
  edges_dropped?: number;
  [key: string]: unknown;
}

export interface ImportResult {
  canvas?: Canvas;
  report?: ImportReport;
  [key: string]: unknown;
}

export interface ExportResult {
  document?: unknown;
  report?: { skipped_assets?: number; [key: string]: unknown };
  [key: string]: unknown;
}
