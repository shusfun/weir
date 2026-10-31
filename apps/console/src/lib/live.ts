// 事件流是否连着：连着时任务、配对、草稿由中心推送，不再轮询；断开时各处退回轮询，并写明「实时推送不可用」。
import { useSyncExternalStore } from "react";

let live = false;
const listeners = new Set<() => void>();

export function isLive() {
  return live;
}

export function setLive(next: boolean) {
  if (next === live) return;
  live = next;
  for (const f of listeners) f();
}

export function onLiveChange(f: () => void) {
  listeners.add(f);
  return () => void listeners.delete(f);
}

export function useLive() {
  return useSyncExternalStore(onLiveChange, isLive, isLive);
}

// 轮询时写在等待行末尾的说明
export const pollingNote = "实时推送不可用，每秒刷新";
