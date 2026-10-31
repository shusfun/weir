// 控制台共用的 QueryClient：事件流与等待后台任务的请求在 React 之外也要读写同一份缓存。
import { QueryClient } from "@tanstack/react-query";

export const queryClient = new QueryClient({
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false, staleTime: 15000 },
  },
});
