// agent-trace 的测试：用假的 fetch 模拟后端，不碰网络。运行：cd scripts/agent-trace && node --test
import assert from 'node:assert/strict'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { test } from 'node:test'

import { main } from './agent-trace.mjs'

const ENV = { VC_API_BASE: 'https://example.test/api/v1', VC_TRACE_TOKEN: 'vca_test' }

function tmpDir() {
  return fs.mkdtempSync(path.join(os.tmpdir(), 'agent-trace-'))
}

function sink() {
  let buf = ''
  return { write: (s) => { buf += s }, get text() { return buf } }
}

// 构造一条摘要，默认是正常成功的运行。
function digest(i, extra = {}) {
  return {
    run_id: `rid${i}`, session_id: 's1', user_id: 'u1', canvas_id: 'c1', mode: 'agent',
    model_key: 'm1', prompt_version: 9, harness_key: 'h1', status: 'succeeded',
    approvals_rejected: 0, undone: false, feedback: 0,
    ended_at: '2026-10-20T06:00:00Z', updated_at: `2026-10-20T06:00:${String(i % 60).padStart(2, '0')}Z`,
    ...extra,
  }
}

const ok = (data) => new Response(JSON.stringify({ code: 0, msg: 'success', data }), { status: 200 })

// 假后端：支持游标分页（单页上限 pageMax）、导出、统计；记录所有请求。
function fakeBackend({ digests = [], stats = [], pageMax = 1000, onExport, requests = {} } = {}) {
  const calls = []
  const fetch = async (url, init) => {
    const u = new URL(url)
    calls.push({ path: u.pathname, query: Object.fromEntries(u.searchParams), headers: init?.headers })
    const p = u.pathname.replace('/api/v1', '')
    if (p === '/admin/agent/runs') {
      const start = Number(u.searchParams.get('cursor') || 0)
      const size = Math.min(Number(u.searchParams.get('limit') || 100), pageMax)
      const items = digests.slice(start, start + size)
      const next = start + size < digests.length ? String(start + size) : ''
      return ok({ items, next_cursor: next })
    }
    const m = p.match(/^\/admin\/agent\/runs\/(.+)\/export$/)
    if (m) {
      m[1] = decodeURIComponent(m[1])
      if (onExport) {
        const r = onExport(m[1], u.searchParams.get('format'))
        if (r) return r
      }
      const format = u.searchParams.get('format')
      const md = `# 运行 ${m[1]}\n`
      const data = { run_id: m[1], steps: [] }
      if (format === 'json') return ok(data)
      if (format === 'both') return ok({ format: 'both', content: md, data })
      return ok({ format: 'md', content: md })
    }
    const rq = p.match(/^\/admin\/agent\/runs\/(.+)\/requests$/)
    if (rq) return ok({ items: requests[decodeURIComponent(rq[1])] ?? [] })
    if (p === '/admin/agent/stats') return ok({ groups: stats })
    return new Response('not found', { status: 404 })
  }
  return {
    fetch,
    calls,
    exportCalls: () => calls.filter((c) => c.path.endsWith('/export')),
    requestCalls: () => calls.filter((c) => c.path.endsWith('/requests')),
  }
}

// 运行 CLI 并收集输出。sleeps 记录退避等待。
async function run(argv, { cwd, backend, env = ENV, random } = {}) {
  const stdout = sink()
  const stderr = sink()
  const sleeps = []
  const code = await main(argv, {
    fetch: backend?.fetch, cwd, stdout, stderr, env, random,
    now: () => new Date('2026-10-21T12:00:00Z'),
    sleep: async (ms) => { sleeps.push(ms) },
  })
  return { code, out: stdout.text, err: stderr.text, sleeps }
}

const readIndex = (cwd) =>
  fs.readFileSync(path.join(cwd, '.traces', 'index.jsonl'), 'utf8').trim().split('\n').filter(Boolean).map((l) => JSON.parse(l))
const runFiles = (cwd) => fs.readdirSync(path.join(cwd, '.traces', 'runs')).sort()

// 可复现的伪随机数。
function seeded(seed = 1) {
  let s = seed
  return () => {
    s = (s * 1664525 + 1013904223) % 4294967296
    return s / 4294967296
  }
}

