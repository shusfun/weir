export type TestPurpose = "https" | "webrtc" | "webtransport";
export type BrowserSession = { id: string; token: string; endpoint: string; expires_at: string; purpose: TestPurpose; max_bytes: number; certificate_hash?: string };
export type BrowserResult = { status: "passed" | "failed" | "unavailable"; reason: string; latency_ms?: number; download_ms?: number; bytes: number };

interface DatagramTransport {
  ready: Promise<void>;
  closed: Promise<unknown>;
  close(): void;
  datagrams: { writable: WritableStream<Uint8Array>; readable: ReadableStream<Uint8Array> };
}
type DatagramConstructor = new (url: string, options?: { serverCertificateHashes: { algorithm: "sha-256"; value: Uint8Array }[] }) => DatagramTransport;

export function browserSession(value: unknown): BrowserSession {
  if (!value || typeof value !== "object" || !("id" in value) || typeof value.id !== "string" || !/^[a-f0-9]{32}$/.test(value.id)
    || !("token" in value) || typeof value.token !== "string" || value.token.length > 2048
    || !("endpoint" in value) || typeof value.endpoint !== "string"
    || !("expires_at" in value) || typeof value.expires_at !== "string" || !Number.isFinite(Date.parse(value.expires_at))
    || !("purpose" in value) || (value.purpose !== "https" && value.purpose !== "webrtc" && value.purpose !== "webtransport")
    || !("max_bytes" in value) || typeof value.max_bytes !== "number" || value.max_bytes < 1 || value.max_bytes > 16 * 1024 * 1024) throw new Error("测试会话响应无效");
  const endpoint = new URL(value.endpoint);
  if (endpoint.protocol !== "https:" || endpoint.username || endpoint.password || endpoint.pathname !== "/" || endpoint.search || endpoint.hash) throw new Error("测试端点地址无效");
  const certificate_hash = "certificate_hash" in value ? value.certificate_hash : undefined;
  if (certificate_hash !== undefined && (typeof certificate_hash !== "string" || !/^[a-f0-9]{64}$/.test(certificate_hash) || value.purpose !== "webtransport")) throw new Error("测试证书指纹无效");
  return { id: value.id, token: value.token, endpoint: endpoint.origin, expires_at: value.expires_at, purpose: value.purpose, max_bytes: value.max_bytes, certificate_hash };
}

async function bounded<T>(promise: Promise<T>, signal: AbortSignal): Promise<T> {
  signal.throwIfAborted();
  let abort = () => {};
  try {
    return await Promise.race([promise, new Promise<never>((_, reject) => {
      abort = () => reject(new Error("测试已取消或超时"));
      signal.addEventListener("abort", abort, { once: true });
    })]);
  } finally { signal.removeEventListener("abort", abort); }
}

async function request(session: BrowserSession, path: string, signal: AbortSignal, init?: RequestInit) {
  const response = await fetch(`${session.endpoint}${path}`, { ...init, signal, cache: "no-store", credentials: "omit", headers: { Authorization: `Bearer ${session.token}`, ...init?.headers } });
  if (!response.ok) throw new Error(`测试端点返回 ${response.status}`);
  return response;
}

