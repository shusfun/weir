// 上移 / 下移一对按钮：列表排序统一的图标、尺寸与读屏文案（“上移 {label}” / “下移 {label}”）。
// 是否可移动由调用方判断（首尾、忙碌、固定在末尾的规则等），这里只负责呈现。
import { ArrowDown, ArrowUp } from "lucide-react";
import { Button } from "./button";

function ReorderButtons({
  label,
  onUp,
  onDown,
  upDisabled = false,
  downDisabled = false,
}: {
  label: string;
  onUp: () => void;
  onDown: () => void;
  upDisabled?: boolean;
  downDisabled?: boolean;
}) {
  return (
    <div data-slot="reorder-buttons" role="group" aria-label={"调整 " + label + " 顺序"} className="inline-flex shrink-0 items-center gap-1">
      <Button type="button" variant="ghost" size="icon-sm" aria-label={"上移 " + label} disabled={upDisabled} onClick={onUp}>
        <ArrowUp />
      </Button>
      <Button type="button" variant="ghost" size="icon-sm" aria-label={"下移 " + label} disabled={downDisabled} onClick={onDown}>
        <ArrowDown />
      </Button>
    </div>
  );
}

export { ReorderButtons };
