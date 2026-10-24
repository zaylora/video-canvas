#!/usr/bin/env node
// agent-trace：把线上 Agent 运行记录拉到本地 .traces/，供 /agent-review skill 分析。
//
// 只依赖 Node 18+ 内置能力（fetch、fs、path、process.argv），无第三方依赖。
// 设计见 docs/design/Agent运行观测/Agent运行观测.md 的 6.4–6.7、8.7 节。
//
// 本 CLI 不对内容做任何脱敏处理（导出按原文），因此 .traces/ 必须被 .gitignore，
// 首次 pull 时会自动补上。

import fs from 'node:fs'
import path from 'node:path'
import { pathToFileURL } from 'node:url'

// 429 退避等待时间（毫秒），共重试 3 次，之后放弃。
const RETRY_DELAYS_MS = [1000, 2000, 4000]
// compare 时每组至少需要的样本数，不足则不给“改善/变差”的结论。
const MIN_SAMPLES = 30
// compare 判定“有明显差异”的比率差阈值（百分点 / 100）。
const DELTA_THRESHOLD = 0.05
// 摘要接口单页最多取多少条（后端上限 500，这里取保守值）。
const PAGE_SIZE = 200
// 状态属于这些值时不算“信号样本”：正常结束或仍在正常流转。
const NORMAL_STATUSES = new Set(['succeeded', 'running', 'queued', 'waiting_approval', 'waiting_input'])
const GROUP_KEYS = ['harness_key', 'prompt_version', 'model_key']
// 允许的别名，方便手敲。
const GROUP_ALIASES = { harness: 'harness_key', model: 'model_key' }

const USAGE = `用法：
  agent-trace pull [--since 7d|24h|2026-10-01] [--until <时间>] [--feedback down|up|any|none]
                   [--undone] [--status a,b] [--harness <key>] [--model <key>]
                   [--prompt-version N] [--user <id>] [--limit 200] [--sample N]
                   [--with-requests]   同时拉每次模型调用实际发给上游的完整请求体（服务端只留 7 天）
  agent-trace show <run_id>
  agent-trace compare --by harness_key|prompt_version|model_key [--since 30d] [--until <时间>]

环境变量（也可写在 .traces/.env，每行 KEY=VALUE）：
  VC_API_BASE      如 https://host/api/v1（末尾不带斜杠）
  VC_TRACE_TOKEN   后台签发的分析令牌（vca_ 开头）`

// 命令行错误：message 直接展示给用户，code 是进程退出码。
class CliError extends Error {
  constructor(message, code = 1) {
    super(message)
    this.name = 'CliError'
    this.code = code
  }
}

// ---------------------------------------------------------------------------
// 参数与配置
// ---------------------------------------------------------------------------

// 每个命令允许的选项：true 表示带值，false 表示布尔开关。
const COMMAND_FLAGS = {
  pull: {
    since: true, until: true, feedback: true, undone: false, status: true, harness: true,
    model: true, 'prompt-version': true, user: true, limit: true, sample: true, 'with-requests': false,
  },
  show: {},
  compare: { by: true, since: true, until: true },
}

function parseArgs(argv) {
  const [command, ...rest] = argv
  if (!command || command === '-h' || command === '--help' || command === 'help') {
    return { command: 'help', flags: {}, positional: [] }
  }
  const spec = COMMAND_FLAGS[command]
  if (!spec) throw new CliError(`未知命令：${command}\n${USAGE}`, 2)
  const flags = {}
  const positional = []
  for (let i = 0; i < rest.length; i++) {
    const arg = rest[i]
    if (!arg.startsWith('--')) {
      positional.push(arg)
      continue
    }
    let name = arg.slice(2)
    let value
    const eq = name.indexOf('=')
    if (eq >= 0) {
      value = name.slice(eq + 1)
      name = name.slice(0, eq)
    }
    if (!(name in spec)) throw new CliError(`${command} 不支持选项 --${name}\n${USAGE}`, 2)
    if (spec[name]) {
      if (value === undefined) {
        value = rest[++i]
        if (value === undefined || value.startsWith('--')) throw new CliError(`选项 --${name} 需要一个值`, 2)
      }
      flags[name] = value
    } else {
      if (value !== undefined) throw new CliError(`选项 --${name} 不需要值`, 2)
      flags[name] = true
    }
  }
  return { command, flags, positional }
}

function parsePositiveInt(value, name) {
  const n = Number(value)
  if (!Number.isInteger(n) || n <= 0) throw new CliError(`--${name} 必须是正整数，收到：${value}`, 2)
  return n
}

