// 教程只记录步骤与请求结果，不能收集表单值、资源地址或响应正文。
export type GuideRecord = { trace: string; tour: string; step: string; stage: string; ms: number; method?: string; status?: number };
export type GuideRequest = { trace: string; path: string; method: string; status: number };
let active: { trace: string; tour: string; step: string; since: number } | null = null;
const records: GuideRecord[] = [];
const listeners = new Set<(request: GuideRequest) => void>();
export const guideTraceId = () => active?.trace;
export const guideEvidence = () => records.slice();
export function guideStage(stage: string, extra: Pick<GuideRecord, "method" | "status"> = {}) {
  if (!active) return;
  const record = { trace: active.trace, tour: active.tour, step: active.step, stage, ms: Math.round(performance.now() - active.since), ...extra };
  records.push(record);
  if (records.length > 200) records.shift();
  window.dispatchEvent(new CustomEvent("weir:guide:trace", { detail: record }));
}
export function beginGuide(tour: string) {
  active = { trace: Array.from(crypto.getRandomValues(new Uint8Array(16)), b => b.toString(16).padStart(2, "0")).join(""), tour, step: "", since: performance.now() };
  guideStage("start");
}
export function guideStep(step: string) { if (active) { active.step = step; guideStage("step"); } }
export function endGuide(reason: string) { guideStage(reason); active = null; }
export function guideRequest(trace: string | undefined, path: string, method: string, status: number) {
  if (!active || trace !== active.trace || method === "GET") return;
  guideStage(status === 202 ? "operation-pending" : status >= 200 && status < 300 ? "request-success" : "request-failed", { method, status });
  for (const listener of listeners) listener({ trace, path: path.split("?")[0], method, status });
}
export function onGuideRequest(listener: (request: GuideRequest) => void) { listeners.add(listener); return () => { listeners.delete(listener); }; }
