// 旧“出站与路由”页已并入设备详情的出站 / 路由标签；保留此地址做重定向，书签不会失效。
import { Navigate, useSearchParams } from "react-router-dom";

export function RoutingRedirect() {
  const [params] = useSearchParams();
  const device = params.get("device");
  return <Navigate to={device && /^\d+$/.test(device) ? `/devices/${device}/routing` : "/devices"} replace />;
}