// 解析 .traces/.env：KEY=VALUE，忽略空行和 # 注释，允许 export 前缀和成对引号。
function parseEnvFile(text) {
  const out = {}
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim()
    if (!line || line.startsWith('#')) continue
    const m = line.match(/^(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$/)
    if (!m) continue
    let val = m[2].trim()
    if (val.length >= 2 && ((val[0] === '"' && val.at(-1) === '"') || (val[0] === "'" && val.at(-1) === "'"))) {
      val = val.slice(1, -1)
    }
    out[m[1]] = val
  }
  return out
}

// 进程环境变量优先，其次 .traces/.env。缺任何一个都以退出码 2 报错。
function loadConfig(cwd, env) {
  let fileEnv = {}
  const envPath = path.join(cwd, '.traces', '.env')
  try {
    fileEnv = parseEnvFile(fs.readFileSync(envPath, 'utf8'))
  } catch {
    // .env 不存在是正常情况
  }
  const base = (env.VC_API_BASE || fileEnv.VC_API_BASE || '').trim().replace(/\/+$/, '')
  const token = (env.VC_TRACE_TOKEN || fileEnv.VC_TRACE_TOKEN || '').trim()
  const missing = []
  if (!base) missing.push('VC_API_BASE（如 https://host/api/v1）')
  if (!token) missing.push('VC_TRACE_TOKEN（后台签发的分析令牌）')
  if (missing.length) {
    throw new CliError(
      `缺少配置：${missing.join('、')}。\n请设置同名环境变量，或写入 ${path.join('.traces', '.env')}（每行 KEY=VALUE）。`,
      2,
    )
  }
  return { base, token }
}

// ---------------------------------------------------------------------------
// 时间解析
// ---------------------------------------------------------------------------

const UNIT_MS = { m: 60_000, h: 3_600_000, d: 86_400_000 }

// 7d / 24h / 30m 表示距现在多久；2026-10-01 表示本地时区当天 0 点；其余按 ISO 时间处理。
// isUntil 且为纯日期时取次日 0 点，使 --until 2026-10-02 包含 10-02 全天。
function parseTime(value, nowDate, name, isUntil = false) {
  const rel = /^(\d+)([mhd])$/.exec(value)
  if (rel) return new Date(nowDate.getTime() - Number(rel[1]) * UNIT_MS[rel[2]]).toISOString()
  const day = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value)
  if (day) {
    const d = new Date(Number(day[1]), Number(day[2]) - 1, Number(day[3]))
    if (isUntil) d.setDate(d.getDate() + 1)
    if (Number.isNaN(d.getTime())) throw new CliError(`--${name} 日期无效：${value}`, 2)
    return d.toISOString()
  }
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) {
    throw new CliError(`--${name} 无法识别：${value}（支持 7d、24h、2026-10-01 或 ISO 时间）`, 2)
  }
  return d.toISOString()
}

