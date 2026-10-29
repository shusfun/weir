// dsh 抽屉的页面感知：打开过抽屉的那个浏览器标签把合并后的上下文报给中心（页面切换、弹窗开关、向导换步、
// 选中对象变化立即报，表单停止输入 1 秒后报；连续变化合并为一次），标签切到后台时标记「不在前台」。
// 抽屉开着时轮询 dsh 的界面导航：只作用于这个标签，跳转后底部提示「由 dsh 打开」。
import { useEffect, useRef } from "react";
import { useLocation, useNavigate, useSearchParams } from "react-router-dom";
import { useToast } from "@weir/ui";
import { api } from "../api";
import { drawerId, snapshot, subscribeContext } from "../lib/console-context";
import { currentNav, systemSections } from "../shell/navigation";
import { objectHref, type ObjectRef } from "../lib/objects";

const closedTTL = 30 * 60 * 1000;

export function DshContextReporter({ open }: { open: boolean }) {
  const location = useLocation();
  const [params] = useSearchParams();
  const navigate = useNavigate();
  const toast = useToast();
  const closedAt = useRef<number | null>(null);
  const timer = useRef<number | undefined>(undefined);
  const where = useRef({ path: location.pathname, tab: params.get("tab") ?? "" });
  where.current = { path: location.pathname, tab: params.get("tab") ?? "" };

  useEffect(() => {
    closedAt.current = open ? null : closedAt.current ?? Date.now();
  }, [open]);

  useEffect(() => {
    const send = () => {
      const id = drawerId();
      if (!id || (closedAt.current !== null && Date.now() - closedAt.current > closedTTL)) return;
      const s = snapshot();
      const nav = currentNav(where.current.path);
      const tab = where.current.path === "/system" ? systemSections.find((x) => x.key === where.current.tab)?.label : where.current.tab;
      const body = { drawer: id, ...s, page: s.page ?? nav?.label ?? where.current.path, tab: s.tab ?? tab ?? "", visible: document.visibilityState === "visible", drawer_open: closedAt.current === null };
      void api("/admin/dsh/context", "POST", body).catch(() => undefined);
    };
    const schedule = (delay: number) => {
      window.clearTimeout(timer.current);
      timer.current = window.setTimeout(send, delay);
    };
    const off = subscribeContext((c) => schedule(c === "form" ? 1000 : 250));
    const onVisible = () => schedule(0);
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("focus", onVisible);
    const beat = window.setInterval(send, 60_000);
    schedule(0);
    return () => {
      off();
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("focus", onVisible);
      window.clearInterval(beat);
      window.clearTimeout(timer.current);
    };
  }, [open]);

  // 换页也算结构变化。
  useEffect(() => {
    window.dispatchEvent(new Event("focus"));
  }, [location.pathname, location.search]);

  useEffect(() => {
    if (!open) return;
    const poll = window.setInterval(async () => {
      const id = drawerId();
      if (!id || document.visibilityState !== "visible") return;
      // 给了对象就按对象类型的固定地址打开（lib/objects.ts）；只给页面时打开页面与标签
      const nav = (await api("/admin/dsh/navigation?drawer=" + id).catch(() => null)) as { path?: string; tab?: string; object?: ObjectRef } | null;
      if (!nav || (!nav.path && !nav.object)) return;
      if (nav.object) navigate(objectHref(nav.object));
      else navigate(nav.path + (nav.tab ? "?tab=" + encodeURIComponent(nav.tab) : ""));
      toast("由 dsh 打开", { tone: "info" });
    }, 3000);
    return () => window.clearInterval(poll);
  }, [open, navigate, toast]);
  return null;
}