export async function measureBrowserPath(session: BrowserSession, signal: AbortSignal): Promise<BrowserResult> {
  signal.throwIfAborted();
  if (!window.isSecureContext) return { status: "unavailable", reason: "当前页面不是安全上下文，请使用可信 HTTPS 或本机回环地址", bytes: 0 };
  if (session.purpose === "https") {
    const start = performance.now();
    const ping = await (await request(session, "/latency", signal)).arrayBuffer();
    const latency_ms = performance.now() - start;
    const download = performance.now();
    const body = await (await request(session, "/download", signal)).arrayBuffer();
    if (ping.byteLength !== 64 || body.byteLength !== 4 * 1024 * 1024) throw new Error("测试数据不完整");
    return { status: "passed", reason: "HTTPS 延迟与下载完成", latency_ms, download_ms: performance.now() - download, bytes: ping.byteLength + body.byteLength };
  }
  if (session.purpose === "webrtc") {
    if (!("RTCPeerConnection" in window)) return { status: "unavailable", reason: "浏览器不支持 WebRTC", bytes: 0 };
    const peer = new RTCPeerConnection({ iceServers: [] });
    const channel = peer.createDataChannel("weir-test", { ordered: false, maxRetransmits: 0 });
    const abort = () => peer.close();
    signal.addEventListener("abort", abort, { once: true });
    try {
      await peer.setLocalDescription(await peer.createOffer());
      const response = await request(session, "/rtc", signal, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(peer.localDescription) });
      const answer: unknown = await response.json();
      if (!answer || typeof answer !== "object" || !("type" in answer) || answer.type !== "answer" || !("sdp" in answer) || typeof answer.sdp !== "string") throw new Error("测试服务返回了无效协商结果");
      let start = 0;
      const payload = crypto.randomUUID();
      let receive: (milliseconds: number) => void = () => {};
      let fail: (error: Error) => void = () => {};
      const received = new Promise<number>((resolve, reject) => { receive = resolve; fail = reject; });
      // Attach handlers before signaling; observe rejection even if signaling fails.
      void received.catch(() => {});
      channel.onopen = () => { start = performance.now(); channel.send(payload); };
      channel.onmessage = event => { event.data === payload ? receive(performance.now() - start) : fail(new Error("UDP 回显不匹配")); };
      channel.onerror = () => fail(new Error("UDP 通道失败"));
      await bounded(peer.setRemoteDescription({ type: "answer", sdp: answer.sdp }), signal);
      const latency_ms = await bounded(received, signal);
      let udp = false;
      const stats = await peer.getStats();
      stats.forEach(report => {
        if (report.type === "candidate-pair" && report.state === "succeeded" && report.nominated) {
          const local = stats.get(report.localCandidateId), remote = stats.get(report.remoteCandidateId);
          udp = local?.protocol === "udp" && remote?.protocol === "udp" && local?.candidateType !== "relay" && remote?.candidateType !== "relay";
        }
      });
      if (!udp) return { status: "unavailable", reason: "数据已回显，但浏览器未提供可确认的直连 UDP 路径", bytes: 72 };
      return { status: "passed", reason: "浏览器与 VPS 的直连 UDP 往返通过", latency_ms, bytes: 72 };
    } finally { signal.removeEventListener("abort", abort); channel.close(); peer.close(); }
  }
  const Transport = (window as Window & { WebTransport?: DatagramConstructor }).WebTransport;
  if (!Transport) return { status: "unavailable", reason: "浏览器不支持 WebTransport，QUIC 未覆盖", bytes: 0 };
  const hash = session.certificate_hash;
  const options = hash ? { serverCertificateHashes: [{ algorithm: "sha-256" as const, value: Uint8Array.from({ length: 32 }, (_, i) => Number.parseInt(hash.slice(i * 2, i * 2 + 2), 16)) }] } : undefined;
  const transport = new Transport(`${session.endpoint}/quic?token=${encodeURIComponent(session.token)}`, options);
  void transport.closed.catch(() => {});
  const abort = () => transport.close();
  signal.addEventListener("abort", abort, { once: true });
  try {
    await bounded(transport.ready, signal);
    const writer = transport.datagrams.writable.getWriter(), reader = transport.datagrams.readable.getReader();
    try {
      const payload = crypto.getRandomValues(new Uint8Array(64));
      const start = performance.now();
      await bounded(writer.write(payload), signal);
      const result = await bounded(reader.read(), signal);
      if (result.done || result.value.length !== payload.length || result.value.some((b, i) => b !== payload[i])) throw new Error("QUIC 回显不匹配");
      return { status: "passed", reason: "浏览器与 VPS 的 QUIC 往返通过", latency_ms: performance.now() - start, bytes: 128 };
    } finally { transport.close(); await reader.cancel().catch(() => {}); writer.releaseLock(); reader.releaseLock(); }
  } finally { signal.removeEventListener("abort", abort); transport.close(); }
}