function localDate(dateLike) {
  const d = new Date(dateLike)
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}`
}

// ---------------------------------------------------------------------------
// HTTP
// ---------------------------------------------------------------------------

// 创建 API 客户端：统一鉴权头、429 退避、响应外壳解包。
function createClient({ base, token, fetchImpl, sleep }) {
  async function request(pathname, query = {}) {
    const qs = new URLSearchParams()
    for (const [k, v] of Object.entries(query)) {
      if (v !== undefined && v !== null && v !== '') qs.set(k, String(v))
    }
    const url = `${base}${pathname}${qs.size ? `?${qs}` : ''}`
    for (let attempt = 0; ; attempt++) {
      let res
      try {
        res = await fetchImpl(url, { headers: { Authorization: `Bearer ${token}`, Accept: 'application/json' } })
      } catch (err) {
        throw new CliError(`请求失败（网络错误）：${err.message}`)
      }
      if (res.status === 429) {
        if (attempt >= RETRY_DELAYS_MS.length) {
          throw new CliError('请求被限流（429），已退避重试 3 次仍失败，已停止。已写入的文件保留，稍后再次运行 pull 会自动续拉。')
        }
        await sleep(retryAfterMs(res) ?? RETRY_DELAYS_MS[attempt])
        continue
      }
      return unwrap(res)
    }
  }
  return { request }
}

// 读取 429 响应的 Retry-After 头（秒数或 HTTP 日期），转成毫秒；没有或无法解析返回 null。
function retryAfterMs(res) {
  const raw = res.headers?.get?.('retry-after')
  if (!raw) return null
  const secs = Number(raw)
  if (Number.isFinite(secs) && secs >= 0) return Math.round(secs * 1000)
  const at = Date.parse(raw)
  return Number.isNaN(at) ? null : Math.max(0, at - Date.now())
}

// 解包统一响应外壳 {code, msg, data}；HTTP 非 2xx 或 code≠0 都视为错误。
async function unwrap(res) {
  let body = null
  try {
    body = JSON.parse(await res.text())
  } catch {
    // 非 JSON 响应，下面统一报错
  }
  if (res.status === 401) throw new CliError('令牌失效，请在后台重新签发')
  const text = body && typeof body === 'object' ? body.msg ?? body.message : ''
  const msg = text ? `：${text}` : ''
  if (res.status < 200 || res.status >= 300) throw new CliError(`接口返回 HTTP ${res.status}${msg}`)
  if (!body || typeof body !== 'object') throw new CliError('接口返回内容不是合法 JSON')
  if (body.code !== 0) throw new CliError(`接口返回错误 code=${body.code}${msg}`)
  return body.data
}

// ---------------------------------------------------------------------------
// 本地文件
// ---------------------------------------------------------------------------

// 保证 .gitignore 含 .traces/；已有等价写法（.traces、/.traces、/.traces/）则不动。
function ensureGitignore(cwd) {
  const file = path.join(cwd, '.gitignore')
  let text = ''
  try {
    text = fs.readFileSync(file, 'utf8')
  } catch {
    // 不存在则新建
  }
  const covered = text.split(/\r?\n/).some((l) => ['.traces/', '.traces', '/.traces/', '/.traces'].includes(l.trim()))
  if (covered) return false
  const sep = text && !text.endsWith('\n') ? '\n' : ''
  fs.appendFileSync(file, `${sep}\n# Agent 运行观测的本地数据（含用户原文，禁止提交）\n.traces/\n`)
  return true
}

function readJsonl(file) {
  let text
  try {
    text = fs.readFileSync(file, 'utf8')
  } catch {
    return []
  }
  const rows = []
  for (const line of text.split(/\r?\n/)) {
    if (!line.trim()) continue
    try {
      rows.push(JSON.parse(line))
    } catch {
      // 跳过损坏行，不让一行坏数据挡住整个命令
    }
  }
  return rows
}

// run_id 是前端编码的字符串，文件名里原样使用但只保留字母数字下划线。
function safeRunId(runId) {
  return String(runId).replace(/[^A-Za-z0-9_]/g, '')
}

// 一次运行的模型请求体文件：服务端只留 7 天，本地这份是唯一的长期副本，重拉运行时不能丢。
function requestsFileOf(runsDir, safeId) {
  try {
    return fs.readdirSync(runsDir).find((f) => f.endsWith(`_r${safeId}.requests.json`))
  } catch {
    return undefined
  }
}

function runFiles(runsDir, safeId) {
  try {
    return fs.readdirSync(runsDir).filter((f) => f.endsWith(`_r${safeId}.md`) || f.endsWith(`_r${safeId}.json`))
  } catch {
    return []
  }
}

// ---------------------------------------------------------------------------
// 抽样
// ---------------------------------------------------------------------------

// 信号样本：点踩、被撤销、有被拒绝的审批、非正常状态结束。
function isSignal(d) {
  return d.feedback === -1 || Boolean(d.undone) || (d.approvals_rejected || 0) > 0 || !NORMAL_STATUSES.has(d.status)
}

function shuffle(arr, random) {
  const a = [...arr]
  for (let i = a.length - 1; i > 0; i--) {
    const j = Math.floor(random() * (i + 1))
    ;[a[i], a[j]] = [a[j], a[i]]
  }
  return a
}

// 分层抽样：信号样本优先，最多 ceil(N*2/3) 条；其余从剩下的运行里随机补满 N。总数不超过 N。
function sampleDigests(digests, n, random) {
  if (!n || digests.length <= n) return digests
  const signals = shuffle(digests.filter(isSignal), random)
  const picked = signals.slice(0, Math.ceil((n * 2) / 3))
  const chosen = new Set(picked.map((d) => d.run_id))
  const rest = shuffle(digests.filter((d) => !chosen.has(d.run_id)), random)
  return [...picked, ...rest.slice(0, n - picked.length)]
}

// ---------------------------------------------------------------------------
// 命令：pull
// ---------------------------------------------------------------------------

