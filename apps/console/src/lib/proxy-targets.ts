import type { ProxyView } from "./core";
export function proxyTargets(_view: ProxyView): [string, string][] { return [["proxy", "走 PROXY"], ["direct", "直连"]]; }
export function proxyURL(base: string, path = ""): string {
  const user = /\/admin\/users\/(\d+)/.exec(base)?.[1];
  return "/proxy" + path + (user ? "?user=" + user : "");
}
