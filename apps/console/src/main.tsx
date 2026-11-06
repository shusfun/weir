import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router-dom";
import { QueryClientProvider } from "@tanstack/react-query";
import { ConfirmProvider, PageScrollArea, ToastProvider } from "@weir/ui";
import { QueryStatus } from "./components/query-status";
import Application from "./app";
import { queryClient } from "./lib/query";
import { watchSystemTheme } from "./theme";
watchSystemTheme();
createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ToastProvider>
        <ConfirmProvider>
          <BrowserRouter>
            {/* 整页滚动区：页面在这里滚动（覆盖式滚动条），不再由 body 滚动；翻页键由它转给视口，视口不另占一个 Tab 位 */}
            <PageScrollArea viewportProps={{ tabIndex: -1 }}>
              <QueryStatus />
              <Application />
            </PageScrollArea>
          </BrowserRouter>
        </ConfirmProvider>
      </ToastProvider>
    </QueryClientProvider>
  </StrictMode>,
);
