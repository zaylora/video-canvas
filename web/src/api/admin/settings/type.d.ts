/** 注册设置（GET / PUT /admin/settings/register） */
export interface RegisterSettings {
  /** 是否开放注册 */
  register_enabled: boolean;
  /** 新用户初始积分，不小于 0 */
  initial_credits: number;
  /** 默认并发上限，1 到 64 */
  default_max_active_tasks: number;
  /** 排队倍数，1 到 20：用户的排队上限 = 并发上限 × 倍数，超过后新任务才会被拒绝 */
  queue_multiplier: number;
  /** 排队超时（分钟），5 到 1440：排队超过这个时间的任务会过期并退回积分 */
  queue_max_wait_minutes: number;
}

/** SMTP 加密方式 */
export type SmtpEncryption = "none" | "starttls" | "tls";

/** SMTP 设置视图：密码永不返回，只告知是否已设置 */
export interface SmtpSettings {
  /** SMTP 服务器主机名 */
  host: string;
  /** 端口 */
  port: number;
  /** 加密方式 */
  encryption: SmtpEncryption;
  /** 登录用户名 */
  username: string;
  /** 是否已设置密码（密码本身只写不读） */
  has_password: boolean;
  /** 发件人地址 */
  from_address: string;
  /** 发件人名称 */
  from_name: string;
  /** 是否启用 */
  enabled: boolean;
  /** 最近一次测试时间（ISO 字符串），null 表示从未测试 */
  last_check_at: string | null;
  /** 最近一次测试是否成功，null 表示从未测试 */
  last_check_ok: boolean | null;
  /** 最近一次测试失败原因（已脱敏），成功或从未测试为空 */
  last_check_error: string;
}

/** 保存 SMTP 设置的请求体：不含密码 */
export type SmtpUpdateRequest = Pick<
  SmtpSettings,
  "host" | "port" | "encryption" | "username" | "from_address" | "from_name" | "enabled"
>;

/** 定价设置保存请求（PUT /admin/settings/pricing） */
export interface PricingSettingsReq {
  /** 1 元 = N 积分，大于 0，最多两位小数 */
  credits_per_yuan: number;
  /** 默认加价率（百分数），整数 0 到 1000 */
  default_markup_pct: number;
  /** 手动覆盖的美元汇率，大于 0，最多四位小数；null 表示使用实时汇率 */
  usd_cny_override: number | null;
}

/** 当前生效的美元兑人民币汇率 */
export interface PricingUsdCny {
  /** 生效汇率：覆盖优先，否则是最近一次实时值；从未拉到且没有覆盖时为 null */
  rate: number | null;
  /** 汇率日期 YYYY-MM-DD；使用覆盖时为 null */
  date: string | null;
  /** 最近一次成功拉取实时汇率的时间，ISO 8601 */
  fetched_at: string | null;
  /** 来源：live 实时、override 手动覆盖、none 没有 */
  source: "live" | "override" | "none";
  /** 实时值距今超过 7 天 */
  stale: boolean;
}

/** 定价设置视图（GET / PUT /admin/settings/pricing） */
export interface PricingSettingsView {
  /** 1 元 = N 积分；null 表示尚未设置 */
  credits_per_yuan: number | null;
  /** 默认加价率（百分数），缺省 30 */
  default_markup_pct: number;
  /** 手动覆盖的美元汇率；null 表示使用实时汇率 */
  usd_cny_override: number | null;
  /** 当前生效的汇率 */
  usd_cny: PricingUsdCny;
}
