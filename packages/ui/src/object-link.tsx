// 对象链接与地址参数：凡是显示对象名的地方都用 ObjectLink，点了去对象所在的页面，浏览器后退能回来。
// 组件库不依赖路由：地址由调用方算好传入 href；用前端路由时经 render 换成路由的链接（render={<Link to={href} />}）。
// 详情窗记在地址栏（useDialogParam）：调用方把路由的地址参数读写（如 react-router 的 useSearchParams()）传进来。
import { useEffect, useRef } from "react";
import { mergeProps } from "@base-ui/react/merge-props";
import { useRender } from "@base-ui/react/use-render";
import { cn } from "cn";
import { useToast } from "./toast";

// ObjectLink：kind 是对象类型的中文名（如「VPS」），name 是对象名；children 不传时显示 name。
function ObjectLink({
  href,
  kind,
  name,
  children,
  className,
  render,
  ...props
}: useRender.ComponentProps<"a"> & { href: string; kind: string; name: string }) {
  return useRender({
    defaultTagName: "a",
    render,
    props: mergeProps<"a">(
      {
        href,
        title: `打开${kind}「${name}」`,
        className: cn("text-[var(--weir-accent-text)] underline-offset-4 hover:underline", className),
        children: children ?? name,
      },
      props,
    ),
  });
}

// 地址参数的读写：与 react-router 的 useSearchParams() 返回值同形，可以直接传入。
type SearchParamsAccess = readonly [
  URLSearchParams,
  (next: (prev: URLSearchParams) => URLSearchParams, options?: { replace?: boolean }) => void,
];

// useDialogParam：详情窗的开关记在地址栏（?gate=3）。打开时追加一条历史，关闭时替换掉参数；
// 后退回到打开前的页面，刷新或从别处链过来也能直接打开。ready 后参数指向的对象不存在时提示并去掉参数。
function useDialogParam<T extends { id: number }>(search: SearchParamsAccess, name: string, items: T[] | undefined, ready: boolean) {
  const [params, setParams] = search;
  const toast = useToast();
  const raw = params.get(name);
  const id = raw ? Number(raw) : null;
  const current = id ? (items?.find((x) => x.id === id) ?? null) : null;
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
function useStringDialogParam(search: SearchParamsAccess, name: string) {
  const [params, setParams] = search;
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

export { ObjectLink, useDialogParam, useStringDialogParam };
export type { SearchParamsAccess };
