import { spawn } from "node:child_process";
import { readFileSync } from "node:fs";
import { readFile, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { setTimeout as delay } from "node:timers/promises";

const fixture = JSON.parse(readFileSync(0, "utf8"));
const profile = join(fixture.dir, "chrome-profile");
const chrome = spawn(fixture.chrome, [
  "--headless=new", "--disable-background-timer-throttling", "--disable-renderer-backgrounding",
  "--disable-backgrounding-occluded-windows", "--no-first-run", "--window-size=2560,1280",
  `--user-data-dir=${profile}`, "--remote-debugging-port=0", "about:blank",
], { stdio: "ignore" });

async function until(label, operation, timeout = 15_000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = await operation();
    if (value) return value;
    await delay(100);
  }
  throw new Error(`等待超时 ${label}`);
}

const outstanding = new Map();
const submissions = new Map();
const droppedResponses = new Map();
const requests = new Map();
const requestDurations = new Map();
const requestBytes = new Map();
const revisionSignals = new Map();
let revisionBytes = 0;
let nextCommand = 0;
let socket;

function command(method, params = {}, sessionId) {
  const id = ++nextCommand;
  return new Promise((resolve, reject) => {
    const timer = setTimeout(() => {
      outstanding.delete(id);
      reject(new Error(`Chrome 指令超时 ${method}`));
    }, 15_000);
    outstanding.set(id, (reply) => {
      clearTimeout(timer);
      if (reply.error) reject(new Error(`${method}: ${reply.error.message}`));
      else resolve(reply.result);
    });
    socket.send(JSON.stringify({ id, method, params, sessionId }));
  });
}

async function connectBrowser() {
  const endpoint = await until("Chrome 调试端口", async () => {
    const contents = await readFile(join(profile, "DevToolsActivePort"), "utf8").catch(() => "");
    const [port, path] = contents.trim().split("\n");
    return path && `ws://127.0.0.1:${port}${path}`;
  });
  socket = new WebSocket(endpoint);
  await new Promise((resolve, reject) => {
    socket.addEventListener("open", resolve, { once: true });
    socket.addEventListener("error", reject, { once: true });
  });
  socket.addEventListener("message", ({ data }) => {
    const reply = JSON.parse(data);
    if (reply.method === "Network.requestWillBeSent") {
      recordSubmission(reply.params);
      trackCanvasRequest(reply.sessionId, reply.params);
      return;
    }
    if (reply.method === "Network.loadingFinished" || reply.method === "Network.loadingFailed") {
      finishCanvasRequest(reply.sessionId, reply.method, reply.params);
      return;
    }
    if (reply.method === "Network.webSocketFrameReceived") {
      const payload = reply.params.response.payloadData;
      if (payload.includes('"type":"canvas.collaboration"')) {
        revisionSignals.set(reply.sessionId, (revisionSignals.get(reply.sessionId) ?? 0) + 1);
        revisionBytes += Buffer.byteLength(payload);
      }
      return;
    }
    if (reply.method === "Fetch.requestPaused") {
      void dropCommittedResponse(reply.sessionId, reply.params);
      return;
    }
    const respond = outstanding.get(reply.id);
    outstanding.delete(reply.id);
    respond?.(reply);
  });
}

function recordSubmission({ request, wallTime }) {
  if (request.method !== "POST" || !request.url.endsWith("/collaboration/ops") || !request.postData) return;
  const batch = JSON.parse(request.postData);
  for (const change of batch.changes ?? []) {
    const title = change.fields?.["data.label"];
    if (typeof title === "string" && title.startsWith("网页 ") && !submissions.has(title)) {
      submissions.set(title, wallTime * 1000);
    }
  }
}

function trackCanvasRequest(sessionId, { requestId, request, timestamp }) {
  const path = new URL(request.url).pathname;
  if (request.method === "GET" && !path.endsWith("/collaboration/events")) return;
  if (request.method === "POST" && !path.endsWith("/collaboration/ops")) return;
  if (request.method !== "GET" && request.method !== "POST") return;
  requests.set(`${sessionId}/${requestId}`, { method: request.method, timestamp });
}

function finishCanvasRequest(sessionId, event, { requestId, timestamp, encodedDataLength }) {
  const key = `${sessionId}/${requestId}`;
  const start = requests.get(key);
  requests.delete(key);
  if (!start || event !== "Network.loadingFinished") return;
  if (!requestDurations.has(start.method)) requestDurations.set(start.method, []);
  requestDurations.get(start.method).push((timestamp - start.timestamp) * 1000);
  requestBytes.set(start.method, (requestBytes.get(start.method) ?? 0) + encodedDataLength);
}

