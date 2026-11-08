import { cloneElement, type ReactElement, type ReactNode } from "react";
import { cn } from "cn";

// 页面内文字链接不依赖路由；调用方通过 render 传入路由链接或普通 a 元素。
function TextLink({ render, children, className }: { render: ReactElement<{ className?: string; children?: ReactNode }>; children?: ReactNode; className?: string }) {
  return cloneElement(render, { className: cn("weir-hit relative text-[var(--weir-accent-text)] underline-offset-4 transition-opacity hover:underline active:opacity-60", className, render.props.className), children: children ?? render.props.children });
}

export { TextLink };
