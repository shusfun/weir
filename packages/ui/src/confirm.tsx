// 确认框 / 输入框：替代 window.confirm / window.prompt，样式与键盘行为同 Dialog（窄屏为底部抽屉）。
// 用法：const confirm = useConfirm(); if (!(await confirm({ title: "删除节点？", tone: "danger" }))) return;
//       const prompt = usePrompt(); const name = await prompt({ title: "重命名", defaultValue }); // 取消返回 null
// 需要在应用根部包一层 ConfirmProvider。
import { createContext, useCallback, useContext, useRef, useState, type ReactNode } from "react";
import { Button } from "./button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle } from "./dialog";
import { Field } from "./field";
import { Form } from "./form";
import { Input } from "./input";

type ConfirmOptions = {
  title: ReactNode;
  description?: ReactNode;
  confirmText?: string;
  cancelText?: string;
  // danger：确认按钮为红色（删除、撤销、换凭据等不可撤回的操作）
  tone?: "default" | "danger";
};
type PromptOptions = Omit<ConfirmOptions, "tone"> & {
  label?: string;
  defaultValue?: string;
  placeholder?: string;
  required?: boolean;
  maxLength?: number;
};
type Request =
  | { kind: "confirm"; options: ConfirmOptions; resolve: (v: boolean) => void }
  | { kind: "prompt"; options: PromptOptions; resolve: (v: string | null) => void };

const ConfirmContext = createContext<((r: Request) => void) | null>(null);

function ConfirmProvider({ children }: { children: ReactNode }) {
  const [request, setRequest] = useState<Request | null>(null);
  const [open, setOpen] = useState(false);
  const [value, setValue] = useState("");
  const settled = useRef(true);
  const pending = useRef<Request | null>(null);
  const ask = useCallback((r: Request) => {
    // 上一个还没答复就来了新的：按取消处理，避免调用方一直等待
    const prev = pending.current;
    if (prev && !settled.current) prev.kind === "confirm" ? prev.resolve(false) : prev.resolve(null);
    pending.current = r;
    settled.current = false;
    setValue(r.kind === "prompt" ? r.options.defaultValue ?? "" : "");
    setRequest(r);
    setOpen(true);
  }, []);
  const finish = (ok: boolean) => {
    if (!request || settled.current) return;
    settled.current = true;
    if (request.kind === "confirm") request.resolve(ok);
    else request.resolve(ok ? value : null);
    setOpen(false);
  };
  const o = request?.options;
  const danger = request?.kind === "confirm" && request.options.tone === "danger";
  return (
    <ConfirmContext.Provider value={ask}>
      {children}
      <Dialog open={open} onOpenChange={(v) => !v && finish(false)} onOpenChangeComplete={(v) => !v && setRequest(null)}>
        {request && o && (
          <DialogContent role="alertdialog" size="sm" layer="top">
            <DialogTitle>{o.title}</DialogTitle>
            {o.description && <DialogDescription>{o.description}</DialogDescription>}
            <Form onSubmit={() => finish(true)}>
              {request.kind === "prompt" && (
                <Field label={request.options.label} htmlFor="weir-prompt-input">
                  <Input
                    id="weir-prompt-input"
                    autoFocus
                    aria-label={request.options.label ? undefined : typeof o.title === "string" ? o.title : "输入"}
                    value={value}
                    placeholder={request.options.placeholder}
                    required={request.options.required}
                    maxLength={request.options.maxLength}
                    onChange={(e) => setValue(e.target.value)}
                  />
                </Field>
              )}
              <DialogFooter>
                <Button type="button" variant="outline" onClick={() => finish(false)}>{o.cancelText ?? "取消"}</Button>
                <Button type="submit" variant={danger ? "destructive" : "default"} autoFocus={request.kind === "confirm"}>{o.confirmText ?? "确定"}</Button>
              </DialogFooter>
            </Form>
          </DialogContent>
        )}
      </Dialog>
    </ConfirmContext.Provider>
  );
}

function useAsk() {
  const ask = useContext(ConfirmContext);
  if (!ask) throw new Error("useConfirm / usePrompt 需要放在 ConfirmProvider 内");
  return ask;
}

function useConfirm() {
  const ask = useAsk();
  return useCallback(
    (options: ConfirmOptions | string) =>
      new Promise<boolean>((resolve) => ask({ kind: "confirm", options: typeof options === "string" ? { title: options } : options, resolve })),
    [ask],
  );
}

function usePrompt() {
  const ask = useAsk();
  return useCallback((options: PromptOptions) => new Promise<string | null>((resolve) => ask({ kind: "prompt", options, resolve })), [ask]);
}

export { ConfirmProvider, useConfirm, usePrompt, type ConfirmOptions, type PromptOptions };