async function cmdPull(ctx, flags) {
  const { client, cwd, stdout, now, random } = ctx
  const limit = flags.limit ? parsePositiveInt(flags.limit, 'limit') : 200
  const sample = flags.sample ? parsePositiveInt(flags.sample, 'sample') : 0
  const nowDate = now()

  const filter = {
    since: flags.since ? parseTime(flags.since, nowDate, 'since') : undefined,
    until: flags.until ? parseTime(flags.until, nowDate, 'until', true) : undefined,
    status: flags.status,
    feedback: flags.feedback,
    undone: flags.undone ? 'true' : undefined,
    harness_key: flags.harness,
    prompt_version: flags['prompt-version'],
    model_key: flags.model,
    user_id: flags.user,
  }

  if (ensureGitignore(cwd)) stdout.write('已在 .gitignore 中追加 .traces/\n')

  // 1. 分页拉摘要，尊重 --limit。
  const hits = []
  let cursor = ''
  while (hits.length < limit) {
    const data = await client.request('/admin/agent/runs', {
      ...filter,
      cursor,
      limit: Math.min(PAGE_SIZE, limit - hits.length),
    })
    const items = Array.isArray(data?.items) ? data.items : []
    hits.push(...items)
    cursor = data?.next_cursor || ''
    if (!cursor || items.length === 0) break
  }
  hits.length = Math.min(hits.length, limit)

  // 2. 抽样。
  const picked = sampleDigests(hits, sample, random)

  // 3. 逐条拉取（每条 1 次 export 请求）并落盘。无论成功与否，index 都要写回，保证已写文件有对应记录。
  const tracesDir = path.join(cwd, '.traces')
  const runsDir = path.join(tracesDir, 'runs')
  const indexFile = path.join(tracesDir, 'index.jsonl')
  fs.mkdirSync(runsDir, { recursive: true })
  const index = new Map()
  for (const row of readJsonl(indexFile)) if (row?.run_id !== undefined) index.set(String(row.run_id), row)

  // 取一次运行的请求体：服务端返回空（过了保留期或没留存）不算错，也不写空文件
  const withRequests = Boolean(flags['with-requests'])
  let requestsWritten = 0
  const fetchRequests = async (id) => {
    const data = await client.request(`/admin/agent/runs/${encodeURIComponent(id)}/requests`)
    return Array.isArray(data?.items) && data.items.some((item) => item?.body) ? data : null
  }

  let written = 0
  let skipped = 0
  try {
    for (const d of picked) {
      const id = String(d.run_id)
      const safeId = safeRunId(id)
      if (!safeId) {
        skipped++
        ctx.stderr.write(`跳过：run_id 无法生成文件名（${id}）\n`)
        continue
      }
      const existing = runFiles(runsDir, safeId)
      const unchanged =
        index.get(id)?.updated_at === d.updated_at &&
        existing.some((f) => f.endsWith('.md')) &&
        existing.some((f) => f.endsWith('.json'))
      if (unchanged) {
        // 运行没变：只在要请求体、本地又没有时补取，不重复导出
        if (withRequests && !requestsFileOf(runsDir, safeId)) {
          const requests = await fetchRequests(id)
          if (requests) {
            const mdName = existing.find((f) => f.endsWith('.md'))
            const date = mdName.slice(0, mdName.indexOf('_r'))
            fs.writeFileSync(path.join(runsDir, `${date}_r${safeId}.requests.json`), `${JSON.stringify(requests, null, 2)}\n`)
            requestsWritten++
          }
        }
        skipped++
        continue
      }
      // format=both 一次请求同时拿到 Markdown（content）和结构化数据（data）。
      const both = await client.request(`/admin/agent/runs/${encodeURIComponent(id)}/export`, { format: 'both' })
      const date = localDate(d.ended_at || d.updated_at || nowDate)
      for (const f of existing) fs.rmSync(path.join(runsDir, f), { force: true }) // 日期可能变化，先清掉旧文件
      fs.writeFileSync(path.join(runsDir, `${date}_r${safeId}.md`), String(both?.content ?? ''))
      fs.writeFileSync(path.join(runsDir, `${date}_r${safeId}.json`), `${JSON.stringify(both?.data ?? null, null, 2)}\n`)
      // 请求体：拉到新的就换掉旧的；没拉（没带开关，或服务端已过保留期）就把本地已有的那份改成新日期的文件名，不留两份也不丢
      const oldRequests = requestsFileOf(runsDir, safeId)
      const requests = withRequests ? await fetchRequests(id) : null
      const requestsName = `${date}_r${safeId}.requests.json`
      if (requests) {
        if (oldRequests && oldRequests !== requestsName) fs.rmSync(path.join(runsDir, oldRequests), { force: true })
        fs.writeFileSync(path.join(runsDir, requestsName), `${JSON.stringify(requests, null, 2)}\n`)
        requestsWritten++
      } else if (oldRequests && oldRequests !== requestsName) {
        fs.renameSync(path.join(runsDir, oldRequests), path.join(runsDir, requestsName))
      }
      index.set(id, d)
      written++
    }
  } finally {
    // 已跳过的运行摘要没变，但 index 里可能缺（例如被手动删过），顺手补齐。
    for (const d of picked) {
      const id = String(d.run_id)
      if (!index.has(id) && runFiles(runsDir, safeRunId(id)).length) index.set(id, d)
    }
    fs.writeFileSync(indexFile, [...index.values()].map((r) => JSON.stringify(r)).join('\n') + (index.size ? '\n' : ''))
  }

  stdout.write(
    `命中 ${hits.length} 条，抽样 ${picked.length} 条，新写 ${written} 条，跳过 ${skipped} 条` +
      (withRequests ? `，请求体 ${requestsWritten} 条` : '') +
      '\n',
  )
}

