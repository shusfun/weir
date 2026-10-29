// 节点能不能从外面连上：要放行的端口（对照这台 VPS 的本机防火墙）与中心从外部连接的结果。
// 云厂商安全组 Weir 读不到，只提示去控制台放行。
import { Badge } from "@weir/ui";
import type { PortNeed, Reach } from "../lib/core";

const fwTone = { open: "ok", blocked: "bad", unknown: "neutral" } as const;
const fwText = { open: "已放行", blocked: "未放行", unknown: "未知" } as const;

export function NodeAccess({ ports, reach, compact }: { ports?: PortNeed[]; reach?: Reach; compact?: boolean }) {
  if (!ports?.length) return null;
  const blocked = ports.some((p) => p.firewall === "blocked") || reach?.status === "failed";
  return (
    <div className="stack gap-1 text-[12.5px]">
      <div className="flex flex-wrap items-center gap-1.5">
        {!compact && <span className="text-[var(--mg-ink-3)]">需放行</span>}
        {ports.map((p) => (
          <Badge key={p.proto} variant="soft" tone={fwTone[p.firewall]} title={p.why}>
            {p.proto.toUpperCase()} {p.port} · {compact ? "" : "本机防火墙"}{fwText[p.firewall]}
          </Badge>
        ))}
        {reach && (
          <Badge variant="soft" tone={reach.status === "ok" ? "ok" : reach.status === "failed" ? "bad" : "neutral"} title={reach.why}>
            外部连接{reach.status === "ok" ? "正常" : reach.status === "failed" ? "失败" : "未知"}
          </Badge>
        )}
      </div>
      {!compact && (
        <small className="text-[var(--mg-ink-3)]">
          {blocked ? ports.find((p) => p.firewall === "blocked")?.why ?? reach?.why : reach?.why}
          {" "}云服务器还要在云厂商控制台的安全组里放行这些端口，Weir 读不到安全组。
        </small>
      )}
    </div>
  );
}
