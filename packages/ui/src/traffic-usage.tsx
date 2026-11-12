// 流量读数统一单位；未知、不限和零额度分别展示，图形封顶不影响实际读数。
import { cn } from "cn";

export type TrafficUsageProps = {
  used: number | null | undefined;
  quota: number | null | undefined;
  unlimited?: boolean;
  className?: string;
};

const valid = (n: number | null | undefined): n is number => typeof n === "number" && Number.isFinite(n) && n >= 0;

export function trafficReading({ used, quota, unlimited = false }: TrafficUsageProps) {
  const scale = Math.max(valid(used) ? used : 0, !unlimited && valid(quota) ? quota : 0);
  const power = Math.min(4, Math.max(0, Math.floor(Math.log(Math.max(1, scale)) / Math.log(1024))));
  const unit = ["B", "KiB", "MiB", "GiB", "TiB"][power];
  const number = (n: number) => (n / 1024 ** power).toLocaleString("zh-CN", { maximumFractionDigits: 1 });
  const value = valid(used) ? number(used) : "未知";
  const limit = unlimited ? "不限额度" : valid(quota) ? `${number(quota)} ${unit}` : "额度未知";
  const ratio = !unlimited && valid(used) && valid(quota) && quota > 0 ? used / quota : null;
  const state = unlimited ? "不限额度" : !valid(quota) ? "额度未知" : quota === 0 ? "零额度" : ratio == null ? "用量未知" : `${Math.round(ratio * 100)}%`;
  return { value, unit, limit, ratio, state, text: `${value}${valid(used) && (unlimited || !valid(quota)) ? ` ${unit}` : ""} / ${limit}` };
}

export function TrafficUsage(props: TrafficUsageProps) {
  const r = trafficReading(props);
  const count = r.ratio == null ? 0 : Math.min(21, Math.round(r.ratio * 21));
  return <span className={cn("weir-traffic-usage", props.className)}>
    <span className="weir-digits" aria-label={r.text}><strong className="weir-traffic-value">{r.value}{(props.unlimited || !valid(props.quota)) && valid(props.used) ? ` ${r.unit}` : ""}</strong><span className="weir-traffic-limit"> / {r.limit}</span></span>
    {r.ratio == null ? (r.state !== r.limit && <small>{r.state}</small>) : <span className="weir-mini-dots" role="img" aria-label={`已用 ${r.state}`}>
      {Array.from({ length: 21 }, (_, i) => <i key={i} data-on={i < count ? (r.ratio! >= 1 ? "bad" : r.ratio! >= 0.9 ? "warn" : "accent") : undefined} />)}
    </span>}
  </span>;
}

export function TrafficRing(props: TrafficUsageProps) {
  const r = trafficReading(props);
  const fill = Math.min(1, r.ratio ?? 0);
  return <div className={cn("weir-dot-ring weir-traffic-ring", props.className)} role="img" aria-label={`本月流量 ${r.text}，${r.state}`}>
    <svg viewBox="0 0 321 321" aria-hidden>
      {Array.from({ length: 4 }, (_, layer) => Array.from({ length: 97 + layer * 9 }, (_, i) => {
        const fraction = i / (97 + layer * 9);
        const angle = (fraction * 306 - 243) * Math.PI / 180;
        const radius = 119 + layer * 9;
        const on = fraction < fill;
        return <circle key={`${layer}-${i}`} cx={160.5 + radius * Math.cos(angle)} cy={160.5 + radius * Math.sin(angle)} r={on ? 1.9 : 1.3} opacity={on ? 0.95 - layer * 0.15 : 0.41 - layer * 0.07} data-tone={on ? (r.ratio! >= 1 ? "bad" : r.ratio! >= 0.9 ? "warn" : "acc") : "off"} />;
      }))}
    </svg>
    <div className="weir-dot-ring-center">
      <span className="weir-dot-ring-caption">本月已用 · {r.unit}</span>
      <span className="weir-dot-ring-value">{r.value}</span>
      <span className="weir-dot-ring-caption">/ {r.limit}{r.state !== r.limit ? ` · ${r.state}` : ""}</span>
    </div>
  </div>;
}
