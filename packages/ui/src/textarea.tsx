// 来源 codedthemes/uiable textarea；本地改为与 Input 相同的填充底。
// mono：节点链接、PEM、订阅输出等原文内容，等宽且不自动折行。
import { ComponentProps } from "react";
import { cn } from "cn";
import { fieldClass } from "./input";

function Textarea({ className, mono = false, ...props }: ComponentProps<"textarea"> & { mono?: boolean }) {
  return (
    <textarea
      data-slot="textarea"
      className={cn(fieldClass, "field-sizing-content min-h-22 py-2", mono && "font-mono text-meta whitespace-pre", className)}
      {...props}
    />
  );
}

export { Textarea };
