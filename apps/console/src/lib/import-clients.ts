const enc = encodeURIComponent;
const b64 = (s: string) => btoa(unescape(enc(s)));
export function withTarget(url: string, target: string) {
  const result = new URL(url, location.origin);
  result.searchParams.set("target", target);
  return result.href;
}

// 导入构造只包装给定地址；动态订阅由调用方选择格式，已发布快照保持原地址。
export const importClients: { name: string; platforms: string; target: string; href: (url: string, name: string) => string }[] = [
  { name: "Clash Verge / Mihomo Party", platforms: "Windows · macOS · Linux", target: "clash", href: (u, n) => `clash://install-config?url=${enc(u)}&name=${enc(n)}` },
  { name: "FlClash", platforms: "Windows · macOS · Linux · Android", target: "clash", href: (u, n) => `flclash://install-config?url=${enc(u)}&name=${enc(n)}` },
  { name: "Clash Meta for Android", platforms: "Android", target: "clash", href: (u, n) => `clashmeta://install-config?url=${enc(u)}&name=${enc(n)}` },
  { name: "Stash", platforms: "iOS · macOS", target: "clash", href: (u, n) => `stash://install-config?url=${enc(u)}&name=${enc(n)}` },
  { name: "Surfboard", platforms: "Android", target: "surge", href: (u) => `surfboard:///install-config?url=${enc(u)}` },
  { name: "sing-box", platforms: "全平台", target: "singbox", href: (u, n) => `sing-box://import-remote-profile?url=${enc(u)}#${enc(n)}` },
  { name: "Hiddify", platforms: "全平台", target: "singbox", href: (u, n) => `hiddify://import/${u}#${enc(n)}` },
  { name: "Karing", platforms: "全平台", target: "singbox", href: (u, n) => `karing://install-config?url=${enc(u)}&name=${enc(n)}` },
  { name: "Shadowrocket", platforms: "iOS", target: "base64", href: (u, n) => `shadowrocket://add/sub://${b64(u)}?remark=${enc(n)}` },
  { name: "Quantumult X", platforms: "iOS", target: "quanx", href: (u, n) => `quantumult-x:///add-resource?remote-resource=${enc(JSON.stringify({ server_remote: [`${u}, tag=${n}`] }))}` },
  { name: "Surge", platforms: "iOS · macOS", target: "surge", href: (u, n) => `surge:///install-config?url=${enc(u)}&name=${enc(n)}` },
  { name: "Loon", platforms: "iOS", target: "loon", href: (u) => `loon://import?sub=${enc(u)}` },
  { name: "Egern", platforms: "iOS", target: "egern", href: (u, n) => `egern:///policy_groups/new?name=${enc(n)}&url=${enc(u)}` },
  { name: "Streisand", platforms: "iOS", target: "base64", href: (u, n) => `streisand://import/${u}#${enc(n)}` },
  { name: "V2Box", platforms: "iOS · Android", target: "base64", href: (u, n) => `v2box://install-sub?url=${enc(u)}&name=${enc(n)}` },
  { name: "v2rayNG", platforms: "Android", target: "base64", href: (u, n) => `v2rayng://install-config?url=${enc(u)}&name=${enc(n)}` },
  { name: "NekoBox", platforms: "Android", target: "base64", href: (u, n) => `sn://subscription?url=${enc(u)}&name=${enc(n)}` },
];
