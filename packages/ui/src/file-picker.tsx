// 选择文件：点按（键帽，按下有水波）或把文件拖进来。文件只在浏览器里交给调用方（读成文本或按原样上传由调用方决定），
// 不自动上传。accept 与 maxBytes 不符时调 onError，不调 onFile；错误交给外层 Field 显示在下方。
// 放在 Field 里时，Field 给的 id 落在按钮上：点标签聚焦按钮，读屏能读到标签与说明。
import { useRef, useState, type ComponentProps, type ReactNode } from "react";
import { FileUp } from "lucide-react";
import { cn } from "cn";
import { Ripple } from "./ripple";

function matches(file: File, accept?: string) {
  if (!accept) return true;
  const name = file.name.toLowerCase();
  return accept.split(",").map((a) => a.trim().toLowerCase()).some((a) => {
    if (!a) return false;
    if (a.startsWith(".")) return name.endsWith(a);
    if (a.endsWith("/*")) return file.type.startsWith(a.slice(0, -1));
    return file.type === a;
  });
}

function sizeText(n: number) {
  return n >= 1024 * 1024 ? `${Math.round(n / 1024 / 1024)} MiB` : `${Math.round(n / 1024)} KiB`;
}

function FilePicker({
  id,
  accept,
  maxBytes,
  onFile,
  onError,
  disabled,
  // anyType：accept 只用于文件对话框的默认筛选，不拦截别的类型（如没有扩展名的私钥文件）
  anyType = false,
  children = "选择文件，或拖到这里",
  className,
  ...props
}: Omit<ComponentProps<"button">, "onError" | "children"> & {
  accept?: string;
  maxBytes?: number;
  onFile: (file: File) => void;
  onError?: (message: string) => void;
  anyType?: boolean;
  children?: ReactNode;
}) {
  const input = useRef<HTMLInputElement>(null);
  const [over, setOver] = useState(false);
  const [picked, setPicked] = useState("");
  const take = (file?: File | null) => {
    if (!file) return;
    if (!anyType && !matches(file, accept)) return onError?.(`不支持这种文件（${file.name}）`);
    if (maxBytes && file.size > maxBytes) return onError?.(`文件太大：${sizeText(file.size)}，最多 ${sizeText(maxBytes)}`);
    setPicked(file.name);
    onFile(file);
  };
  return (
    <>
      <button
        type="button"
        id={id}
        data-slot="file-picker"
        data-over={over || undefined}
        disabled={disabled}
        className={cn(
          "weir-key weir-key-ghost relative flex min-h-11 w-full min-w-0 items-center gap-2 rounded-[var(--weir-radius)] border border-dashed border-[var(--weir-line)] px-3 text-left text-[12.5px] text-[var(--weir-ink-2)] disabled:pointer-events-none disabled:opacity-50 data-[over]:border-[var(--weir-accent-text)] data-[over]:bg-[var(--weir-accent-soft)] [&_svg]:size-4 [&_svg]:shrink-0",
          className,
        )}
        onClick={() => input.current?.click()}
        onDragOver={(e) => {
          e.preventDefault();
          setOver(true);
        }}
        onDragLeave={() => setOver(false)}
        onDrop={(e) => {
          e.preventDefault();
          setOver(false);
          take(e.dataTransfer.files?.[0]);
        }}
        {...props}
      >
        <FileUp />
        <span className="min-w-0 flex-1 truncate">{children}</span>
        {picked && <small className="max-w-[45%] shrink-0 truncate text-[var(--weir-ink-3)]" title={picked}>{picked}</small>}
        <Ripple />
      </button>
      <input
        ref={input}
        type="file"
        accept={accept}
        tabIndex={-1}
        aria-hidden
        className="hidden"
        onChange={(e) => {
          take(e.target.files?.[0]);
          e.target.value = "";
        }}
      />
    </>
  );
}

export { FilePicker };
