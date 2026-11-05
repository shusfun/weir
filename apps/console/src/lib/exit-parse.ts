// 只在浏览器内拆分代理资料；最终权限、地址与连接校验仍由中心负责。
export type ProxyKind = "socks5" | "http" | "https";
export interface ParsedProxy {
  kind: ProxyKind;
  address: string;
  port: number;
  username?: string;
  password?: string;
}
export type Parsed = { type: "proxy"; proxy: ParsedProxy; notice: string } | { type: "link"; link: string } | { type: "subscription" } | { type: "error"; message: string };
const error = (message: string): Parsed => ({ type: "error", message });
const schemes: Record<string, ProxyKind> = { socks5: "socks5", socks5h: "socks5", socks: "socks5", http: "http", https: "https" };
const linkSchemes = ["vless", "vmess", "trojan", "ss", "hysteria2", "hy2", "tuic", "anytls"];
function host(s: string) { return /^\[[0-9a-f:.]+\]$/i.test(s) || /^[^\s:/@?#\[\]，,；;]+$/.test(s); }
function splitHost(s: string) {
  const m = s.match(/^(\[[0-9a-f:.]+\]|[^:\s\[\]]+):(\d{1,5})$/i);
  return m && host(m[1]) && +m[2] > 0 && +m[2] <= 65535 ? { address: m[1].replace(/^\[|\]$/g, ""), port: +m[2] } : null;
}
function result(p: Omit<ParsedProxy, "kind"> & { kind?: ProxyKind }): Parsed {
  if (!host(p.address.includes(":") ? `[${p.address}]` : p.address) || !Number.isInteger(p.port) || p.port < 1 || p.port > 65535) return error("地址或端口不合法，端口须在 1–65535 之间");
  return { type: "proxy", proxy: { ...p, kind: p.kind ?? "http" }, notice: [!p.kind && "未提供协议，暂按 HTTP，可修改", (!p.username || !p.password) && "认证信息不完整，请确认是否需要用户名和密码"].filter(Boolean).join("；") };
}
function single(text: string): Parsed {
  const scheme = text.match(/^([a-z0-9+.-]+):\/\//i)?.[1]?.toLowerCase();
  if (scheme && linkSchemes.includes(scheme)) return { type: "link", link: text };
  if (scheme) {
    const kind = schemes[scheme];
    if (!kind) return error("不支持的代理协议，请选择 HTTP、HTTPS 或 SOCKS5");
    try {
      const u = new URL(text);
      if ((kind === "http" || kind === "https") && ((u.pathname && u.pathname !== "/") || u.search)) return { type: "subscription" };
      if (u.hash || (kind === "socks5" && u.pathname && u.pathname !== "/")) return error("代理地址不能包含路径或片段");
      return result({ kind, address: u.hostname.replace(/^\[|\]$/g, ""), port: Number(u.port || (kind === "https" ? 443 : kind === "http" ? 80 : 0)), username: decodeURIComponent(u.username), password: decodeURIComponent(u.password) });
    } catch { return error("地址格式或 URL 编码不合法"); }
  }
  // 优先识别 host:port:user:password，避免密码中的 @ 被当成分隔符。
  const compact = text.match(/^(\[[0-9a-f:.]+\]|[^:\s]+):(\d{1,5}):([^:]*):(.*)$/i);
  if (compact) {
    const hp = splitHost(`${compact[1]}:${compact[2]}`);
    if (hp) return result({ ...hp, username: compact[3], password: compact[4] });
  }
  const at = text.lastIndexOf("@");
  if (at > 0) {
    const hp = splitHost(text.slice(at + 1)), cred = text.slice(0, at), i = cred.indexOf(":");
    if (hp && i > 0) return result({ ...hp, username: cred.slice(0, i), password: cred.slice(i + 1) });
  }
  const hp = splitHost(text.trimEnd());
  if (hp) return result(hp);
  const parts = text.split(/\s+/);
  if ((parts.length === 2 || parts.length === 4) && host(parts[0]) && /^\d+$/.test(parts[1])) return result({ address: parts[0].replace(/^\[|\]$/g, ""), port: +parts[1], username: parts[2], password: parts[3] });
  return error("认不出字段顺序；请粘贴一个代理，或修改下面的字段");
}
const labels: Record<string, string> = {
  "代理入口": "address", "代理地址": "address", "地址": "address", ip: "address", host: "address", hostname: "address", server: "address", address: "address",
  "端口": "port", port: "port", "用户名": "username", "账号": "username", "帐号": "username", username: "username", user: "username", login: "username",
  "密码": "password", password: "password", pass: "password", "协议": "kind", protocol: "kind", type: "kind",
};
export function parseExitText(raw: string): Parsed {
  if (!raw.trim()) return error("内容为空");
  if (raw.length > 16384) return error("内容过长，请只粘贴一个代理的信息");
  const lines = raw.replace(/\r\n?/g, "\n").split("\n");
  const fields: Record<string, string> = {};
  let tagged = false;
  const extras: string[] = [];
  for (const line of lines) {
    if (!line.trim()) continue;
    const m = line.match(/^\s*([^:：]+?)\s*[:：][ \t]?(.*)$/);
    const key = m && labels[m[1].trim().toLowerCase()];
    if (!key) { extras.push(line.trim()); continue; }
    tagged = true;
    // 密码只移除标签后的一个排版空格，其余字符完整保留。
    const value = key === "password" ? m![2] : m![2].trim();
    if (key in fields && fields[key] !== value) return error("同一字段出现不同内容，请只保留一个代理的信息");
    fields[key] = value;
  }
  if (!tagged) {
    if (lines.filter(l => l.trim()).length > 1) {
      const parts = lines.filter(l => l.trim()).map(l => l.trim());
      if (parts.length === 4 && host(parts[0]) && /^\d+$/.test(parts[1])) return single(parts.join("\t"));
      return error("检测到多行不明确内容，请只保留一个代理，或使用地址、端口等字段标签");
    }
    return single(lines.find(line => line.trim())!.trimStart());
  }
  if (extras.some(s => /:\/\//.test(s) || /^(?:\[[0-9a-f:.]+\]|[\w.-]+):\d+/i.test(s))) return error("检测到额外代理地址，请只保留一个代理的信息");
  let kind: ProxyKind | undefined;
  if (fields.kind) {
    const name = fields.kind.match(/^(socks5h?|socks|https?|HTTP代理|HTTPS代理)(?=$|[\s（(])/i)?.[1]?.toLowerCase().replace(/代理$/, "");
    if (!name || !schemes[name]) return error("协议无法确认，请选择 HTTP、HTTPS 或 SOCKS5");
    kind = schemes[name];
  }
  if (!fields.address) return error("没有识别到代理地址，请补充地址");
  const hp = splitHost(fields.address);
  if (hp && fields.port && +fields.port !== hp.port) return error("地址中的端口与端口字段不一致，请修改后重试");
  if (!hp && !fields.port) return error("没有识别到端口，请补充端口");
  if (fields.port && !/^\d+$/.test(fields.port)) return error("端口须在 1–65535 之间");
  return result({ kind, address: hp?.address ?? fields.address.replace(/^\[|\]$/g, ""), port: hp?.port ?? +fields.port, username: fields.username, password: fields.password });
}