test('配置缺失：退出码 2，中文提示缺哪个变量', async () => {
  const cwd = tmpDir()
  const r = await run(['pull'], { cwd, env: {}, backend: fakeBackend() })
  assert.equal(r.code, 2)
  assert.match(r.err, /VC_API_BASE/)
  assert.match(r.err, /VC_TRACE_TOKEN/)

  const r2 = await run(['pull'], { cwd, env: { VC_API_BASE: 'https://x/api/v1' }, backend: fakeBackend() })
  assert.equal(r2.code, 2)
  assert.doesNotMatch(r2.err, /缺少配置：VC_API_BASE/)
  assert.match(r2.err, /VC_TRACE_TOKEN/)
})

test('从 .traces/.env 读取配置，末尾斜杠被去掉，带鉴权头', async () => {
  const cwd = tmpDir()
  fs.mkdirSync(path.join(cwd, '.traces'))
  fs.writeFileSync(path.join(cwd, '.traces', '.env'), '# 注释\nVC_API_BASE="https://example.test/api/v1/"\nexport VC_TRACE_TOKEN=vca_file\n')
  const backend = fakeBackend({ digests: [] })
  const r = await run(['pull'], { cwd, env: {}, backend })
  assert.equal(r.code, 0, r.err)
  assert.equal(backend.calls[0].path, '/api/v1/admin/agent/runs')
  assert.equal(backend.calls[0].headers.Authorization, 'Bearer vca_file')
})

test('401：提示令牌失效', async () => {
  const cwd = tmpDir()
  const backend = { fetch: async () => new Response(JSON.stringify({ code: 40101, msg: 'unauthorized' }), { status: 401 }), calls: [] }
  const r = await run(['pull'], { cwd, backend })
  assert.equal(r.code, 1)
  assert.match(r.err, /令牌失效，请在后台重新签发/)
})

test('code≠0 与 HTTP 5xx 都报错', async () => {
  const cwd = tmpDir()
  const bizErr = { fetch: async () => new Response(JSON.stringify({ code: 40001, msg: '参数错误' }), { status: 200 }) }
  const r = await run(['pull'], { cwd, backend: bizErr })
  assert.equal(r.code, 1)
  assert.match(r.err, /code=40001.*参数错误/)

  const http500 = { fetch: async () => new Response('boom', { status: 500 }) }
  const r2 = await run(['pull'], { cwd, backend: http500 })
  assert.equal(r2.code, 1)
  assert.match(r2.err, /HTTP 500/)
})

