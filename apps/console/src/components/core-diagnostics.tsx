import { Notice, SectionHeader, Badge, Card, CardContent } from "@migate/ui";
import { Status } from "./feedback";
import type { Task } from "../lib/models";
import { bytes, date } from "../lib/models";

interface Diagnostics {
  core: string;
  observed_at?: string;
  source?: string;
  config?: {
    validity: string;
    validation?: string;
    sha256?: string;
    bytes?: number;
    inbounds?: number;
    outbounds?: number;
    rules?: number;
    modified_at?: string;
  };
  journal?: {
    validity: string;
    sampled?: number;
    errors?: number;
    categories?: Record<string, number>;
    latest_at?: string;
    possibly_limited?: boolean;
  };
}
const categories: Record<string, string> = {
  listener: "监听",
  tls: "TLS / 证书",
  authentication: "认证",
  dns: "DNS",
  configuration: "配置",
  network: "网络",
  other: "其他",
  binary: "二进制记录（未分类）",
};
export function CoreDiagnostics({
  tasks,
  deviceId,
}: {
  tasks: Task[];
  deviceId: number;
}) {
  const latest = new Map<string, Task>();
  for (const t of [...tasks].sort((a, b) => b.id - a.id)) {
    if (t.device_id !== deviceId || t.operation !== "core.inspect") continue;
    const core = (t.result as Diagnostics | undefined)?.core;
    // Pending/failed requests remain visible in the task list; never substitute
    // an old result as the result of a new failed request.
    if (t.status === "applied" && core && !latest.has(core))
      latest.set(core, t);
  }
  return (
    <section className="stack" aria-label="只读内核诊断">
      <h3>配置摘要与日志分类</h3>
      <p className="muted">
        检查当前落盘配置，执行内核校验；日志仅统计本项目服务最近 15 分钟、最多
        200 条记录。分类数量不等于连接次数，原始配置与日志不回传。
      </p>
      {!latest.size && (
        <Notice>尚无诊断回执。点击内核的“只读诊断”开始检查。</Notice>
      )}
      <div className="core-diagnostics-grid">
        {[...latest].map(([core, task]) => {
          const d = task.result as Diagnostics,
            c = d.config,
            j = d.journal;
          const fresh =
            !!d.observed_at && Date.now() - Date.parse(d.observed_at) < 60000;
          return (
            <Card className="core-diagnostic-card" size="sm" key={core}>
              <CardContent className="flex flex-col gap-2.5">
                <SectionHeader size="sm" title={<>{core} · 诊断 #{task.id}</>} actions={<Badge variant="outline">
                    {fresh ? "最近观测" : "历史观测"}
                  </Badge>} />
                <p>{date(d.observed_at)} · 来源：Gate 本地辅助程序</p>
                {c?.validity === "fresh" ? (
                  <>
                    <SectionHeader size="sm" title="配置校验" actions={<Status
                        value={c.validation === "passed" ? "applied" : "failed"}
                        label={c.validation === "passed" ? "校验通过" : "失败 / 不可用"}
                      />} />
                    {c.validation !== "passed" && (
                      <Notice tone="bad">
                        校验失败或校验器不可用；请结合任务记录与服务器受限日志排查。
                      </Notice>
                    )}
                    <p>
                      入站 {c.inbounds} · 出站 {c.outbounds} · 路由 {c.rules} ·{" "}
                      {bytes(c.bytes)}
                    </p>
                    <small>修改时间：{date(c.modified_at)}</small>
                    <details>
                      <summary>配置 SHA-256</summary>
                      <code className="break-all">{c.sha256}</code>
                    </details>
                  </>
                ) : (
                  <Notice>配置不可读取或格式无效，状态未知。</Notice>
                )}
                {j?.validity === "fresh" ? (
                  <>
                    <p>
                      日志采样 {j.sampled} 条 · systemd 错误优先级 {j.errors} 条
                    </p>
                    <div className="actions">
                      {Object.entries(j.categories ?? {}).map(([k, n]) => (
                        <Badge key={k} variant="outline">
                          {categories[k] ?? "其他"} {n}
                        </Badge>
                      ))}
                    </div>
                    {!!j.latest_at && (
                      <small>最近记录：{date(j.latest_at)}</small>
                    )}
                    {j.possibly_limited && (
                      <Notice>
                        已达到采样上限；此窗口可能还有未展示的记录。
                      </Notice>
                    )}
                  </>
                ) : (
                  <Notice>日志不可用或输出截断，数量未知。</Notice>
                )}
              </CardContent>
            </Card>
          );
        })}
      </div>
    </section>
  );
}
