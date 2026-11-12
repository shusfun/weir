import { StrictMode, useEffect, useRef } from "react";
import { createRoot } from "react-dom/client";
// 设计稿字体（SIL OFL），随程序本地打包，不走外部 CDN
import "@fontsource-variable/inter";
import "@fontsource-variable/jetbrains-mono";
import { createBrowserRouter, RouterProvider, useBlocker } from "react-router-dom";
import { QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider, PageScrollArea, ToastProvider, useConfirm } from "@weir/ui";
import { QueryStatus } from "./components/query-status";
import Application from "./app";
import { queryClient } from "./lib/query";
import { watchSystemTheme } from "./theme";
import { bootAppearance } from "./appearance";
import { hasUnsavedChanges } from "./lib/dirty";
function UnsavedGuard() {
  const confirm = useConfirm();
  const blocker = useBlocker(() => hasUnsavedChanges());
  const asking = useRef(false);
  useEffect(() => {
    if (blocker.state !== "blocked" || asking.current) return;
    asking.current = true;
    void confirm({title:"放弃未保存修改？",description:"离开会丢失当前表单修改。",confirmText:"放弃修改",tone:"danger"}).then((ok) => {
      if (ok) blocker.proceed(); else blocker.reset();
    }).finally(() => { asking.current = false; });
  }, [blocker, confirm]);
  return null;
}
const router = createBrowserRouter([{path:"*",element:<><UnsavedGuard /><PageScrollArea viewportProps={{tabIndex:-1}}><QueryStatus /><Application /></PageScrollArea></>}]);
watchSystemTheme();
bootAppearance();
createRoot(document.getElementById("root")!).render(<StrictMode><QueryClientProvider client={queryClient}><ToastProvider><ConfirmProvider><RouterProvider router={router} /></ConfirmProvider></ToastProvider></QueryClientProvider></StrictMode>);
