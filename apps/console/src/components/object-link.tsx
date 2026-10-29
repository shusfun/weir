// 对象链接与地址参数：凡是显示对象名的地方都用 ObjectLink，点了去对象所在的页面，浏览器后退能回来。
// 地址约定见 lib/objects.ts。FocusFromURL 常驻在控制台里，处理 ?focus=：等列表行出现后滚过去并高亮。
import { useEffect, useRef, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useToast } from "@weir/ui";
import { focusValue, objectHref, objectLabels, objectKey, type ObjectRef } from "../lib/objects";

export function ObjectLink({ to, children, className }: { to: ObjectRef; children?: ReactNode; className?: string }) {
  const label = children ?? to.name ?? `${objectLabels[to.type]} ${objectKey(to)}`;
  return (
    <Link
      to={objectHref(to)}
      title={`打开${objectLabels[to.type]}「${to.name ?? objectKey(to)}」`}
      className={"text-[var(--mg-accent-text)] underline-offset-4 hover:underline " + (className ?? "")}
    >
      {label}
    </Link>
  );
}

// dataObject 给列表行加上可被 ?focus= 找到的标记。
export function dataObject(ref: ObjectRef) {
  return { "data-object": focusValue(ref) };
}

// FocusFromURL：?focus=类型:编号 时等这一行渲染出来（数据加载、标签切换、弹窗打开都要时间，最多等 6 秒），
// 滚到中间并高亮约 2 秒，然后把参数去掉；等不到就如实提示，不停在半路。
export function FocusFromURL() {
  const [params, setParams] = useSearchParams();
  const toast = useToast();
  const focus = params.get("focus");
  useEffect(() => {
    if (!focus) return;
    let tries = 0;
    const clear = () =>
      setParams((p) => {
        p.delete("focus");
        return p;
      }, { replace: true });
    const timer = window.setInterval(() => {
      const el = document.querySelector<HTMLElement>(`[data-object="${CSS.escape(focus)}"]`);
      if (el) {
        window.clearInterval(timer);
        el.scrollIntoView({ block: "center", behavior: "smooth" });
        el.classList.add("mg-flash");
        window.setTimeout(() => el.classList.remove("mg-flash"), 2200);
        clear();
      } else if (++tries > 30) {
        window.clearInterval(timer);
        toast("没有找到要打开的对象", { tone: "warn", description: "它可能已被删除，或不在当前的筛选结果里。" });
        clear();
      }
    }, 200);
    return () => window.clearInterval(timer);
  }, [focus, setParams, toast]);
  return null;
}

// useDialogParam：详情窗的开关记在地址栏（?gate=3）。打开时追加一条历史，关闭时替换掉参数；
// 后退回到打开前的页面，刷新或从别处链过来也能直接打开。ready 后参数指向的对象不存在时提示并去掉参数。
export function useDialogParam<T extends { id: number }>(name: string, items: T[] | undefined, ready: boolean) {
  const [params, setParams] = useSearchParams();
  const toast = useToast();
  const raw = params.get(name);
  const id = raw ? Number(raw) : null;
  const current = id ? items?.find((x) => x.id === id) ?? null : null;
  const reported = useRef<number | null>(null);
  useEffect(() => {
    if (id && ready && !current && reported.current !== id) {
      reported.current = id;
      toast("没有找到要打开的对象", { tone: "warn", description: "它可能已被删除。" });
      setParams((p) => {
        p.delete(name);
        return p;
      }, { replace: true });
    }
  }, [id, ready, current, name, setParams, toast]);
  const open = (v: T | number) =>
    setParams((p) => {
      p.set(name, String(typeof v === "number" ? v : v.id));
      return p;
    });
  const close = () =>
    setParams((p) => {
      p.delete(name);
      return p;
    }, { replace: true });
  return { current, open, close };
}

// useStringDialogParam：按名称打开的详情窗（如 ?driver=xray-26.3），规则同 useDialogParam。
export function useStringDialogParam(name: string) {
  const [params, setParams] = useSearchParams();
  const value = params.get(name);
  const open = (v: string) =>
    setParams((p) => {
      p.set(name, v);
      return p;
    });
  const close = () =>
    setParams((p) => {
      p.delete(name);
      return p;
    }, { replace: true });
  return { value, open, close };
}
