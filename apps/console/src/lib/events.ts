// 控制台事件流（/api/v1/me/events）：每个标签页一条连接，推后台任务的变化、VPS 配对进度与新草稿，代替逐个轮询。
// 中心每 15 秒推一次 ping、每条连接最多 2 分钟；浏览器重连时带上最后的事件编号，只补发之后的变化。
// 服务端（或反代）回了非 200 时 EventSource 会永久关闭，这里按退避自己重建；35 秒没收到任何消息就当作断开（半开的连接不会报错）。
// 断开期间页面退回轮询（见 live.ts）；重连后 hello 说要重新读取（中心重启过）时，任务、草稿、配对全部重新读取。
import { isAwaited, putJob, type Job } from "./jobs";
import { setLive } from "./live";
import { queryClient } from "./query";

type OrphanHandler = (j: Job) => void;

let es: EventSource | null = null;
let lastId = "";
let heardAt = 0;
let wait = 2000;
let retry: ReturnType<typeof setTimeout> | undefined;
let watchdog: ReturnType<typeof setInterval> | undefined;
let me = 0;
let orphan: OrphanHandler | null = null;
// 本标签页见过在运行的任务：结束时没人在等（刷新过页面、关掉了窗口），由 onOrphan 提示结果
const seenRunning = new Set<string>();

function refreshAll() {
  void queryClient.invalidateQueries({ queryKey: ["job"] });
  void queryClient.invalidateQueries({ queryKey: ["jobs"] });
  void queryClient.invalidateQueries({ queryKey: ["ai-drafts"] });
  void queryClient.invalidateQueries({ queryKey: ["pairing"] });
}

function live(on: boolean) {
  setLive(on);
  // 轮询间隔取决于是否连着：变了就让相关查询重新算一次
  void queryClient.invalidateQueries({ queryKey: ["job"] });
  void queryClient.invalidateQueries({ queryKey: ["jobs"] });
  void queryClient.invalidateQueries({ queryKey: ["ai-drafts"] });
  void queryClient.invalidateQueries({ queryKey: ["pairing"] });
}

function onJob(j: Job) {
  putJob(j);
  if (j.status === "running") {
    seenRunning.add(j.id);
    return;
  }
  if (!seenRunning.delete(j.id) || j.kind !== "action" || j.owner !== me) return;
  // 202 的响应可能比事件晚到：等一会再看是不是有人在等
  setTimeout(() => {
    if (!isAwaited(j.id)) orphan?.(j);
  }, 3000);
}

function connect() {
  heardAt = Date.now();
  es = new EventSource("/api/v1/me/events" + (lastId ? "?since=" + encodeURIComponent(lastId) : ""));
  es.addEventListener("hello", (e) => {
    const h = JSON.parse((e as MessageEvent).data) as { reset: boolean; uid: number };
    me = h.uid;
    heardAt = Date.now();
    wait = 2000;
    if (h.reset) refreshAll();
    live(true);
  });
  es.addEventListener("job", (e) => {
    heardAt = Date.now();
    onJob(JSON.parse((e as MessageEvent).data) as Job);
  });
  es.addEventListener("nudge", (e) => {
    heardAt = Date.now();
    const n = JSON.parse((e as MessageEvent).data) as { kind: string; key?: string };
    if (n.kind === "drafts") void queryClient.invalidateQueries({ queryKey: ["ai-drafts"] });
    if (n.kind === "pairing" || n.kind === "gate") {
      void queryClient.invalidateQueries({ queryKey: ["pairing"] });
      void queryClient.invalidateQueries({ queryKey: ["gates"] });
    }
  });
  es.addEventListener("ping", () => (heardAt = Date.now()));
  es.addEventListener("sync", (e) => {
    heardAt = Date.now();
    lastId = (e as MessageEvent).lastEventId || lastId;
  });
  es.onerror = () => {
    // CONNECTING：浏览器自己在重连（每条连接 2 分钟后正常结束），不算断开；CLOSED 才自己重建
    if (es?.readyState === EventSource.CLOSED) reconnect();
  };
}

function reconnect() {
  if (retry) return;
  es?.close();
  es = null;
  live(false);
  retry = setTimeout(() => {
    retry = undefined;
    connect();
  }, wait);
  wait = Math.min(wait * 2, 30_000);
}

// startEvents 在登录后的控制台里调用一次；返回停止函数（退出登录时）。
export function startEvents(onOrphan: OrphanHandler) {
  orphan = onOrphan;
  if (!es && !retry) connect();
  watchdog ??= setInterval(() => {
    if (es && Date.now() - heardAt > 35_000) reconnect();
  }, 5000);
  return () => {
    orphan = null;
    es?.close();
    es = null;
    clearTimeout(retry);
    retry = undefined;
    clearInterval(watchdog);
    watchdog = undefined;
    lastId = "";
    setLive(false);
  };
}