async function evaluate(sessionId, expression) {
  const result = await command("Runtime.evaluate", { expression, returnByValue: true }, sessionId);
  if (result.exceptionDetails) throw new Error(result.exceptionDetails.text);
  return result.result.value;
}

const selector = (index) => `.react-flow__node[data-id="node-${index}"]`;

async function openCanvas(identity, index) {
  const { browserContextId } = await command("Target.createBrowserContext");
  const { targetId } = await command("Target.createTarget", { url: "about:blank", browserContextId });
  const { sessionId } = await command("Target.attachToTarget", { targetId, flatten: true });
  await command("Emulation.setDeviceMetricsOverride", { width: 2560, height: 1280, deviceScaleFactor: 1, mobile: false }, sessionId);
  await command("Page.enable", {}, sessionId);
  await command("Network.enable", {}, sessionId);
  await command("Page.addScriptToEvaluateOnNewDocument", {
    source: `localStorage.setItem('video_design_token', ${JSON.stringify(identity.token)}); localStorage.setItem('video_design_role','user');`,
  }, sessionId);
  await command("Page.navigate", { url: `${identity.base}/canvas/${fixture.code}/collaboration` }, sessionId);
  try {
    await until(`网页 ${index + 1} 的画布节点`, () => evaluate(sessionId, `!!document.querySelector(${JSON.stringify(selector(index))})`), 15_000);
  } catch (error) {
    const state = await evaluate(sessionId, `({path: location.pathname, text: document.body.innerText.slice(0,400), nodes: [...document.querySelectorAll('.react-flow__node')].slice(0,8).map(node=>node.dataset.id), xhr: performance.getEntriesByType('resource').filter(entry=>entry.name.includes('/api/')).slice(0,12).map(entry=>({path:new URL(entry.name).pathname,status:entry.responseStatus}))})`);
    throw new Error(`${error.message} ${JSON.stringify(state)}`);
  }
  await evaluate(sessionId, `(() => {
    window.canvasMixedApplications = {};
    const observer = new MutationObserver(() => {
      for (const index of [0, 1, 2, 3]) {
        const title = document.querySelector('.react-flow__node[data-id="node-' + index + '"] [title="双击重命名"]')?.textContent;
        if (title?.startsWith('网页 ')) window.canvasMixedApplications[title] ??= Date.now();
      }
    });
    observer.observe(document.body, { childList: true, characterData: true, subtree: true });
  })()`);
  return { sessionId, index, edits: 0 };
}

async function renameNode(client) {
  const { sessionId, index } = client;
  const node = JSON.stringify(selector(index));
  await evaluate(sessionId, `document.querySelector(${node})?.querySelector('[title="双击重命名"]')?.dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))`);
  const input = `${node}.concat(' input[aria-label="节点名称"]')`;
  await until(`网页 ${index + 1} 的名称输入`, () => evaluate(sessionId, `!!document.querySelector(${input})`));
  const title = `网页 ${index + 1} 镜头 ${client.edits + 1}`;
  await evaluate(sessionId, `(() => {
    const input = document.querySelector(${input});
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set.call(input, ${JSON.stringify(title)});
    input.dispatchEvent(new Event('input', { bubbles: true }));
  })()`);
  await evaluate(sessionId, `document.querySelector(${input}).blur()`);
  await until(`网页 ${index + 1} 本地显示新名称`, () => evaluate(sessionId, `document.querySelector(${node})?.textContent.includes(${JSON.stringify(title)})`));
  client.edits += 1;
  return title;
}

function observedTitles(client, labels) {
  const expression = `(() => {
    const content = (id) => document.querySelector('.react-flow__node[data-id="' + id + '"] [title="双击重命名"]')?.textContent;
    return Object.entries(${JSON.stringify(labels)}).every(([id, title]) => content(id) === title);
  })()`;
  return until(`网页 ${client.index + 1} 接收其他参与者的修改`, () => evaluate(client.sessionId, expression), 20_000);
}

async function setOffline(client, offline) {
  await command("Network.emulateNetworkConditions", {
    offline, latency: 80, downloadThroughput: -1, uploadThroughput: -1,
  }, client.sessionId);
}

async function armResponseLoss(client) {
  const response = { complete: false };
  droppedResponses.set(client.sessionId, response);
  await command("Fetch.enable", {
    patterns: [{ urlPattern: "*/collaboration/ops", requestStage: "Response" }],
  }, client.sessionId);
  return response;
}

