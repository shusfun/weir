// 审计记录的展示规则：结果串翻成中文、区分发起方。页面与测试共用，不依赖 React。

export const resultLabels: Record<string, string> = {
  all: "全部结果",
  ok: "成功",
  denied: "拒绝",
  failed: "失败",
  queued: "已排队",
};

// 结果串里「键=值」附加信息的中文名；没登记的键不在正文里猜，只留在详情的原始记录
const resultExtras: Record<string, string> = { chains: "链式代理", nodes: "节点", users: "用户", links: "链接", gates: "VPS", count: "数量", version: "版本", attempts: "尝试次数" };

// 把「ok chains=0」翻成 { label: "成功", extras: ["链式代理 0"] }
export function describeResult(result: string) {
  const [code = "", ...rest] = result.split(" ").filter(Boolean);
  const extras = rest.flatMap((piece) => {
    const [key, value] = piece.split("=");
    return value !== undefined && resultExtras[key] ? [`${resultExtras[key]} ${value}`] : [];
  });
  return { label: code === "all" ? "其他结果" : resultLabels[code] ?? (code ? "其他结果" : "未记录"), extras };
}

// 发起方：dsh 起草的业务修改带 ai. / ops. 前缀；有管理员编号表示经管理员确认，没有表示 dsh 在护栏内自动执行
export type Origin = { kind: "dsh-confirmed" | "dsh-auto" | "admin" | "system"; label: string };
export function originOf(a: { action: string; actor: number; actor_name: string }): Origin {
  const dsh = a.action.startsWith("ai.") || a.action.startsWith("ops.");
  if (dsh) return a.actor ? { kind: "dsh-confirmed", label: `dsh 起草 · ${a.actor_name || "管理员"}确认` } : { kind: "dsh-auto", label: "dsh 自动执行" };
  return a.actor ? { kind: "admin", label: a.actor_name || "管理员" } : { kind: "system", label: "系统" };
}

export const originText: Record<Origin["kind"], string> = { "dsh-confirmed": "dsh 起草，管理员确认", "dsh-auto": "dsh 自动执行（受护栏限制）", admin: "管理员", system: "系统 / 未登录" };
