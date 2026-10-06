/**
 * 桥客户端：Node 进程和 Go 的全部通信都在这里。进程里没有任何渠道密钥，只有一个一次性的桥令牌。
 * 响应约定：Go 的标准格式 {code, msg, data}，code 不为 0 或 HTTP 非 2xx 都视为失败。
 */
export class BridgeError extends Error {
  constructor(message, status, code) {
    super(message);
    this.name = "BridgeError";
    this.status = status;
    this.code = code;
  }
}

/**
 * @param {{ baseUrl: string, token: string, fetchImpl?: typeof fetch }} opts
 */
export function createBridge({ baseUrl, token, fetchImpl = fetch }) {
  const base = baseUrl.replace(/\/+$/, "") + "/internal/agent/bridge";

  async function post(path, body, signal) {
    const res = await fetchImpl(base + path, {
      method: "POST",
      headers: { "content-type": "application/json", authorization: "Bearer " + token },
      body: JSON.stringify(body),
      signal,
    });
    const json = await res.json().catch(() => null);
    if (!res.ok || !json || json.code !== 0) {
      throw new BridgeError(json?.msg || `桥返回 HTTP ${res.status}`, res.status, json?.code);
    }
    return json.data;
  }

  return {
    /** 执行一次工具调用，返回 {content, is_error, terminate} */
    tool: (toolCallId, name, args, signal) => post("/tool", { tool_call_id: toolCallId, name, args }, signal),
    /** 回合结束时交回对话历史 */
    state: (messages) => post("/state", { messages }),
    /** 报告片段结束：done 跑完了；paused 因工具要求停下（等审批、等回答）；error 出错 */
    finish: (status, message = "") => post("/finish", { status, message }),
  };
}
