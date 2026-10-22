// 浏览器端测速与校验下载：对 GitHub 直连与管理员配置的镜像逐一测速，自动选最快且可校验的来源；
// 下载完成后按 GitHub 公布的 SHA-256 校验，不一致拒绝保存。中心不参与下载。
export interface ReleaseAsset {
  name: string;
  size: number;
  sha256?: string;
  url: string;
}

export interface ProbeResult {
  url: string;
  host: string;
  mirror: boolean;
  // 可读取正文（CORS 允许）时才能测速和校验
  verifiable: boolean;
  bytesPerSecond?: number;
  latencyMs?: number;
  error?: string;
}

const probeBytes = 256 * 1024;

export function detectPlatform(): string {
  const ua = navigator.userAgent.toLowerCase();
  if (ua.includes("android")) return "android";
  if (/iphone|ipad|ipod/.test(ua)) return "ios";
  if (ua.includes("mac os")) return "macos";
  if (ua.includes("windows")) return "windows";
  if (ua.includes("linux")) return "linux";
  return "all";
}

const patterns: Record<string, RegExp[]> = {
  windows: [/setup.*\.exe$/i, /\.exe$/i, /\.msi$/i, /windows.*\.zip$/i],
  macos: [/arm64.*\.dmg$/i, /\.dmg$/i, /\.pkg$/i],
  linux: [/amd64.*\.deb$/i, /\.deb$/i, /\.appimage$/i, /\.rpm$/i],
  android: [/arm64-v8a.*\.apk$/i, /universal.*\.apk$/i, /\.apk$/i],
};

// 按平台挑出可安装的包，推荐项排在最前
export function assetsFor(assets: ReleaseAsset[], platform: string) {
  const list = patterns[platform];
  if (!list) return [];
  const score = (name: string) => {
    const i = list.findIndex((re) => re.test(name));
    return i < 0 ? Infinity : i;
  };
  return assets.filter((a) => score(a.name) !== Infinity && !/\.(sha256|sig|blockmap)$/i.test(a.name)).sort((a, b) => score(a.name) - score(b.name));
}

export function candidates(asset: ReleaseAsset, mirrors: string[]) {
  return [{ url: asset.url, mirror: false }, ...mirrors.map((m) => ({ url: m + asset.url, mirror: true }))];
}

function timeout(ms: number) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), ms);
  return { signal: controller.signal, done: () => clearTimeout(timer), abort: () => controller.abort() };
}

// 先尝试可读取正文的测速；被 CORS 拒绝时退回只测延迟（不可校验）
export async function probe(url: string, mirror: boolean): Promise<ProbeResult> {
  const host = new URL(url).host;
  const t = timeout(8000);
  const started = performance.now();
  try {
    const res = await fetch(url, { headers: { Range: `bytes=0-${probeBytes - 1}` }, signal: t.signal, cache: "no-store", redirect: "follow" });
    if (!res.ok || !res.body) throw Error("http " + res.status);
    const reader = res.body.getReader();
    let read = 0;
    while (read < probeBytes) {
      const { done, value } = await reader.read();
      if (done) break;
      read += value.byteLength;
    }
    t.abort();
    const seconds = (performance.now() - started) / 1000;
    return { url, host, mirror, verifiable: true, bytesPerSecond: read / Math.max(seconds, 0.001), latencyMs: Math.round(seconds * 1000) };
  } catch {
    t.done();
  }
  const t2 = timeout(8000);
  const started2 = performance.now();
  try {
    await fetch(url, { mode: "no-cors", headers: { Range: "bytes=0-0" }, signal: t2.signal, cache: "no-store" });
    return { url, host, mirror, verifiable: false, latencyMs: Math.round(performance.now() - started2) };
  } catch {
    return { url, host, mirror, verifiable: false, error: "无法连接" };
  } finally {
    t2.done();
  }
}

// 可校验来源按速度排序在前；全部不可校验时按延迟排序
export function rank(results: ProbeResult[]) {
  return [...results]
    .filter((r) => !r.error)
    .sort((a, b) => {
      if (a.verifiable !== b.verifiable) return a.verifiable ? -1 : 1;
      if (a.verifiable) return (b.bytesPerSecond ?? 0) - (a.bytesPerSecond ?? 0);
      return (a.latencyMs ?? Infinity) - (b.latencyMs ?? Infinity);
    });
}

async function sha256Hex(data: ArrayBuffer) {
  const sum = await crypto.subtle.digest("SHA-256", data);
  return [...new Uint8Array(sum)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

// 完整下载并校验；通过后交给浏览器保存
export async function downloadVerified(url: string, asset: ReleaseAsset, onProgress: (ratio: number) => void, signal?: AbortSignal) {
  const res = await fetch(url, { signal, cache: "no-store" });
  if (!res.ok || !res.body) throw Error("下载失败（HTTP " + res.status + "）");
  const reader = res.body.getReader();
  const chunks: Uint8Array[] = [];
  let read = 0;
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    chunks.push(value);
    read += value.byteLength;
    if (read > asset.size) throw Error("下载内容大于公布的文件大小，已停止");
    onProgress(read / asset.size);
  }
  if (read !== asset.size) throw Error("下载不完整，请重试");
  const blob = new Blob(chunks as BlobPart[]);
  if (asset.sha256) {
    const actual = await sha256Hex(await blob.arrayBuffer());
    if (actual !== asset.sha256) throw Error("SHA-256 校验不一致，安装包可能被篡改，已拒绝保存");
  }
  const href = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = href;
  a.download = asset.name;
  document.body.append(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(href), 60000);
  return { verified: !!asset.sha256 };
}

export function speed(bytesPerSecond?: number) {
  if (!bytesPerSecond) return "未知";
  const mb = bytesPerSecond / 1024 / 1024;
  return mb >= 1 ? mb.toFixed(1) + " MB/s" : (bytesPerSecond / 1024).toFixed(0) + " KB/s";
}
