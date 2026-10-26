// 测试用的子进程：不停地给一个会话追加事件，每追加一条在 stdout 打印它的序号。父进程在中途 SIGKILL 它，
// 再重新打开数据库，检查没有空洞、没有丢失已经确认过的事件。
import { openStore } from "../../src/store/store.ts";

const [dbPath, sessionId] = process.argv.slice(2);
const store = openStore(dbPath, { workerId: 2 });
for (let i = 0; ; i++) {
  const ev = store.appendEvent(sessionId, "0", "message.delta", { i });
  process.stdout.write(`${ev.seq}\n`);
}
