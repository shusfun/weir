// 头像首字母块（设计稿 .v-av）：没有图片，只取名字第一个字的大写。
import { cn } from "cn";

function Avatar({ name, size = "md", className }: { name: string; size?: "md" | "lg"; className?: string }) {
  const first = Array.from(name.trim())[0]?.toUpperCase() ?? "?";
  return (
    <span data-slot="avatar" data-size={size} className={cn("weir-avatar", className)} aria-hidden>
      {first}
    </span>
  );
}

export { Avatar };