// ---------------------------------------------------------------------------
// 命令：show
// ---------------------------------------------------------------------------

async function cmdShow(ctx, positional) {
  if (positional.length !== 1) throw new CliError(`show 需要且只需要一个 run_id\n${USAGE}`, 2)
  const id = positional[0]
  const data = await ctx.client.request(`/admin/agent/runs/${encodeURIComponent(id)}/export`, { format: 'md' })
  const content = String(data?.content ?? '')
  ctx.stdout.write(content.endsWith('\n') ? content : `${content}\n`)
}

// ---------------------------------------------------------------------------
// 命令：compare
// ---------------------------------------------------------------------------

function pct(x) {
  return typeof x === 'number' && Number.isFinite(x) ? `${(x * 100).toFixed(1)}%` : '-'
}

function num(x, digits = 1) {
  return typeof x === 'number' && Number.isFinite(x) ? String(Number(x.toFixed(digits))) : '-'
}

// 把 verdicts.jsonl 按分组合并成 {judged, bad}；同一 run_id 以最后一条为准，unclear 不计入已判定。
function aggregateVerdicts(cwd, by) {
  const tracesDir = path.join(cwd, '.traces')
  const indexById = new Map(readJsonl(path.join(tracesDir, 'index.jsonl')).map((r) => [String(r.run_id), r]))
  const latest = new Map()
  for (const v of readJsonl(path.join(tracesDir, 'verdicts.jsonl'))) {
    if (v?.run_id !== undefined) latest.set(String(v.run_id), v)
  }
  const groups = new Map()
  let unmatched = 0
  for (const [runId, v] of latest) {
    if (!['achieved', 'partial', 'failed'].includes(v.outcome)) continue
    const raw = (by === 'harness_key' ? v.harness_key : undefined) ?? indexById.get(runId)?.[by]
    if (raw === undefined || raw === null || raw === '') {
      unmatched++
      continue
    }
    const key = String(raw)
    const g = groups.get(key) || { judged: 0, bad: 0 }
    g.judged++
    if (v.outcome !== 'achieved') g.bad++
    groups.set(key, g)
  }
  return { groups, unmatched }
}

// 以样本最多的组为基线，其它组在失败结束率、点踩率、AI 判定失败率上与它比较。
function conclude(rows) {
  if (rows.some((r) => r.n < MIN_SAMPLES)) return rows.map(() => '样本不足')
  const base = rows.reduce((a, b) => (b.n > a.n ? b : a))
  return rows.map((r) => {
    if (r === base) return '基线'
    const deltas = ['fail_end_rate', 'down_rate', 'ai_fail_rate']
      .filter((k) => typeof r[k] === 'number' && typeof base[k] === 'number')
      .map((k) => r[k] - base[k])
    const better = deltas.some((d) => d <= -DELTA_THRESHOLD)
    const worse = deltas.some((d) => d >= DELTA_THRESHOLD)
    if (better && worse) return '有升有降'
    if (better) return '改善'
    if (worse) return '变差'
    return '无明显差异'
  })
}

