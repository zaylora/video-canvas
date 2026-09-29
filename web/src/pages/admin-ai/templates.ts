/** 新建配置时编辑器里的起始内容，字段含义见「平台协议配置化设计」5.3 / 5.4 */
export const PROVIDER_TEMPLATE = {
  dsl: 1,
  key: "",
  name: "",
  base_url: "https://",
  allowed_hosts: [],
  auth: { type: "bearer", secret: "" },
  rate_limit: { rps: 5, max_concurrency: 20 },
  poll: { first_delay: "10s", interval: "5s", max_interval: "15s", jitter: 0.2 },
  operations: {
    submit: {
      method: "POST",
      path: "/",
      encoding: { type: "json" },
      body: {},
      success: "status == 200",
      extract: { provider_task_id: "resp.taskId" },
    },
    query: {
      method: "POST",
      path: "/",
      encoding: { type: "json" },
      body: { taskId: "${ task.provider_task_id }" },
      success: "status == 200",
      extract: { status: "resp.status", outputs: "[]" },
    },
    cancel: null,
  },
  status_map: { _default: "running" },
  error_rules: [{ when: "true", class: "terminal" }],
};

export const MODEL_TEMPLATE = {
  key: "",
  kind: "video",
  provider: "runninghub",
  label: "",
  hint: "",
  credits: 10,
  deadline: "30m",
  enabled: false,
  sort: 100,
  params: {},
  input_schema: {
    prompt: { type: "text", label: "提示词", required: true, max_length: 2000, port: "text" },
  },
  mapping: {},
  output: { media: "video" },
};
