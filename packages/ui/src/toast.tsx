// Weir 轻提示：操作结果的短暂反馈。错误与需要核对的结果仍在页面内常驻显示。
import { Toast } from "@base-ui/react/toast";
import { XIcon } from "lucide-react";
import type { ReactNode } from "react";
import { StatusLamp, type LampTone } from "./lamp";
import { CheckMark } from "./check-mark";

function ToastViewport() {
  const { toasts } = Toast.useToastManager();
  return (
    <Toast.Portal>
      <Toast.Viewport className="fixed right-4 bottom-4 z-[60] flex w-[min(360px,calc(100vw-32px))] flex-col gap-2 outline-none">
        {toasts.map((toast) => (
          <Toast.Root
            key={toast.id}
            toast={toast}
            className="mg-panel flex items-start gap-3 px-4 py-3 transition-[opacity,transform] duration-[var(--mg-mid)] data-[starting-style]:translate-y-2 data-[starting-style]:opacity-0 data-[ending-style]:opacity-0"
          >
            {/* 成功提示：圈和对勾依次画出；其他语气仍用指示灯 */}
            {toast.type === "ok" ? (
              <CheckMark circle className="mt-px size-4 shrink-0 text-[var(--mg-ok)]" />
            ) : (
              <StatusLamp tone={(toast.type as LampTone) || "info"} className="mt-1.5" />
            )}
            <div className="min-w-0 flex-1">
              <Toast.Title className="text-[13px] font-semibold text-[var(--mg-ink)]" />
              <Toast.Description className="mt-0.5 text-xs leading-relaxed text-[var(--mg-ink-3)]" />
            </div>
            <Toast.Close aria-label="关闭提示" className="rounded p-1 text-[var(--mg-ink-3)] hover:bg-[var(--mg-well)]">
              <XIcon className="size-3.5" />
            </Toast.Close>
          </Toast.Root>
        ))}
      </Toast.Viewport>
    </Toast.Portal>
  );
}

function ToastProvider({ children }: { children: ReactNode }) {
  return (
    <Toast.Provider timeout={4000} limit={3}>
      {children}
      <ToastViewport />
    </Toast.Provider>
  );
}

function useToast() {
  const manager = Toast.useToastManager();
  return (title: string, options: { description?: string; tone?: LampTone } = {}) =>
    manager.add({ title, description: options.description, type: options.tone ?? "ok" });
}

export { ToastProvider, useToast };