async function cmdCompare(ctx, flags) {
  const { client, cwd, stdout, now } = ctx
  const by = GROUP_ALIASES[flags.by] || flags.by
  if (!GROUP_KEYS.includes(by)) throw new CliError(`compare 需要 --by ${GROUP_KEYS.join('|')}`, 2)
  const nowDate = now()
  const data = await client.request('/admin/agent/stats', {
    group_by: by,
    since: parseTime(flags.since || '30d', nowDate, 'since'),
    until: flags.until ? parseTime(flags.until, nowDate, 'until', true) : undefined,
  })
  const stats = Array.isArray(data?.groups) ? data.groups : []
  if (!stats.length) {
    stdout.write('该时间范围内没有运行记录。\n')
    return
  }
  const { groups: ai, unmatched } = aggregateVerdicts(cwd, by)
  const rows = stats.map((g) => {
    const a = ai.get(String(g.key))
    return {
      ...g,
      judged: a?.judged ?? 0,
      ai_fail_rate: a && a.judged > 0 ? a.bad / a.judged : null,
    }
  })
  const verdictCol = conclude(rows)

  const header = ['分组', '运行数', '反馈率', '点踩率', '撤销率', '失败结束率', '中位耗时(s)', '中位步数', '平均积分', '已判定数', 'AI 判定失败率', '结论']
  const lines = [`| ${header.join(' | ')} |`, `|${header.map(() => ' --- ').join('|')}|`]
  rows.forEach((r, i) => {
    lines.push(
      `| ${[
        r.key, r.n, pct(r.feedback_rate), pct(r.down_rate), pct(r.undo_rate), pct(r.fail_end_rate),
        num(r.median_duration_ms / 1000), num(r.median_steps), num(r.avg_credits, 2),
        r.judged, pct(r.ai_fail_rate), verdictCol[i],
      ].join(' | ')} |`,
    )
  })
  stdout.write(`按 ${by} 对比\n\n${lines.join('\n')}\n\n`)
  stdout.write(`说明：AI 判定失败率 = (failed + partial) / 已判定数，unclear 不计入；任一组运行数不足 ${MIN_SAMPLES} 时不给改善/变差结论；有结论时以运行数最多的组为基线，比率差 ≥ ${DELTA_THRESHOLD * 100} 个百分点才算明显。\n`)
  if (unmatched) stdout.write(`注意：有 ${unmatched} 条判定无法归到分组（本地 index.jsonl 里缺对应运行），未计入。\n`)
}

// ---------------------------------------------------------------------------
// 入口
// ---------------------------------------------------------------------------

/**
 * 命令行主函数，返回进程退出码（0 成功，1 运行失败，2 用法或配置错误）。
 * 所有外部依赖都可注入，便于测试：
 *   fetch  HTTP 实现；cwd  工作目录（.traces/ 放在这里）；stdout/stderr  带 write 的对象；
 *   now    返回当前时间的函数；sleep  等待函数（429 退避用）；random  0–1 随机数；env  环境变量。
 */
export async function main(argv, opts = {}) {
  const stdout = opts.stdout || process.stdout
  const stderr = opts.stderr || process.stderr
  const cwd = opts.cwd || process.cwd()
  try {
    const { command, flags, positional } = parseArgs(argv)
    if (command === 'help') {
      stdout.write(`${USAGE}\n`)
      return 0
    }
    const { base, token } = loadConfig(cwd, opts.env || process.env)
    const sleep = opts.sleep || ((ms) => new Promise((r) => setTimeout(r, ms)))
    const ctx = {
      cwd, stdout, stderr,
      now: opts.now || (() => new Date()),
      random: opts.random || Math.random,
      client: createClient({ base, token, fetchImpl: opts.fetch || globalThis.fetch, sleep }),
    }
    if (command === 'pull') await cmdPull(ctx, flags)
    else if (command === 'show') await cmdShow(ctx, positional)
    else await cmdCompare(ctx, flags)
    return 0
  } catch (err) {
    if (err instanceof CliError) {
      stderr.write(`错误：${err.message}\n`)
      return err.code
    }
    stderr.write(`错误：${err?.stack || err}\n`)
    return 1
  }
}

// 仅在直接运行时执行（被 import 做测试时不执行）。
function isDirectRun() {
  if (!process.argv[1]) return false
  try {
    return import.meta.url === pathToFileURL(fs.realpathSync(process.argv[1])).href
  } catch {
    return false
  }
}

if (isDirectRun()) {
  process.exitCode = await main(process.argv.slice(2))
}
