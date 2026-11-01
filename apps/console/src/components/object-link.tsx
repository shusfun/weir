// 对象链接与地址参数：凡是显示对象名的地方都用 ObjectLink，点了去对象所在的页面，浏览器后退能回来。
// 地址约定见 lib/objects.ts；链接样式与地址栏详情窗的规则在 @weir/ui。FocusFromURL 常驻在控制台里，处理 ?focus=：等列表行出现后滚过去并高亮。
import { useEffect, type ReactNode } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { ObjectLink as UIObjectLink, useDialogParam as useUIDialogParam, useStringDialogParam as useUIStringDialogParam, useToast } from "@weir/ui";
import { focusValue, objectHref, objectLabels, objectKey, type ObjectRef } from "../lib/objects";

export function ObjectLink({ to, children, className }: { to: ObjectRef; children?: ReactNode; className?: string }) {
  const href = objectHref(to);
  return (
    <UIObjectLink href={href} kind={objectLabels[to.type]} name={to.name ?? objectKey(to)} className={className} render={<Link to={href} />}>
      {children ?? to.name ?? `${objectLabels[to.type]} ${objectKey(to)}`}
    </UIObjectLink>
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
        el.classList.add("weir-flash");
        window.setTimeout(() => el.classList.remove("weir-flash"), 2200);
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

// 详情窗记在地址栏（?gate=3、?driver=xray-26.3）：规则在 @weir/ui，这里接上路由的地址参数。
export function useDialogParam<T extends { id: number }>(name: string, items: T[] | undefined, ready: boolean) {
  return useUIDialogParam(useSearchParams(), name, items, ready);
}

export function useStringDialogParam(name: string) {
  return useUIStringDialogParam(useSearchParams(), name);
}
