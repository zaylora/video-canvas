/**
 * 迁移前参数是 typebox 定义，它在校验前会把单个值包成数组（模型常把 nodeIds 写成一个字符串）；
 * Go 下发的是普通 JSON Schema，pi 不会这样做，所以在校验前补上，模型看到的行为不变。
 * 只处理标量：对象放在要求数组的地方不动，让校验照常报错。不修改入参。
 */
export function wrapScalarArrays(schema, value) {
  if (!schema || typeof schema !== "object") return value;
  if (schema.type === "array") {
    if (value === undefined || value === null) return value;
    if (!Array.isArray(value) && typeof value === "object") return value;
    const arr = Array.isArray(value) ? value : [value];
    return schema.items ? arr.map((v) => wrapScalarArrays(schema.items, v)) : arr;
  }
  if (schema.type === "object" && schema.properties && value && typeof value === "object" && !Array.isArray(value)) {
    const out = { ...value };
    for (const [k, s] of Object.entries(schema.properties)) if (k in out) out[k] = wrapScalarArrays(s, out[k]);
    return out;
  }
  return value;
}

/**
 * 生成 pi 的工具对象。工具的名字、说明和参数 schema 都由 Go 下发（启动参数的 tools），这里不内置任何业务工具；
 * execute 只是回调 Go（桥）。工具层的失败（参数不对、校验不过）是 is_error 的普通结果，
 * 让模型看到并自己改正；回调本身失败（网络、令牌无效）才抛异常，pi 会把它变成给模型的错误结果。
 *
 * @param {{ tool: Function }} bridge
 * @param {Array<{ name: string, label?: string, description: string, parameters: object, executor?: string }>} [decls] Go 下发的声明，顺序即给模型的顺序
 */
export function buildTools(bridge, decls = [], onTerminate = () => {}) {
  return decls.map((d) => {
    if ((d.executor ?? "server") !== "server") throw new Error(`不支持的工具执行方：${d.name}（${d.executor}）`);
    return {
      name: d.name,
      label: d.label ?? d.name,
      description: d.description,
      parameters: d.parameters,
      prepareArguments: (args) => wrapScalarArrays(d.parameters, args),
      execute: async (toolCallId, params, signal) => {
        const r = await bridge.tool(toolCallId, d.name, params, signal);
        if (r.terminate) onTerminate();
        // 对象存储的图片只有地址：先放进 data，发请求前由 restoreImageUrls 还原
        const images = (r.images ?? []).map((i) => ({ type: "image", data: i.url ?? i.data, mimeType: i.mime_type }));
        return { content: [{ type: "text", text: r.content }, ...images], details: {}, isError: !!r.is_error, terminate: !!r.terminate };
      },
    };
  });
}
