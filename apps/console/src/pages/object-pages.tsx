// 保留旧对象地址；详情和标签统一映射到集合页的地址弹窗。
import { Navigate, useParams, useSearchParams } from "react-router-dom";

function ObjectRedirect({ path, param, section }: { path: string; param: string; section?: string }) {
  const { id = "" } = useParams();
  const [search] = useSearchParams();
  const next = new URLSearchParams(search);
  const oldTab = next.get("tab");
  if (oldTab) next.set("detail_tab", oldTab);
  next.delete("tab");
  if (section) next.set("tab", section);
  next.set(param, id);
  return <Navigate to={`${path}?${next}`} replace />;
}

export function GateDetailPage() { return <ObjectRedirect path="/gates" param="gate" />; }
export function NodeDetailPage() { return <ObjectRedirect path="/gates" param="node" />; }
export function UserDetailPage() { return <ObjectRedirect path="/users" param="user" />; }
export function SourceDetailPage() { return <ObjectRedirect path="/subscriptions" param="source" section="sources" />; }
export function ProfileDetailPage() { return <ObjectRedirect path="/subscriptions" param="profile_detail" section="profiles" />; }
export function PublicPageDetail() { return <ObjectRedirect path="/pages" param="page" section="pages" />; }