test('429：按 1s/2s/4s 退避重试后成功', async () => {
  const cwd = tmpDir()
  const real = fakeBackend({ digests: [digest(1)] })
  let hits = 0
  const backend = {
    fetch: async (url, init) => {
      if (new URL(url).pathname.endsWith('/admin/agent/runs') && hits++ < 3) return new Response('', { status: 429 })
      return real.fetch(url, init)
    },
  }
  const r = await run(['pull'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  assert.deepEqual(r.sleeps, [1000, 2000, 4000])
  assert.match(r.out, /新写 1 条/)
})

test('429：重试 3 次仍失败则停止，已写文件保留', async () => {
  const cwd = tmpDir()
  const backend = fakeBackend({
    digests: [digest(1), digest(2)],
    onExport: (id) => (id === 'rid2' ? new Response('', { status: 429 }) : null),
  })
  const r = await run(['pull'], { cwd, backend })
  assert.equal(r.code, 1)
  assert.match(r.err, /429/)
  assert.deepEqual(r.sleeps, [1000, 2000, 4000])
  // rid1 已写；rid2 没写；index 只有 rid1
  assert.deepEqual(runFiles(cwd).map((f) => f.replace(/^\d{4}-\d{2}-\d{2}_/, '')), ['rrid1.json', 'rrid1.md'])
  assert.deepEqual(readIndex(cwd).map((d) => d.run_id), ['rid1'])
})

test('分页：按游标拉完，并尊重 --limit', async () => {
  const cwd = tmpDir()
  const digests = Array.from({ length: 5 }, (_, i) => digest(i + 1))
  const backend = fakeBackend({ digests, pageMax: 2 })
  const r = await run(['pull'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  assert.match(r.out, /命中 5 条，抽样 5 条，新写 5 条，跳过 0 条/)
  assert.equal(backend.calls.filter((c) => c.path.endsWith('/admin/agent/runs')).length, 3)

  const cwd2 = tmpDir()
  const backend2 = fakeBackend({ digests, pageMax: 2 })
  const r2 = await run(['pull', '--limit', '3'], { cwd: cwd2, backend: backend2 })
  assert.match(r2.out, /命中 3 条，抽样 3 条，新写 3 条，跳过 0 条/)
  assert.equal(readIndex(cwd2).length, 3)
})

test('筛选条件与时间参数被透传到摘要接口', async () => {
  const cwd = tmpDir()
  const backend = fakeBackend()
  await run(['pull', '--since', '24h', '--feedback', 'down', '--undone', '--status', 'failed,canceled', '--harness', 'h1', '--prompt-version', '9', '--user', 'u7'], { cwd, backend })
  const q = backend.calls[0].query
  assert.equal(q.since, '2026-10-20T12:00:00.000Z')
  assert.equal(q.feedback, 'down')
  assert.equal(q.undone, 'true')
  assert.equal(q.status, 'failed,canceled')
  assert.equal(q.harness_key, 'h1')
  assert.equal(q.prompt_version, '9')
  assert.equal(q.user_id, 'u7')
})

test('增量：updated_at 没变的跳过，变了的重写（且不留重复文件）', async () => {
  const cwd = tmpDir()
  const digests = [digest(1), digest(2)]
  const backend = fakeBackend({ digests })
  const r1 = await run(['pull'], { cwd, backend })
  assert.match(r1.out, /新写 2 条，跳过 0 条/)
  const before = backend.exportCalls().length

  const r2 = await run(['pull'], { cwd, backend })
  assert.match(r2.out, /新写 0 条，跳过 2 条/)
  assert.equal(backend.exportCalls().length, before, '跳过时不应再请求导出')

  digests[1] = digest(2, { updated_at: '2026-10-21T01:00:00Z', ended_at: '2026-10-21T01:00:00Z' })
  const r3 = await run(['pull'], { cwd, backend })
  assert.match(r3.out, /新写 1 条，跳过 1 条/)
  assert.equal(runFiles(cwd).filter((f) => f.endsWith('_rrid2.md')).length, 1)
  assert.equal(readIndex(cwd).length, 2)
  assert.equal(readIndex(cwd).find((d) => d.run_id === 'rid2').updated_at, '2026-10-21T01:00:00Z')
})

test('落盘：文件名格式、run_id 去掉非法字符、.md 原样保存、.json 为结构化数据', async () => {
  const cwd = tmpDir()
  const backend = fakeBackend({ digests: [digest(1, { run_id: 'a-b/c_9==' })] })
  const r = await run(['pull'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  const files = runFiles(cwd)
  assert.equal(files.length, 2)
  assert.match(files[0], /^\d{4}-\d{2}-\d{2}_rabc_9\.json$/)
  assert.match(files[1], /^\d{4}-\d{2}-\d{2}_rabc_9\.md$/)
  assert.equal(fs.readFileSync(path.join(cwd, '.traces', 'runs', files[1]), 'utf8'), '# 运行 a-b/c_9==\n')
  assert.equal(JSON.parse(fs.readFileSync(path.join(cwd, '.traces', 'runs', files[0]), 'utf8')).run_id, 'a-b/c_9==')
})

test('分层抽样：信号样本优先，总数不超过 N', async () => {
  const cwd = tmpDir()
  // 30 条：前 10 条是信号（点踩 / 撤销 / 拒绝审批 / 失败各种），其余正常。
  const digests = Array.from({ length: 30 }, (_, i) => {
    if (i < 3) return digest(i + 1, { feedback: -1 })
    if (i < 5) return digest(i + 1, { undone: true })
    if (i < 7) return digest(i + 1, { approvals_rejected: 1 })
    if (i < 10) return digest(i + 1, { status: 'failed' })
    return digest(i + 1)
  })
  const backend = fakeBackend({ digests })
  const r = await run(['pull', '--sample', '9'], { cwd, backend, random: seeded(7) })
  assert.equal(r.code, 0, r.err)
  assert.match(r.out, /命中 30 条，抽样 9 条，新写 9 条/)
  const picked = readIndex(cwd)
  assert.equal(picked.length, 9)
  const signalIds = new Set(digests.slice(0, 10).map((d) => d.run_id))
  const signalCount = picked.filter((d) => signalIds.has(d.run_id)).length
  assert.ok(signalCount >= 6, `信号样本至少取 ceil(9*2/3)=6 条，实际 ${signalCount}`)
})

test('分层抽样：信号少于配额时全部入选，其余补满；N 不小于命中数时不抽', async () => {
  const cwd = tmpDir()
  const digests = Array.from({ length: 20 }, (_, i) => (i < 2 ? digest(i + 1, { feedback: -1 }) : digest(i + 1)))
  const backend = fakeBackend({ digests })
  const r = await run(['pull', '--sample', '6'], { cwd, backend, random: seeded(3) })
  assert.match(r.out, /抽样 6 条/)
  const ids = readIndex(cwd).map((d) => d.run_id)
  assert.equal(ids.length, 6)
  assert.ok(ids.includes('rid1') && ids.includes('rid2'))

  const cwd2 = tmpDir()
  const r2 = await run(['pull', '--sample', '50'], { cwd: cwd2, backend: fakeBackend({ digests }) })
  assert.match(r2.out, /命中 20 条，抽样 20 条/)
})

test('.gitignore：缺 .traces/ 时追加，已有则不重复', async () => {
  const cwd = tmpDir()
  fs.writeFileSync(path.join(cwd, '.gitignore'), 'node_modules/')
  await run(['pull'], { cwd, backend: fakeBackend() })
  await run(['pull'], { cwd, backend: fakeBackend() })
  const text = fs.readFileSync(path.join(cwd, '.gitignore'), 'utf8')
  assert.equal(text.split('\n').filter((l) => l.trim() === '.traces/').length, 1)
  assert.match(text, /^node_modules\/\n/)

  const cwd2 = tmpDir()
  fs.writeFileSync(path.join(cwd2, '.gitignore'), '/.traces\n')
  await run(['pull'], { cwd: cwd2, backend: fakeBackend() })
  assert.equal(fs.readFileSync(path.join(cwd2, '.gitignore'), 'utf8'), '/.traces\n')
})

test('show：在终端打印 Markdown', async () => {
  const cwd = tmpDir()
  const backend = fakeBackend()
  const r = await run(['show', 'rid9'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  assert.equal(r.out, '# 运行 rid9\n')
  assert.equal(backend.calls[0].query.format, 'md')
  assert.equal((await run(['show'], { cwd, backend })).code, 2)
})

function statGroup(key, n, extra = {}) {
  return { key, n, feedback_rate: 0.2, down_rate: 0.1, undo_rate: 0.05, fail_end_rate: 0.1, median_duration_ms: 12000, median_steps: 8, avg_credits: 3.5, ...extra }
}

function writeVerdicts(cwd, rows) {
  fs.mkdirSync(path.join(cwd, '.traces'), { recursive: true })
  fs.writeFileSync(path.join(cwd, '.traces', 'verdicts.jsonl'), rows.map((r) => JSON.stringify(r)).join('\n') + '\n')
}

test('compare：任一组 n<30 时结论为“样本不足”，不给改善/变差', async () => {
  const cwd = tmpDir()
  const backend = fakeBackend({
    stats: [statGroup('h_old', 100, { fail_end_rate: 0.3 }), statGroup('h_new', 12, { fail_end_rate: 0.01, down_rate: 0 })],
  })
  const r = await run(['compare', '--by', 'harness_key', '--since', '30d'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  assert.equal(r.out.split('样本不足').length - 1, 2, '两行结论都应是样本不足')
  assert.doesNotMatch(r.out, /\| (改善|变差) \|/)
  assert.equal(backend.calls[0].query.group_by, 'harness_key')
  assert.equal(backend.calls[0].query.since, '2026-09-21T12:00:00.000Z')
})

test('compare：样本都够时以最大组为基线给出结论，并合并 verdicts 算 AI 判定失败率', async () => {
  const cwd = tmpDir()
  // index 用来把 run_id 映射到 prompt_version；verdicts 里 harness_key 直接给。
  const backend = fakeBackend({
    stats: [
      statGroup('h_old', 100, { fail_end_rate: 0.3, down_rate: 0.2 }),
      statGroup('h_new', 40, { fail_end_rate: 0.1, down_rate: 0.1 }),
      statGroup('h_same', 35, { fail_end_rate: 0.3, down_rate: 0.2 }),
    ],
  })
  writeVerdicts(cwd, [
    { run_id: 'a', harness_key: 'h_new', outcome: 'failed' },
    { run_id: 'b', harness_key: 'h_new', outcome: 'partial' },
    { run_id: 'c', harness_key: 'h_new', outcome: 'achieved' },
    { run_id: 'd', harness_key: 'h_new', outcome: 'unclear' }, // 不计入已判定
    { run_id: 'e', harness_key: 'h_old', outcome: 'achieved' },
    { run_id: 'e', harness_key: 'h_old', outcome: 'failed' }, // 同 run_id 以最后一条为准
  ])
  const r = await run(['compare', '--by', 'harness_key'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  const row = (key) => r.out.split('\n').find((l) => l.startsWith(`| ${key} |`))
  assert.match(row('h_new'), /\| 3 \| 66\.7% \| 改善 \|$/) // 已判定 3，(failed+partial)/3
  assert.match(row('h_old'), /\| 1 \| 100\.0% \| 基线 \|$/)
  assert.match(row('h_same'), /\| 0 \| - \| 无明显差异 \|$/)
  assert.doesNotMatch(r.out, /样本不足/)
})

test('compare：按 prompt_version / model_key 分组时通过本地 index 映射 run_id', async () => {
  const cwd = tmpDir()
  const backend = fakeBackend({ stats: [statGroup('9', 50), statGroup('10', 50)] })
  fs.mkdirSync(path.join(cwd, '.traces'))
  fs.writeFileSync(
    path.join(cwd, '.traces', 'index.jsonl'),
    [digest(1, { prompt_version: 9 }), digest(2, { prompt_version: 10 }), digest(3, { prompt_version: 10 })].map((d) => JSON.stringify(d)).join('\n') + '\n',
  )
  writeVerdicts(cwd, [
    { run_id: 'rid1', harness_key: 'h1', outcome: 'achieved' },
    { run_id: 'rid2', harness_key: 'h1', outcome: 'failed' },
    { run_id: 'rid3', harness_key: 'h1', outcome: 'achieved' },
    { run_id: 'ghost', harness_key: 'h1', outcome: 'failed' }, // index 里没有，无法归组
  ])
  const r = await run(['compare', '--by', 'prompt_version'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  const row = (key) => r.out.split('\n').find((l) => l.startsWith(`| ${key} |`))
  assert.match(row('9'), /\| 1 \| 0\.0% \|/)
  assert.match(row('10'), /\| 2 \| 50\.0% \|/)
  assert.match(r.out, /1 条判定无法归到分组/)
})

test('compare：--by 非法时退出码 2', async () => {
  const r = await run(['compare', '--by', 'color'], { cwd: tmpDir(), backend: fakeBackend() })
  assert.equal(r.code, 2)
})

test('未知命令与未知选项：退出码 2', async () => {
  const cwd = tmpDir()
  assert.equal((await run(['bogus'], { cwd, backend: fakeBackend() })).code, 2)
  assert.equal((await run(['pull', '--nope'], { cwd, backend: fakeBackend() })).code, 2)
  assert.equal((await run(['pull', '--limit', 'abc'], { cwd, backend: fakeBackend() })).code, 2)
})

test('pull 使用 format=both：每条运行只请求 1 次导出，.md 来自 content、.json 来自 data', async () => {
  const cwd = tmpDir()
  const backend = fakeBackend({
    digests: [digest(1), digest(2)],
    onExport: (id, format) =>
      format === 'both' ? ok({ format: 'both', content: `MD-${id}\n`, data: { run_id: id, marker: `JSON-${id}` } }) : null,
  })
  const r = await run(['pull'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  const exports = backend.exportCalls()
  assert.equal(exports.length, 2, '每条运行只发 1 次 export 请求')
  assert.ok(exports.every((c) => c.query.format === 'both'))
  const dir = path.join(cwd, '.traces', 'runs')
  for (const id of ['rid1', 'rid2']) {
    const md = runFiles(cwd).find((f) => f.endsWith(`_r${id}.md`))
    const js = runFiles(cwd).find((f) => f.endsWith(`_r${id}.json`))
    assert.equal(fs.readFileSync(path.join(dir, md), 'utf8'), `MD-${id}\n`)
    assert.equal(JSON.parse(fs.readFileSync(path.join(dir, js), 'utf8')).marker, `JSON-${id}`)
  }
})

test('429 带 Retry-After 时优先用它，没有则用默认退避', async () => {
  const cwd = tmpDir()
  const real = fakeBackend({ digests: [] })
  let hits = 0
  const backend = {
    fetch: async (url, init) => {
      if (hits++ === 0) return new Response('', { status: 429, headers: { 'Retry-After': '7' } })
      if (hits === 2) return new Response('', { status: 429 })
      return real.fetch(url, init)
    },
  }
  const r = await run(['pull'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  assert.deepEqual(r.sleeps, [7000, 2000])
})

// ---------------------------------------------------------------------------
// pull --with-requests：把每次模型调用实际发给上游的完整请求体也拉下来
// ---------------------------------------------------------------------------

const requestItems = (id) => [{ id: `call-${id}`, messages: 3, tools: 2, bytes: 100, kept: true, params: { model: 'm' }, body: { model: 'm', marker: `BODY-${id}` } }]
const requestsFileOf = (cwd, id) => runFiles(cwd).find((f) => f.endsWith(`_r${id}.requests.json`))

test('pull 不带 --with-requests：不请求请求体接口，也不写 .requests.json', async () => {
  const cwd = tmpDir()
  const backend = fakeBackend({ digests: [digest(1)], requests: { rid1: requestItems('rid1') } })
  const r = await run(['pull'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  assert.equal(backend.requestCalls().length, 0)
  assert.equal(requestsFileOf(cwd, 'rid1'), undefined)
})

test('pull --with-requests：每条运行取一次请求体，写成 _r<id>.requests.json，不影响 .md 和 .json', async () => {
  const cwd = tmpDir()
  const backend = fakeBackend({ digests: [digest(1), digest(2)], requests: { rid1: requestItems('rid1'), rid2: requestItems('rid2') } })
  const r = await run(['pull', '--with-requests'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  assert.equal(backend.requestCalls().length, 2, '每条运行只发 1 次 requests 请求')
  assert.match(r.out, /请求体 2 条/)
  for (const id of ['rid1', 'rid2']) {
    const f = requestsFileOf(cwd, id)
    assert.ok(f, `${id} 应有 .requests.json`)
    const data = JSON.parse(fs.readFileSync(path.join(cwd, '.traces', 'runs', f), 'utf8'))
    assert.equal(data.items[0].body.marker, `BODY-${id}`)
    assert.ok(runFiles(cwd).some((x) => x.endsWith(`_r${id}.md`)) && runFiles(cwd).some((x) => x.endsWith(`_r${id}.json`)))
  }
})

test('请求体已过保留期（后端返回空）：不写空文件', async () => {
  const cwd = tmpDir()
  const backend = fakeBackend({ digests: [digest(1)], requests: {} })
  const r = await run(['pull', '--with-requests'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  assert.equal(requestsFileOf(cwd, 'rid1'), undefined)
  assert.match(r.out, /请求体 0 条/)
})

test('运行没变、本地已有请求体：不再请求任何接口；没有请求体文件时只补取请求体，不重复导出', async () => {
  const cwd = tmpDir()
  const first = fakeBackend({ digests: [digest(1)] })
  await run(['pull'], { cwd, backend: first }) // 先拉过一次，没带 --with-requests
  const backend = fakeBackend({ digests: [digest(1)], requests: { rid1: requestItems('rid1') } })
  const r = await run(['pull', '--with-requests'], { cwd, backend })
  assert.equal(r.code, 0, r.err)
  assert.equal(backend.exportCalls().length, 0, '运行没变，不该重复导出')
  assert.equal(backend.requestCalls().length, 1, '只补取请求体')
  assert.ok(requestsFileOf(cwd, 'rid1'))

  const again = fakeBackend({ digests: [digest(1)], requests: { rid1: requestItems('rid1') } })
  await run(['pull', '--with-requests'], { cwd, backend: again })
  assert.equal(again.exportCalls().length + again.requestCalls().length, 0, '都已在本地：什么也不请求')
})

test('运行更新后重拉（不带 --with-requests）：本地已有的请求体保留，不因日期变化被删掉或丢下', async () => {
  const cwd = tmpDir()
  await run(['pull', '--with-requests'], { cwd, backend: fakeBackend({ digests: [digest(1)], requests: { rid1: requestItems('rid1') } }) })
  const before = requestsFileOf(cwd, 'rid1')
  assert.ok(before)
  // 运行有了新进展：更新时间和结束日期都变了
  const changed = digest(1, { updated_at: '2026-10-22T06:00:01Z', ended_at: '2026-10-22T06:00:00Z' })
  await run(['pull'], { cwd, backend: fakeBackend({ digests: [changed] }) })
  const after = requestsFileOf(cwd, 'rid1')
  assert.ok(after, '请求体服务端只留 7 天，本地的不能丢')
  assert.match(after, /^2026-10-22_/, '文件名跟着新日期走，不留两份')
  assert.equal(runFiles(cwd).filter((f) => f.endsWith('_rrid1.requests.json')).length, 1)
})
