/**
 * 前端用的 REST 接口（Go 鉴权后反向代理过来），用 Fastify 提供。路径、方法、请求体、响应信封都和 Go 现在一致：
 * 成功 {code:0, msg:"success", data}（没有数据时没有 data 字段），发起运行是 202；
 * 失败 {code, msg}，HTTP 状态取业务错误自带的。
 *
 * 认证：服务令牌（请求头 X-Agent-Service-Token）+ 用户（请求头 X-Agent-User，十进制 id，由 Go 在校验过用户的登录态后带上）。
 *
 * 请求参数的校验不用 Fastify 自带的 schema：错误提示要和 Go 的中文措辞一致（前端原样显示给用户），见 validate.ts。
 */
import { timingSafeEqual } from "node:crypto";
import Fastify, { type FastifyInstance, type FastifyReply, type FastifyRequest } from "fastify";
import { AgentError, Err } from "../engine/errors.ts";
import type { IdCodec } from "../ids.ts";
import type { AgentApiService } from "./service.ts";
import { ValidationError, queryInt, validateBody, type FieldSpec } from "./validate.ts";

export const USER_HEADER = "x-agent-user";
const SERVICE_TOKEN_HEADER = "x-agent-service-token";
const DEFAULT_MAX_BODY = 1 << 20;
const MODES = ["all", "script", "storyboard", "prompt"] as const;
const DEFAULT_OBSERVE_PAGE = 200;
const BUDGET = { min: 0, max: 100000 };

const CREATE_SESSION: FieldSpec[] = [
  { key: "title", label: "标题", kind: "string", max: 40 },
  { key: "mode", label: "任务模式", kind: "string", oneof: MODES },
  { key: "model_key", label: "模型", kind: "string", max: 128 },
];
const RENAME: FieldSpec[] = [{ key: "title", label: "标题", kind: "string", required: true, max: 40 }];
const START_RUN: FieldSpec[] = [
  { key: "message", label: "消息", kind: "string", required: true, max: 20000 },
  { key: "mode", label: "任务模式", kind: "string", oneof: MODES },
  { key: "selection", label: "选中节点", kind: "strings", max: 200, itemMax: 64 },
  { key: "viewport", label: "视口", kind: "any" },
  { key: "budget_credits", label: "本轮预算", kind: "int", nullable: true, ...BUDGET },
  { key: "agent_model_key", label: "Agent 模型", kind: "string", max: 128 },
];
const INTERJECT: FieldSpec[] = [{ key: "message", label: "消息", kind: "string", required: true, max: 20000 }];
const RESUME: FieldSpec[] = [{ key: "add_budget", label: "追加预算", kind: "int", ...BUDGET }];
const DECIDE: FieldSpec[] = [
  { key: "decision", label: "决定", kind: "string", required: true, oneof: ["approve", "reject"] },
  {
    key: "items", label: "条目", kind: "items", max: 50,
    item: [{ key: "index", label: "序号", kind: "int", min: 0 }, { key: "approve", label: "approve", kind: "bool" }, { key: "count", label: "数量", kind: "int", min: 0, max: 4 }],
  },
  { key: "answer", label: "回答", kind: "string", max: 2000 },
  { key: "add_budget", label: "追加预算", kind: "int", ...BUDGET },
];

const FEEDBACK: FieldSpec[] = [
  { key: "rating", label: "评价", kind: "int", required: true, oneof: ["1", "-1"] },
  { key: "reasons", label: "原因", kind: "strings", max: 5, itemMax: 32 },
  { key: "comment", label: "说明", kind: "string", max: 2000 },
];

export interface ApiOptions {
  service: AgentApiService;
  codec: IdCodec;
  serviceToken: string;
  /** 请求体上限（字节），默认 1MB */
  maxBody?: number;
  log?: (msg: string, extra?: Record<string, unknown>) => void;
}

type Req = FastifyRequest<{ Params: Record<string, string> }>;

