import { useEffect } from "react";
import { useQuery } from "../lib/query";

// src 带上内容摘要（中心返回的 ETag）：换图后地址随之改变，页面上的图片与网页图标立即更新。
export function useLogo() {
  return useQuery({ queryKey: ["logo"], queryFn: async () => {
    const res = await fetch("/api/v1/logo", { credentials: "same-origin", cache: "no-cache" });
    if (res.status === 404) return { src: "", configured: false };
    if (!res.ok) throw new Error("Logo 加载失败");
    const version = (res.headers.get("ETag") ?? "").replace(/"/g, "").slice(0, 16) || String(Date.now());
    return { src: `/api/v1/logo?v=${version}`, configured: true };
  } });
}

// 没有设置 Logo 时的网页图标，与 BrandMark 的占位一致。
const defaultIcon = "data:image/svg+xml," + encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 32 32"><rect width="32" height="32" rx="9" fill="#1c1c1e"/><text x="16" y="22" font-family="system-ui,sans-serif" font-size="17" font-weight="800" text-anchor="middle" fill="#fff">W</text></svg>');

// Favicon 挂在应用根部：标签页图标跟随设置里的 Logo，读取失败时保留默认图标。
export function Favicon() {
  const logo = useLogo();
  const href = logo.data?.src || defaultIcon;
  useEffect(() => {
    let link = document.querySelector<HTMLLinkElement>('link[rel="icon"]');
    if (!link) {
      link = document.createElement("link");
      link.rel = "icon";
      document.head.appendChild(link);
    }
    link.href = href;
  }, [href]);
  return null;
}

export function BrandMark({ className }: { className?: string }) {
  const logo = useLogo();
  return <span className={className ?? "block size-8"} aria-label="Weir">
    {logo.data?.src ? <img src={logo.data.src} alt="" className="block size-full object-contain" /> : <span className="grid size-full place-items-center rounded-[var(--weir-radius-9)] bg-[var(--weir-pill)] text-body font-extrabold text-[var(--weir-pill-ink)] [corner-shape:squircle]">W</span>}
  </span>;
}
