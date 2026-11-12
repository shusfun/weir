type Turnstile = {
  render: (host: HTMLElement, options: { sitekey: string; action: string; language: string; callback: (token: string) => void; "error-callback": () => void; "expired-callback": () => void }) => string;
  remove: (id: string) => void;
};
declare global { interface Window { turnstile?: Turnstile } }
let loading: Promise<Turnstile> | null = null;
const active = new WeakMap<HTMLElement, () => void>();

function load(): Promise<Turnstile> {
  if (window.turnstile) return Promise.resolve(window.turnstile);
  if (loading) return loading;
  loading = new Promise<Turnstile>((resolve, reject) => {
    const script = document.createElement("script");
    const fail = () => { clearTimeout(timer); script.remove(); loading = null; reject(new Error("Turnstile 服务无法加载，请稍后重试")); };
    const timer = window.setTimeout(fail, 15000);
    script.src = "https://challenges.cloudflare.com/turnstile/v0/api.js?render=explicit";
    script.async = true;
    script.addEventListener("error", fail, { once: true });
    script.addEventListener("load", () => { clearTimeout(timer); if (window.turnstile) resolve(window.turnstile); else fail(); }, { once: true });
    document.head.append(script);
  });
  return loading;
}

export async function turnstileProof(host: HTMLElement, sitekey: string, purpose: string, signal: AbortSignal, expired: () => void): Promise<string> {
  const api = await load();
  if (signal.aborted) throw new DOMException("已取消", "AbortError");
  active.get(host)?.();
  return new Promise<string>((resolve, reject) => {
    let settled = false;
    let widget: string | undefined;
    const finish = (token?: string, error?: Error) => {
      clearTimeout(timer);
      if (settled) return;
      settled = true;
      if (token) resolve(token); else reject(error ?? new Error("Turnstile 未完成，请重新验证"));
    };
    const abort = () => { if (widget) api.remove(widget); active.delete(host); signal.removeEventListener("abort", abort); finish(undefined, new DOMException("已取消", "AbortError")); };
    const timer = window.setTimeout(() => { if (widget) api.remove(widget); finish(undefined, new Error("Turnstile 验证超时，请重试")); }, 120000);
    signal.addEventListener("abort", abort, { once: true });
    active.set(host, abort);
    try {
      widget = api.render(host, { sitekey, action: purpose, language: "zh-CN", callback: (token) => finish(token), "error-callback": () => { expired(); finish(); }, "expired-callback": () => { expired(); finish(undefined, new Error("Turnstile 验证已过期，请重试")); } });
    } catch (e) { signal.removeEventListener("abort", abort); finish(undefined, e instanceof Error ? e : new Error("Turnstile 无法显示，请重试")); }
  });
}
