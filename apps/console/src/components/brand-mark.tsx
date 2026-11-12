import { useQuery } from "../lib/query";

export function useLogo() {
  return useQuery({ queryKey: ["logo"], queryFn: async () => {
    const res = await fetch("/api/v1/logo", { credentials: "same-origin" });
    if (res.status === 404) return { src: "", configured: false };
    if (!res.ok) throw new Error("Logo 加载失败");
    return { src: "/api/v1/logo", configured: true };
  } });
}

export function BrandMark({ className }: { className?: string }) {
  const logo = useLogo();
  return <span className={className ?? "block size-8"} aria-label="Weir">
    {logo.data?.src ? <img src={logo.data.src} alt="" className="block size-full object-contain" /> : <span className="grid size-full place-items-center rounded-[var(--weir-radius-9)] bg-[var(--weir-pill)] text-body font-extrabold text-[var(--weir-pill-ink)] [corner-shape:squircle]">W</span>}
  </span>;
}
