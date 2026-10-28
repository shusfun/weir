// 订阅检查结果：中心用 mihomo -t / sing-box check 检查生成的 Clash / sing-box 订阅。
import { Badge } from "@weir/ui";

export interface SubCheck {
  status: "passed" | "failed" | "unknown";
  kernel: string;
  version?: string;
  error?: string;
  at: string;
  ref: string;
  name: string;
  target: string;
}
const checkTone = { passed: ["通过", "ok"], failed: ["未通过", "bad"], unknown: ["未知", "neutral"] } as const;

export function SubCheckBadge({ check }: { check: Pick<SubCheck, "status" | "kernel" | "version"> }) {
  const [label, tone] = checkTone[check.status];
  return <Badge variant="soft" tone={tone}>{check.kernel === "mihomo" ? "mihomo -t" : "sing-box check"} · {label}</Badge>;
}

