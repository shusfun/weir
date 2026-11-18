import type { Hotspots, OverviewCert, OverviewGate } from "./health-overview";

export function overviewDemo(now: number): { gates: OverviewGate[]; certs: OverviewCert[]; hotspots: Hotspots } {
  const relative = (minutes: number) => new Date(now + minutes * 60_000).toISOString();
  const active = { status: "active", state: "applied", online: true, last_seen_at: relative(-0.2), applied_revision: 12, config_revision: 12, last_error: "" } as const;
  const sample = { calls: 0, slow: 0, failures: 0, p95_ms: null, max_ms: null, last_anomaly_at: null };
  return {
    gates: [
      { ...active, id: 1, name: "演示 · 东京" },
      { ...active, id: 2, name: "演示 · 香港", online: false, last_seen_at: relative(-12) },
      { ...active, id: 3, name: "演示 · 新加坡", state: "failed", config_revision: 13, last_error: "演示：配置检查未通过" },
    ],
    certs: [
      { id: 1, name: "演示 · 正常证书", domain: "stable.example.test", not_after: relative(30 * 1440) },
      { id: 2, name: "演示 · 即将到期", domain: "soon.example.test", not_after: relative(3 * 1440) },
      { id: 3, name: "演示 · 已过期", domain: "expired.example.test", not_after: relative(-1440) },
    ],
    hotspots: {
      coverage_start: relative(-15),
      sampled_at: relative(0),
      items: [
        { ...sample, operation: "worker.maintenance", calls: 180, p95_ms: 3, max_ms: 8 },
        { ...sample, operation: "worker.tasks.list", calls: 12, slow: 2, p95_ms: 1480, max_ms: 1480, last_anomaly_at: relative(-3) },
        { ...sample, operation: "worker.tasks.statistics", calls: 6, failures: 3, p95_ms: 42, max_ms: 42, last_anomaly_at: relative(-2) },
        { ...sample, operation: "traffic.domains.country-data" },
      ],
    },
  };
}