/** 业务错误和校验错误变成 Go 的错误信封；Fastify 自己的请求错误（体太大、内容类型不对）也翻成同样的格式 */
function errorBody(e: unknown, log: NonNullable<ApiOptions["log"]>): { status: number; body: { code: number; msg: string } } {
  if (e instanceof AgentError) return { status: e.status, body: { code: e.code, msg: e.message } };
  if (e instanceof ValidationError) return { status: 400, body: { code: 10001, msg: e.message } };
  const fe = e as { code?: string; statusCode?: number };
  if (fe?.code === "FST_ERR_CTP_BODY_TOO_LARGE") return { status: 413, body: { code: 10001, msg: "请求体过大" } };
  if (typeof fe?.statusCode === "number" && fe.statusCode >= 400 && fe.statusCode < 500) return { status: 400, body: { code: 10001, msg: "请求参数格式错误" } };
  log("REST 接口出错", { error: String((e as Error)?.stack ?? e) });
  return { status: 500, body: { code: 10000, msg: "服务内部错误" } };
}

/**
 * 创建提供前端 REST 接口的 Fastify 实例（还没监听）。调用方 listen / close；
 * 服务入口可以在同一个实例上再挂别的路由（健康检查等）。
 */
export function createApiApp(o: ApiOptions): FastifyInstance {
  const { service, codec } = o;
  const log = o.log ?? ((msg, extra) => void process.stderr.write(JSON.stringify({ msg, ...extra }) + "\n"));
  const root = Fastify({ bodyLimit: o.maxBody ?? DEFAULT_MAX_BODY, logger: false, disableRequestLogging: true });
  const want = Buffer.from(o.serviceToken);

  root.setErrorHandler((e, _req, reply) => {
    const { status, body } = errorBody(e, log);
    return reply.code(status).send(body);
  });
  root.setNotFoundHandler((_req, reply) => reply.code(404).send({ code: 10002, msg: "资源不存在" }));
  // 继续、取消、撤销这类不需要请求体的，前端可能什么都不发（甚至带着 JSON 的内容类型）：空体当 {}；不是合法 JSON 是 400
  root.addContentTypeParser("application/json", { parseAs: "string" }, (_req, body, done) => {
    const raw = String(body).trim();
    if (!raw) return done(null, {});
    try {
      done(null, JSON.parse(raw));
    } catch {
      done(new ValidationError("请求参数格式错误"));
    }
  });
  const tokenOk = (req: FastifyRequest): boolean => {
    const got = Buffer.from(String(req.headers[SERVICE_TOKEN_HEADER] ?? ""));
    return got.length === want.length && timingSafeEqual(got, want);
  };
  // 观测接口：Go 的后台只读，只认服务令牌，没有用户（和用户的接口分开，用户头也不会让它多出权限）
  root.register(async (app) => {
    app.addHook("onRequest", async (req, reply) => {
      if (!tokenOk(req)) return reply.code(401).send({ code: 10003, msg: "未登录或登录已过期" });
    });
    const ok = (reply: FastifyReply, data: unknown) => reply.header("cache-control", "no-store").send({ code: 0, msg: "success", data });
    app.get("/observe/runs", async (req, reply) => {
      const q = new URL(req.url, "http://x").searchParams;
      const after = queryInt(q, "updated_after", "updated_after", 0);
      const limit = queryInt(q, "limit", "limit", 0, 500) || DEFAULT_OBSERVE_PAGE;
      return ok(reply, await service.observeRuns(after, limit));
    });
    app.get("/observe/runs/:rid", async (req: Req, reply) => {
      const rid = req.params.rid;
      if (!/^[1-9][0-9]{0,19}$/.test(rid)) throw Err.invalidParams("rid 格式错误");
      return ok(reply, await service.observeFacts(rid));
    });
  });
  // 鉴权钩子和路由放在子插件里：同一个实例上还能再挂不需要令牌的路由（健康检查）
  root.register(async (app) => {
    app.addHook("onRequest", async (req, reply) => {
      const user = String(req.headers[USER_HEADER] ?? "");
      if (!tokenOk(req) || !/^[1-9][0-9]{0,19}$/.test(user)) {
        return reply.code(401).send({ code: 10003, msg: "未登录或登录已过期" });
      }
    });

    /** 路径里的编码 id 还原成十进制；格式不对是 10001「xxx 格式错误」 */
    const id = (req: Req, name: string): string => {
      const dec = codec.decodeDecimal(req.params[name]);
      if (dec === undefined) throw Err.invalidParams(`${name} 格式错误`);
      return dec;
    };
    const user = (req: FastifyRequest) => String(req.headers[USER_HEADER]);
    const body = (req: FastifyRequest) => req.body ?? {};
    const query = (req: FastifyRequest) => new URL(req.url, "http://x").searchParams;
    const ok = (reply: FastifyReply, data?: unknown, status = 200) =>
      reply.code(status).header("cache-control", "no-store").send({ code: 0, msg: "success", ...(data !== undefined && { data }) });

    app.get("/agent/models", async (_req, reply) => ok(reply, await service.models()));
    app.get("/canvas/:id/agent/sessions", async (req: Req, reply) => ok(reply, await service.listSessions(user(req), id(req, "id"))));
    app.post("/canvas/:id/agent/sessions", async (req: Req, reply) => {
      const canvas = id(req, "id");
      return ok(reply, await service.createSession(user(req), canvas, validateBody(body(req), CREATE_SESSION)));
    });
    app.patch("/agent/sessions/:sid", async (req: Req, reply) => {
      const sid = id(req, "sid");
      return ok(reply, await service.renameSession(user(req), sid, validateBody(body(req), RENAME).title));
    });
    app.delete("/agent/sessions/:sid", async (req: Req, reply) => {
      await service.deleteSession(user(req), id(req, "sid"));
      return ok(reply);
    });
    app.get("/agent/sessions/:sid/events", async (req: Req, reply) => {
      const sid = id(req, "sid");
      const q = query(req);
      return ok(reply, await service.events(user(req), sid, queryInt(q, "after", "after", 0), queryInt(q, "limit", "limit", 0, 200)));
    });
    app.get("/agent/sessions/:sid/history", async (req: Req, reply) => {
      const sid = id(req, "sid");
      const q = query(req);
      return ok(reply, await service.history(user(req), sid, queryInt(q, "before", "before", 0), queryInt(q, "limit", "limit", 0, 200)));
    });
    app.post("/agent/sessions/:sid/runs", async (req: Req, reply) => {
      const sid = id(req, "sid");
      return ok(reply, await service.startRun(user(req), sid, validateBody(body(req), START_RUN) as never), 202);
    });
    app.post("/agent/runs/:rid/interject", async (req: Req, reply) => {
      const rid = id(req, "rid");
      await service.interject(user(req), rid, validateBody(body(req), INTERJECT).message);
      return ok(reply);
    });
    app.post("/agent/runs/:rid/cancel", async (req: Req, reply) => ok(reply, await service.cancel(user(req), id(req, "rid"))));
    app.post("/agent/runs/:rid/resume", async (req: Req, reply) => {
      const rid = id(req, "rid");
      return ok(reply, await service.resume(user(req), rid, validateBody(body(req), RESUME).add_budget));
    });
    app.post("/agent/runs/:rid/undo", async (req: Req, reply) => ok(reply, await service.undo(user(req), id(req, "rid"))));
    app.put("/agent/runs/:rid/feedback", async (req: Req, reply) => {
      const rid = id(req, "rid");
      return ok(reply, await service.setFeedback(user(req), rid, validateBody(body(req), FEEDBACK) as never));
    });
    app.delete("/agent/runs/:rid/feedback", async (req: Req, reply) => {
      await service.clearFeedback(user(req), id(req, "rid"));
      return ok(reply);
    });
    app.post("/agent/approvals/:aid/decision", async (req: Req, reply) => {
      const aid = id(req, "aid");
      return ok(reply, await service.decide(user(req), aid, validateBody(body(req), DECIDE) as never));
    });
  });

  return root;
}
