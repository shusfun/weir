// 粘贴识别：把商家给的代理文本拆成协议、地址、端口、用户名、密码，或判断是一条分享链接。
// 只做填表辅助，最终以中心的校验为准（订阅地址在中心同样会被拒绝）。

export type ProxyKind = "socks5" | "http" | "https";
export interface ParsedProxy {
  kind?: ProxyKind;
  address: string;
  port: number;
  username?: string;
  password?: string;
}
export type Parsed = { type: "proxy"; proxy: ParsedProxy } | { type: "link"; link: string } | { type: "subscription" } | { type: "error"; message: string };

const linkSchemes = ["vless", "vmess", "trojan", "ss", "hysteria2", "hy2", "tuic", "anytls"];

// splitHost 拆出 host:port（IPv6 用方括号）；端口不合法时返回 null。
function splitHost(s: string): { address: string; port: number } | null {
  const m = s.match(/^\[([0-9a-fA-F:.]+)\]:(\d{1,5})$/) ?? s.match(/^([^:\s\[\]]+):(\d{1,5})$/);
  if (!m) return null;
  const port = Number(m[2]);
  return port >= 1 && port <= 65535 ? { address: m[1], port } : null;
}

export function parseExitText(raw: string): Parsed {
  const text = raw.trim();
  if (!text) return { type: "error", message: "内容为空" };
  if (/\s/.test(text)) return { type: "error", message: "一次只能粘贴一个出口" };
  const scheme = text.match(/^([a-z0-9+.-]+):\/\//i)?.[1]?.toLowerCase();
  if (scheme && linkSchemes.includes(scheme)) return { type: "link", link: text };
  if (scheme) {
    const schemes: Record<string, ProxyKind> = { socks5: "socks5", socks5h: "socks5", socks: "socks5", http: "http", https: "https" };
    const kind = schemes[scheme];
    if (!kind) return { type: "error", message: `不支持 ${scheme}:// 开头的地址` };
    let u: URL;
    try {
      u = new URL(text);
    } catch {
      return { type: "error", message: "地址格式不对" };
    }
    if ((kind === "http" || kind === "https") && ((u.pathname && u.pathname !== "/") || u.search)) return { type: "subscription" };
    const port = Number(u.port || (kind === "https" ? 443 : kind === "http" ? 80 : 0));
    if (!u.hostname || !port) return { type: "error", message: "缺少端口" };
    return { type: "proxy", proxy: { kind, address: u.hostname.replace(/^\[|\]$/g, ""), port, username: decodeURIComponent(u.username) || undefined, password: decodeURIComponent(u.password) || undefined } };
  }
  // user:pass@host:port
  const at = text.lastIndexOf("@");
  if (at > 0) {
    const hp = splitHost(text.slice(at + 1));
    const cred = text.slice(0, at);
    const i = cred.indexOf(":");
    if (hp && i > 0) return { type: "proxy", proxy: { ...hp, username: cred.slice(0, i), password: cred.slice(i + 1) } };
  }
  // host:port 或 host:port:user:pass（密码里可能还有冒号）
  const hp = splitHost(text);
  if (hp) return { type: "proxy", proxy: hp };
  const parts = text.split(":");
  if (parts.length >= 4) {
    const head = splitHost(parts.slice(0, 2).join(":"));
    if (head) return { type: "proxy", proxy: { ...head, username: parts[2], password: parts.slice(3).join(":") } };
  }
  return { type: "error", message: "认不出这段内容；可以直接填下面的地址与端口" };
}
