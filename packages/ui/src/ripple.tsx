// 水波：点在哪，波就从哪散开——反馈跟着手指走，不跟着控件走。
// 用法：把 <Ripple /> 放进任意已定位（relative / absolute）的可点元素里，它监听父元素的按下事件。
// 波纹画在内部的裁剪层里，不裁掉键帽的外阴影；键盘 Enter / 空格从中心散开。
// 按在内部的其他按钮、链接、输入框上时不触发，避免卡片和里面的按钮同时起波。
// 减少动态效果时不插入任何元素。
import { useEffect, useRef } from "react";
import { prefersReducedMotion } from "./motion";

const nested = "button, a[href], input, select, textarea, [role=button], [role=switch], [role=tab], [role=menuitem]";
const maxWaves = 4;

function Ripple() {
  const ref = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    const layer = ref.current;
    const host = layer?.parentElement;
    if (!layer || !host) return;

    const disabled = () => host.matches(":disabled, [aria-disabled=true], [data-disabled]");
    // 按下时键帽有 0.97 缩放：用 offset 尺寸与外框比例换算回元素自身坐标
    const spawn = (clientX?: number, clientY?: number) => {
      if (prefersReducedMotion() || disabled()) return;
      const rect = host.getBoundingClientRect();
      const w = host.offsetWidth;
      const h = host.offsetHeight;
      if (!w || !h) return;
      const x = clientX === undefined ? w / 2 : ((clientX - rect.left) * w) / rect.width;
      const y = clientY === undefined ? h / 2 : ((clientY - rect.top) * h) / rect.height;
      // 半径取到最远角的距离，波纹正好铺满整块
      const r = Math.hypot(Math.max(x, w - x), Math.max(y, h - y));
      const wave = document.createElement("span");
      wave.className = "mg-ripple";
      wave.style.cssText = `left:${x - r}px;top:${y - r}px;width:${r * 2}px;height:${r * 2}px`;
      wave.addEventListener("animationend", () => wave.remove(), { once: true });
      while (layer.childElementCount >= maxWaves) layer.firstElementChild?.remove();
      layer.appendChild(wave);
    };
    const onPointer = (e: PointerEvent) => {
      if (e.button !== 0) return;
      const inner = (e.target as Element | null)?.closest(nested);
      if (inner && inner !== host && host.contains(inner)) return;
      spawn(e.clientX, e.clientY);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.target === host && !e.repeat && (e.key === "Enter" || e.key === " ")) spawn();
    };
    host.addEventListener("pointerdown", onPointer);
    host.addEventListener("keydown", onKey);
    return () => {
      host.removeEventListener("pointerdown", onPointer);
      host.removeEventListener("keydown", onKey);
    };
  }, []);
  return <span ref={ref} aria-hidden data-slot="ripple" className="mg-ripple-host" />;
}

export { Ripple };
