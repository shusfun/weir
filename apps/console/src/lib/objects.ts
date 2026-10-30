// 对象类型：控制台里能被引用、能跳过去的对象。每类对象有固定地址，对象链接（components/object-link.tsx）、
// dsh 的界面导航、页面感知里的「正在看的对象」都用这一份；类型与中心 internal/center/objects.go 一一对应。
//
// 地址约定：
// - 有详情窗的对象用自己的参数打开详情：?gate=、?user=、?driver=、?audit=；
// - 列表里的对象用 ?focus=类型:编号 滚到并高亮那一行（见 useFocusFromURL）；
// - 链接、链式代理在所属用户的详情里，先打开用户再高亮；只知道编号时由用户页向中心查归属。

export type ObjectType = "gate" | "node" | "user" | "link" | "chain" | "driver" | "kernel" | "source" | "profile" | "page" | "cert" | "audit";

export interface ObjectRef {
  type: ObjectType;
  // 数字编号的对象填 id；驱动（名称）、内核（内核名，或「内核@版本」指某个版本）、公开页面（地址名）填 key
  id?: number;
  key?: string;
  name?: string;
  // 链接、链式代理所属的用户
  owner?: number;
}

export const objectLabels: Record<ObjectType, string> = {
  gate: "VPS",
  node: "节点",
  user: "用户",
  link: "订阅链接",
  chain: "链式代理",
  driver: "内核驱动",
  kernel: "内核版本",
  source: "订阅源",
  profile: "订阅组",
  page: "公开页面",
  cert: "证书",
  audit: "审计记录",
};

export const objectTypes = Object.keys(objectLabels) as ObjectType[];

export function objectKey(ref: ObjectRef) {
  return ref.key ?? (ref.id !== undefined ? String(ref.id) : "");
}

// focusValue 是列表行上的 data-object 值，也是 ?focus= 的值。
export function focusValue(ref: ObjectRef) {
  return `${ref.type}:${objectKey(ref)}`;
}

// objectHref 生成对象的地址。
export function objectHref(ref: ObjectRef): string {
  const k = encodeURIComponent(objectKey(ref));
  const focus = "focus=" + encodeURIComponent(focusValue(ref));
  switch (ref.type) {
    case "gate":
      return `/gates?gate=${k}`;
    case "node":
      return `/gates?node=${k}`;
    case "user":
      return `/users?user=${k}`;
    case "link":
      // 订阅链接在「订阅管理」里：管理员按所属用户筛选后滚到这条
      return ref.owner ? `/links?user=${ref.owner}&${focus}` : `/links?${focus}`;
    case "chain":
      return ref.owner ? `/users?user=${ref.owner}&${focus}` : `/users?${ref.type}=${k}`;
    case "driver":
      return `/kernels?tab=drivers&driver=${k}`;
    case "kernel":
      return `/kernels?tab=versions&${focus}`;
    case "source":
      return `/subscriptions?tab=sources&${focus}`;
    case "profile":
      return `/subscriptions?tab=profiles&${focus}`;
    case "page":
      return `/pages?${focus}`;
    case "cert":
      return `/gates?certs=1&${focus}`;
    case "audit":
      return `/ops/audit?audit=${k}`;
  }
}

// parseTarget 把审计里的目标（如 node:12、gate:3:xray）解析成对象；认不出时返回 null。
export function parseTarget(target: string): ObjectRef | null {
  const m = /^([a-z]+):(\d+)/.exec(target);
  if (!m) return null;
  const type = m[1] as ObjectType;
  if (!["gate", "node", "user", "link", "chain", "source", "profile", "cert"].includes(type)) return null;
  return { type, id: Number(m[2]) };
}
