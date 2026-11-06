import { useEffect } from "react";
import { useQuery } from "../lib/query";
import { api } from "../api";

export function useLogo() {
  return useQuery({ queryKey: ["logo"], queryFn: () => api("/logo") as Promise<{ data: string }> });
}

export function BrandMark({ className }: { className?: string }) {
  const logo = useLogo();
  useEffect(() => {
    document.querySelector('link[rel="icon"]')?.remove();
    if (logo.data?.data) {
      const icon = document.createElement("link");
      icon.rel = "icon";
      icon.href = logo.data.data;
      document.head.appendChild(icon);
    }
  }, [logo.data?.data]);
  return logo.data?.data ? <span className={className}><img src={logo.data.data} alt="" className="block size-full object-contain" /></span> : null;
}