async function dropCommittedResponse(sessionId, { requestId, request, responseStatusCode }) {
  const response = droppedResponses.get(sessionId);
  try {
    if (!response || response.complete || request.method !== "POST" || responseStatusCode !== 200) {
      await command("Fetch.continueRequest", { requestId }, sessionId);
      return;
    }
    response.complete = true;
    await command("Fetch.failRequest", { requestId, errorReason: "ConnectionReset" }, sessionId);
    await command("Fetch.disable", {}, sessionId);
  } catch (error) {
    if (response) response.error = error;
  }
}

try {
  await connectBrowser();
  const clients = await Promise.all(fixture.sessions.map(openCanvas));
  await Promise.all(clients.map((client) => setOffline(client, false)));
  await writeFile(join(fixture.dir, "ready"), "", { mode: 0o600 });
  const started = Date.now();
  let recovered = false;
  let responseLoss;
  let titles;
  const propagation = [];
  const remoteApplications = [];
  while (Date.now() - started < fixture.durationMs) {
    const elapsed = Date.now() - started;
    const offlineWindow = !recovered && elapsed > fixture.durationMs / 3;
    const droppedResponseWindow = !responseLoss && elapsed > fixture.durationMs * 2 / 3;
    if (offlineWindow) await setOffline(clients[3], true);
    if (droppedResponseWindow) responseLoss = await armResponseLoss(clients[2]);
    const begin = Date.now();
    titles = await Promise.all(clients.map(renameNode));
    if (offlineWindow) {
      await delay(1750);
      await setOffline(clients[3], false);
      recovered = true;
    }
    const labels = Object.fromEntries(titles.map((title, index) => [`node-${index}`, title]));
    await Promise.all(clients.map(async (client) => {
      await observedTitles(client, labels);
      if (offlineWindow || droppedResponseWindow) return;
      propagation.push(Date.now() - begin);
      const applied = await evaluate(client.sessionId, `(${JSON.stringify(titles)}).map(title => window.canvasMixedApplications[title] ?? null)`);
      applied.forEach((timestamp, index) => {
        const submitted = submissions.get(titles[index]);
        if (index !== client.index && submitted !== undefined && timestamp >= submitted) {
          remoteApplications.push(timestamp - submitted);
        }
      });
    }));
    for (const title of titles) submissions.delete(title);
    await delay(Math.max(0, 3000 - (Date.now() - begin)));
  }
  if (!recovered || !titles) throw new Error("未完成断网恢复");
  if (!responseLoss?.complete || responseLoss.error) throw new Error("未验证已提交操作的响应丢失", { cause: responseLoss?.error });
  await writeFile(join(fixture.dir, "editing-done"), "", { mode: 0o600 });
  const finalLabels = await until("服务端确认最终状态", async () => {
    const contents = await readFile(join(fixture.dir, "final.json"), "utf8").catch((error) => {
      if (error.code !== "ENOENT") throw error;
      return "";
    });
    return contents && JSON.parse(contents);
  }, 60_000);
  titles.forEach((title, index) => {
    if (finalLabels[`node-${index}`] !== title) throw new Error(`网页 ${index + 1} 的最终改名未持久化`);
  });
  await Promise.all(clients.map((client) => observedTitles(client, finalLabels)));
  const p95 = (samples) => Math.round(samples.sort((left, right) => left - right)[Math.ceil(samples.length * 0.95) - 1]);
  if (remoteApplications.length < (fixture.durationMs > 60_000 ? 100 : 12)) throw new Error(`提交到接收端的延迟样本不足 ${remoteApplications.length}`);
  const remoteP95 = p95(remoteApplications);
  console.log(`4 个真实 Chrome 页面各提交 ${clients.map((client) => client.edits).join("/")} 次改名并收敛，断网和已提交响应丢失后恢复`);
  console.log(`网页改名到渲染 ${propagation.length} 个样本，P95 ${p95(propagation)} ms。提交到远端网页渲染 ${remoteApplications.length} 个样本，P95 ${remoteP95} ms`);
  for (const [method, samples] of requestDurations) {
    console.log(`网页 ${method} 协作请求 ${samples.length} 个样本，P95 ${p95(samples)} ms，接收 ${requestBytes.get(method)} B`);
  }
  console.log(`各网页收到的版本通知 ${clients.map(({ sessionId }) => revisionSignals.get(sessionId) ?? 0).join("/")}，通知载荷合计 ${revisionBytes} B`);
  if (fixture.durationMs >= 30 * 60_000 && remoteP95 > 300) {
    throw new Error(`混合负载中提交到远端网页渲染 P95 ${remoteP95} ms，超过 300 ms 发布门槛`);
  }
} finally {
  socket?.close();
  if (chrome.exitCode === null && chrome.signalCode === null) {
    const exited = new Promise((resolve) => chrome.once("exit", resolve));
    chrome.kill();
    await exited;
  }
}
