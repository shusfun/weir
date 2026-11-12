import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

// theme.css 里新增的字号类（text-body、text-caption1 等）要登记给 tailwind-merge：
// 否则它们会被当成文字颜色，和 text-[var(--weir-ink)] 互相覆盖、被合并掉。
const twMerge = extendTailwindMerge({
  extend: {
    theme: {
      text: [
        "body", "meta", "fine", "lead", "name", "control", "heading", "dialog", "title", "display", "hero", "detail",
        "large-title", "title1", "title2", "title3", "headline", "callout", "subheadline", "footnote", "caption1", "caption2",
      ],
    },
  },
});

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs));
}
