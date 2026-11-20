// 节点、代理、链式代理改动后，中心在后台把凭据合并下发到 VPS。这一行如实告诉人「还有没下发完的改动」或失败原因，
// 下发完成后不占位置。
import { api } from "../api";
import { useQuery } from "../lib/query";

type SyncStatus = { pending?: boolean; requested?: number; done?: number; running?: boolean; last_error?: string; gates_pending?: number; gates_failed?: number };

const preparing = (status: SyncStatus) => status.pending ?? ((status.requested ?? 0) > (status.done ?? 0));

export function SyncLine({ admin = true }: { admin?: boolean }) {
  const q = useQuery({
    queryKey: ["sync-status", admin],
    queryFn: () => api(admin ? "/admin/sync-status" : "/me/sync-status") as Promise<SyncStatus>,
    refetchInterval: (query) => query.state.data && (preparing(query.state.data) || query.state.data.gates_pending) ? 2000 : 15000,
  });
  const s = q.data;
  if (q.error) return <span className="text-meta text-[var(--weir-warn-ink)]">下发状态未知</span>;
  if (!s) return null;
  if (s.last_error && !s.running) return <span className="text-meta text-[var(--weir-bad-ink)]" role="status">配置同步失败，正在重试</span>;
  if (s.gates_failed) return <span className="text-meta text-[var(--weir-bad-ink)]" role="status">{s.gates_failed} 台 VPS 应用配置失败</span>;
  if (preparing(s)) return <span className="text-meta text-[var(--weir-ink-3)]" role="status">改动已保存，正在准备下发</span>;
  if (s.gates_pending) return <span className="text-meta text-[var(--weir-ink-3)]" role="status">等待 {s.gates_pending} 台 VPS 应用配置</span>;
  return null;
}
