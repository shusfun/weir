import {GuestbookManager} from "../components/guestbook";
import {PublicSubscriptionDirectory} from "../components/public-subscription-directory";
import {ProfilePublication} from "../components/profile-publication";
import {ProfileOrdering,ProfileOutput} from "../components/profile-workspace";
import {NodeGroupManager,readNodeGroups} from "../components/node-groups";
import { date } from "../lib/models";
import { useState, useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useSearchParams } from "react-router-dom";
import {
  Button,
  Input,
  Checkbox,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  Textarea,
  Select, SelectTrigger, SelectValue, SelectContent, SelectItem,
} from "@migate/ui";
import {
  Plus,
  Search,
  Upload,
  RefreshCw,
  ArrowLeft,
  ArrowRight,
  ArrowUp, ArrowDown,
  Layers,
  Link2,
  SlidersHorizontal,
  Code2,
  Globe,
  Mail,
} from "lucide-react";
import { api } from "../api";
import { client } from "../lib/client";
import type { Resource } from "../lib/models";
import { State, Status } from "../components/feedback";
import { ResourceEditor } from "../components/resource-editor";
import { NodeDedup } from "../components/node-dedup";
import { SourceUsagePanel } from "../components/source-usage";
import { InventoryImport } from "../components/inventory-import";
const kinds = [
  ["sources", "订阅源", Link2],
  ["nodes", "手动节点", Layers],
  ["profiles", "订阅组", Layers],
  ["operators", "处理链", SlidersHorizontal],
  ["templates", "输出模板", Code2],
  ["public-page", "公开页", Globe],
  ["mail", "邮件队列", Mail],
] as const;
export function InventoryPage() {
  const [params, SP] = useSearchParams();
  const kind = kinds.some((k) => k[0] === params.get("kind"))
    ? params.get("kind")!
    : "sources";
  const title = kinds.find((k) => k[0] === kind)![1];
  const cache = useQueryClient();
  const [search, S] = useState("");
  const [page, P] = useState(1);
  const [selected, SEL] = useState<number[]>([]);
  const [editor, ED] = useState<Resource | "new" | null>(null);
  const [importing, IM] = useState(false);
  const [dedup, DD] = useState(false);
  const [remove, RM] = useState<number[] | null>(null);
  const [busy, B] = useState(false);
  const [notice, N] = useState("");
  const [error, E] = useState("");
  const [preview, PV] = useState<string | null>(null);
 const [publicDirectory,PDIR]=useState<Resource|null>(null);
const [guestbook,GB]=useState<Resource|null>(null);
 const [publication,PUB]=useState<Resource|null>(null);
 const [profileOutput,PO]=useState<Resource|null>(null),[profileOrder,PS]=useState(false);
  const [status, F] = useState("all");
  const [protocol, PF] = useState("all");
  const [group, GF] = useState("all"), [groupDialog, GD] = useState(false), [destination, DG] = useState("");
  const q = useQuery({
    queryKey: ["inventory", kind],
    queryFn: () => client.resources(kind),
  });
  const groupsQuery=useQuery({queryKey:["node-groups"],queryFn:readNodeGroups,enabled:kind==="nodes"});
  const groupNames=groupsQuery.data?.groups.map(g=>g.name)??[...new Set((q.data?.items??[]).map(r=>String(r.data.group??""))).values()].filter(Boolean).sort();
  const [managingGroups,MG]=useState(false);
  const ordered = [...(q.data?.items ?? [])].sort((a,b) => ["nodes","profiles"].includes(kind) ? Number(a.data.position ?? -a.id)-Number(b.data.position ?? -b.id)||b.id-a.id : 0);
  const rows = ordered.filter(
    (r) =>
      String(r.data.name ?? r.key)
        .toLowerCase()
        .includes(search.toLowerCase()) &&
      (kind !== "nodes" || protocol === "all" || (r.node?.valid ? r.node.protocol : "invalid") === protocol) &&
      (kind !== "nodes" || group === "all" || (group === "ungrouped" ? !r.data.group : r.data.group === group.slice(2))) &&
      (status === "all" ||
        (r.data.enabled === true ? "enabled" : "disabled") === status),
  );
  const shown = rows.slice((page - 1) * 20, page * 20);
  useEffect(() => {
    S("");
    P(1);
    SEL([]);
    ED(null);
    PV(null);
    PO(null);PS(false);
    N("");
    E("");
    F("all");
    PF("all"); GF("all");GD(false);
  }, [kind]);
  useEffect(() => P(1), [search, status, protocol, group]);
  async function changed() {
    await cache.invalidateQueries({ queryKey: ["inventory", kind] });
    if(kind==="nodes")await cache.invalidateQueries({queryKey:["node-groups"]});
    await cache.invalidateQueries({ queryKey: ["selection", kind] });
    await cache.invalidateQueries({ queryKey: ["legacy"] });
    await cache.invalidateQueries({ queryKey: ["source-usage"] });
  }
  async function batch(action: string, ids: number[]) {
    B(true);
    E("");
    N("");
    const result = await Promise.allSettled(
      ids.map(async (id) => {
        if (action === "delete")
          return api("/admin/resources/" + kind + "/" + id, "DELETE");
        if (action === "refresh")
          return api("/admin/sources/" + id + "/refresh", "POST");
        const r = q.data?.items.find((r) => r.id === id);
        if (!r) throw Error("资源已不存在");
        return api("/admin/resources/" + kind, "POST", {
          id,
          data: { ...r.data, enabled: action === "enable" },
        });
      }),
    );
    const failed = ids.filter((_, i) => result[i].status === "rejected");
    N(
      "成功 " +
        (ids.length - failed.length) +
        " 项；失败 " +
        failed.length +
        " 项" +
        (failed.length
          ? "（#" + failed.join("、#") + "，已保留选择，可重试）"
          : ""),
    );
    SEL(failed);
    RM(null);
    await changed();
    B(false);
  }
  async function move(id:number,other:number){B(true);E("");N("");try{await api("/admin/nodes/order","POST",{id,other_id:other});await changed();N("节点显示顺序已保存；订阅组保持自己的顺序。")}catch(e){E(e instanceof Error?e.message:"排序失败")}finally{B(false)}}
  async function saveGroup(){B(true);E("");N("");try{const result=await api("/admin/nodes/group","POST",{ids:selected,group:destination});const failed=result.items.filter((r:{ok:boolean})=>!r.ok).map((r:{id:number})=>r.id);SEL(failed);await changed();GD(false);N(`分组成功 ${result.items.length-failed.length} 项；失败 ${failed.length} 项${failed.length?"，已保留选择供重试":""}`)}catch(e){E(e instanceof Error?e.message:"移动失败")}finally{B(false)}}
  async function inspect(r: Resource) {
 if(kind==="profiles"){PO(r);return}
    B(true);
    E("");
    PV(null);
    try {
      if (kind === "profiles") {
        const v = await api("/admin/profiles/preview", "POST", {
          id: r.id,
          target: "links",
        });
        const usage = v.upstream_usage;
        const summary = usage ? `上游自报用量（独立于用户额度）：计入 ${usage.included} 个，排除 ${usage.excluded} 个，未知 ${usage.unknown_sources} 个\n上传 ${usage.upload ?? "未知"} B / 下载 ${usage.download ?? "未知"} B / 总量 ${usage.total ?? "未知"} B / 剩余 ${usage.remaining ?? "未知"} B\n最早到期 ${usage.expire == null ? "未知" : usage.expire === "0" ? "未设到期" : date(Number(usage.expire))}\n\n` : "";
        PV(summary + (v.warnings ?? []).join("\n") + "\n" + (v.output || "没有节点"));
      } else if (kind === "nodes") {
        PV(String(r.data.url ?? ""));
      } else if (kind === "sources") {
        PV(
          "节点数：" +
            String(r.data.node_count ?? "未知") +
            "\n最后成功：" +
            String(r.data.last_update ?? "未知") +
            "\n当前状态：" +
            String(r.data.status ?? "未知"),
        );
      }
    } catch (e) {
      E(String(e));
    } finally {
      B(false);
    }
  }
  return (
    <div className="stack">
      <div className="page-intro">
        <div>
          <h2>订阅工作空间</h2>
          <p>组织订阅源、节点与处理链。用户订阅仍只发布有效的 Gate 授权。</p>
        </div>
        <div className="actions">
          {kind === "nodes" && (
            <Button variant="outline" onClick={() => IM(true)}>
              <Upload size={15} />
              导入节点
            </Button>
          )}
          {kind === "nodes" && <Button variant="outline" disabled={busy} onClick={()=>DD(true)}>清理重复</Button>}
          {kind !== "mail" && (
            <Button onClick={() => ED("new")}>
              <Plus size={15} />
              新增{title}
            </Button>
          )}
        </div>
      </div>
      <div className="inventory-tabs" role="tablist" aria-label="订阅工作空间">
        {kinds.map(([key, label, Icon]) => (
          <Button
            key={key}
            role="tab"
            aria-selected={kind === key}
            variant={kind === key ? "secondary" : "ghost"}
            onClick={() => SP({ kind: key })}
          >
            <Icon size={15} />
            {label}
          </Button>
        ))}
      </div>
      {kind === "sources" && <SourceUsagePanel />}
      {notice && (
        <div role="status" className="notice">
          {notice}
        </div>
      )}
      {error && (
        <div role="alert" className="notice error">
          {error}
        </div>
      )}
      <Card className="table-card">
        <div className="table-toolbar">
          <div className="search-field">
            <Search size={16} />
            <Input
              aria-label={"搜索" + title}
              placeholder={"搜索" + title + "…"}
              value={search}
              onChange={(e) => S(e.target.value)}
            />
          </div>
          {["sources", "profiles", "public-page"].includes(kind) && (
            <div className="filter-pills">
              {[
                ["all", "全部"],
                ["enabled", kind === "public-page" ? "已发布" : "已启用"],
                ["disabled", kind === "public-page" ? "未发布" : "已停用"],
              ].map(([v, n]) => (
                <Button
                  key={v}
                  size="sm"
                  variant={status === v ? "secondary" : "ghost"}
                  onClick={() => F(v)}
                >
                  {n}
                </Button>
              ))}
            </div>
          )}
          <span className="result-count">{rows.length} 条记录</span>
          {kind === "profiles"&&<Button variant="outline" onClick={()=>PS(true)}>调整订阅组顺序</Button>}
          {kind === "nodes"&&<Button variant="outline" onClick={()=>MG(true)}>管理节点分组</Button>}
          {kind === "nodes" && <Select value={group} onValueChange={v=>GF(String(v))}><SelectTrigger aria-label="筛选节点分组"><SelectValue>{group==="all"?"全部分组":group==="ungrouped"?"未分组":group.slice(2)}</SelectValue></SelectTrigger><SelectContent><SelectItem value="all">全部分组</SelectItem><SelectItem value="ungrouped">未分组</SelectItem>{groupNames.map(g=><SelectItem key={g} value={"g:"+g}>{g}</SelectItem>)}</SelectContent></Select>}
          {kind === "nodes" && <Select value={protocol} onValueChange={v => PF(String(v))}><SelectTrigger aria-label="筛选节点协议"><SelectValue>{protocol === "all" ? "全部协议" : protocol === "invalid" ? "解析失败" : protocol}</SelectValue></SelectTrigger><SelectContent><SelectItem value="all">全部协议</SelectItem>{[...new Set((q.data?.items??[]).map(r => r.node?.valid ? r.node.protocol! : "invalid"))].sort().map(value => <SelectItem key={value} value={value}>{value === "invalid" ? "解析失败" : value}</SelectItem>)}</SelectContent></Select>}
        </div>
        {kind !== "mail" && (
          <div className="inventory-bulk">
            <Checkbox
              aria-label="选择当前页"
              checked={
                shown.length > 0 && shown.every((r) => selected.includes(r.id))
              }
              onCheckedChange={(v) =>
                SEL(
                  v
                    ? [...new Set([...selected, ...shown.map((r) => r.id)])]
                    : selected.filter((id) => !shown.some((r) => r.id === id)),
                )
              }
            />
            <span>已选 {selected.length}</span>
            {selected.length > 0 && (
              <>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={busy}
                  onClick={() => RM(selected)}
                >
                  批量删除
                </Button>
                {kind === "nodes" && <Button size="sm" variant="outline" disabled={busy||selected.length>500} onClick={()=>{DG("");GD(true)}}>移动到分组</Button>}
                {kind === "sources" && (
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={busy}
                    onClick={() => batch("refresh", selected)}
                  >
                    刷新所选
                  </Button>
                )}
                {["sources", "profiles", "public-page"].includes(kind) && (
                  <>
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={busy}
                      onClick={() => batch("enable", selected)}
                    >
                      {kind === "public-page" ? "发布" : "启用"}
                    </Button>
                    <Button
                      size="sm"
                      variant="ghost"
                      disabled={busy}
                      onClick={() => batch("disable", selected)}
                    >
                      {kind === "public-page" ? "撤下" : "停用"}
                    </Button>
                  </>
                )}
                <Button size="sm" variant="ghost" onClick={() => SEL([])}>
                  取消选择
                </Button>
              </>
            )}
          </div>
        )}
        <CardContent>
          {q.isPending ? (
            <State loading />
          ) : q.error ? (
            <State error={q.error} onRetry={() => q.refetch()} />
          ) : !shown.length ? (
            <State empty={search ? "没有匹配的" + title : "暂无" + title} />
          ) : (
            <div className={kind==="profiles"?"inventory-items profile-cards":"inventory-items"}>
              {shown.map((r) => (
                <article className="inventory-item" key={r.id}>
                  <div className="inventory-item-main">
                    {kind !== "mail" && (
                      <Checkbox
                        aria-label={"选择 " + String(r.data.name ?? r.key)}
                        checked={selected.includes(r.id)}
                        onCheckedChange={(v) =>
                          SEL(
                            v
                              ? [...selected, r.id]
                              : selected.filter((id) => id !== r.id),
                          )
                        }
                      />
                    )}
                    <div>
                      <h3>{String(r.data.name ?? r.data.to ?? r.key)}</h3>
                      <p>{kind === "nodes" ? r.node?.valid ? `${r.node.protocol} · ${r.node.server}:${r.node.port}` : "节点解析失败，请编辑修正链接" : description(kind, r)}</p>
                      {kind === "nodes" && <small>分组：{String(r.data.group||"未分组")}{!!r.data.notes&&` · ${r.data.notes}`}</small>}
                      {kind === "sources" && (
                        <><small>{sourceObservation(r.data)}</small>{r.data.exclude_traffic===true&&<small>已从上游流量合计排除（到期仍计入）</small>}{r.data.last_rejection==="suspicious_node_drop"&&<small role="status">异常节点缩水已拦截，保留最近成功库存；上游当前用量未知</small>}</>
                      )}
                    </div>
                  </div>
                  <div className="actions">
                    {kind === "nodes" && <><Button variant="ghost" size="icon-sm" aria-label={"上移 "+String(r.data.name??r.key)} disabled={busy||rows.indexOf(r)===0} onClick={()=>move(r.id,rows[rows.indexOf(r)-1].id)}><ArrowUp size={14}/></Button><Button variant="ghost" size="icon-sm" aria-label={"下移 "+String(r.data.name??r.key)} disabled={busy||rows.indexOf(r)===rows.length-1} onClick={()=>move(r.id,rows[rows.indexOf(r)+1].id)}><ArrowDown size={14}/></Button></>}
                    {typeof r.data.enabled === "boolean" && (
                      <Status value={r.data.enabled ? "enabled" : "disabled"} />
                    )}
                    <small className="muted">#{r.id}</small>
                    {["nodes", "profiles", "sources"].includes(kind) && (
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={busy}
                        onClick={() => inspect(r)}
                      >
                        {kind==="profiles"?"预览与导出":"预览"}
                      </Button>
                    )}
                    {kind === "profiles" && <Button variant="outline" size="sm" disabled={busy} onClick={()=>PUB(r)}>分享与访问</Button>}
                    {kind === "sources" && (
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={busy}
                        onClick={() => batch("refresh", [r.id])}
                      >
                        <RefreshCw size={13} />
                        刷新
                      </Button>
                    )}
                    {kind !== "mail" && (
                      <>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => ED(r)}
                        >
                          编辑
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => RM([r.id])}
                        >
                          删除
                        </Button>
                      </>
                    )}
                    {kind === "public-page" && <><Button variant="outline" size="sm" onClick={()=>PDIR(r)}>订阅目录</Button><Button variant="outline" size="sm" onClick={()=>GB(r)}>留言管理</Button></>}
                    {kind === "public-page" && r.data.enabled === true && (
                      <Button
                        variant="outline"
                        size="sm"
                        onClick={() =>
                          window.open(
                            "/p/" + encodeURIComponent(String(r.data.slug)),
                            "_blank",
                            "noopener,noreferrer",
                          )
                        }
                      >
                        查看公开页
                      </Button>
                    )}
                  </div>
                </article>
              ))}
            </div>
          )}
        </CardContent>
        <div className="pagination">
          <span>第 {page} 页 · 每页 20 条</span>
          <div className="actions">
            <Button
              variant="outline"
              size="icon-sm"
              aria-label="上一页"
              disabled={page === 1}
              onClick={() => P((p) => p - 1)}
            >
              <ArrowLeft size={14} />
            </Button>
            <Button
              variant="outline"
              size="icon-sm"
              aria-label="下一页"
              disabled={page * 20 >= rows.length}
              onClick={() => P((p) => p + 1)}
            >
              <ArrowRight size={14} />
            </Button>
          </div>
        </div>
      </Card>
      <Dialog
        open={!!editor}
        onOpenChange={(v) => {
          if (!v) ED(null);
        }}
      >
        <DialogContent className="business-dialog inventory-dialog">
          <DialogTitle>
            {editor === "new" ? "新增" : "编辑"}
            {title}
          </DialogTitle>
          <DialogDescription>
            保存到中心库存。普通用户不会获得额外节点权限。
          </DialogDescription>
          {editor && (
            <ResourceEditor
              key={kind + ":" + (editor === "new" ? "new" : editor.id)}
              kind={kind}
              existing={editor === "new" ? null : editor}
              save={async (d) => {
                await api("/admin/resources/" + kind, "POST", d);
                await changed();
                ED(null);
                N("已保存" + title);
              }}
            />
          )}
        </DialogContent>
      </Dialog>
      {guestbook&&<GuestbookManager page={guestbook} close={()=>GB(null)}/>}
{publicDirectory&&<PublicSubscriptionDirectory page={publicDirectory} close={()=>PDIR(null)}/>}
      {publication&&<ProfilePublication profile={publication} close={()=>PUB(null)}/>}
      {profileOutput&&<ProfileOutput profile={profileOutput} close={()=>PO(null)}/>}
      {profileOrder&&<ProfileOrdering close={()=>PS(false)} saved={async()=>{await changed();N("订阅组显示顺序已保存；组内引用与用户授权保持不变。")}}/>}
      {managingGroups&&<NodeGroupManager close={()=>MG(false)} saved={async()=>{await changed();await cache.invalidateQueries({queryKey:["selection","nodes"]});await cache.invalidateQueries({queryKey:["inventory","profiles"]});GF("all")}}/>}
      {dedup&&<NodeDedup close={()=>DD(false)} saved={async message=>{SEL([]);await changed();await cache.invalidateQueries({queryKey:["inventory","profiles"]});await cache.invalidateQueries({queryKey:["selection","profiles"]});N(message)}}/>}
      <Dialog open={groupDialog} onOpenChange={v=>!busy&&GD(v)}><DialogContent><DialogTitle>移动所选节点到分组</DialogTitle><DialogDescription>只修改所选 {selected.length} 个节点的分组。留空移至未分组，订阅组授权保持不变。</DialogDescription><form className="stack" onSubmit={e=>{e.preventDefault();void saveGroup()}}><label htmlFor="node-group-destination">目标分组</label><Input id="node-group-destination" value={destination} maxLength={120} onChange={e=>DG(e.target.value)} placeholder="输入新分组，或选择已有分组"/><div className="actions">{groupNames.slice(0,20).map(g=><Button key={g} type="button" size="sm" variant="outline" onClick={()=>DG(g)}>{g}</Button>)}</div>{error&&<p role="alert">{error}</p>}<DialogFooter><Button type="button" variant="outline" disabled={busy} onClick={()=>GD(false)}>取消</Button><Button type="submit" disabled={busy}>{busy?"保存中…":"确认移动分组"}</Button></DialogFooter></form></DialogContent></Dialog>
      <Dialog open={importing} onOpenChange={IM}>
        <DialogContent className="business-dialog inventory-dialog">
          <DialogTitle>导入节点</DialogTitle>
          <DialogDescription>
            原始内容只用于解析与写入；预览不会创建资源。
          </DialogDescription>
          <InventoryImport
            saved={() => {
              void changed();
              N("节点导入完成");
            }}
          />
        </DialogContent>
      </Dialog>
      <Dialog
        open={!!remove}
        onOpenChange={(v) => {
          if (!v && !busy) RM(null);
        }}
      >
        <DialogContent>
          <DialogTitle>
            删除 {remove?.length ?? 0} 条{title}
          </DialogTitle>
          <DialogDescription>
            删除可能影响引用该资源的订阅组。逐项执行并报告失败，不影响 Gate
            上已有授权。
          </DialogDescription>
          <DialogFooter>
            <Button variant="outline" disabled={busy} onClick={() => RM(null)}>
              取消
            </Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={() => remove && batch("delete", remove)}
            >
              {busy ? "正在删除…" : "确认删除"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={preview !== null}
        onOpenChange={(v) => {
          if (!v) PV(null);
        }}
      >
        <DialogContent className="business-dialog inventory-dialog">
          <DialogTitle>节点与输出预览</DialogTitle>
          <DialogDescription>
            仅管理员可见。节点凭据不得公开。
          </DialogDescription>
          <Textarea
            aria-label="库存预览"
            className="sensitive-output"
            value={preview ?? ""}
            readOnly
            rows={12}
          />
        </DialogContent>
      </Dialog>
    </div>
  );
}
function description(kind: string, r: Resource) {
  const d = r.data;
  if (kind === "profiles")
    return `${((d.source_ids as any[]) ?? []).length} 个源 · ${((d.node_ids as any[]) ?? []).length} 个节点 · ${((d.operator_ids as any[]) ?? []).length} 条处理链`;
  if (kind === "operators")
    return `${((d.operators as any[]) ?? []).length} 个有序处理步骤`;
  if (kind === "templates") return String(d.target ?? "未知格式");
  if (kind === "public-page") return "/p/" + String(d.slug ?? "");
  if (kind === "sources")
    return `${d.node_count ?? "未知"} 个节点 · 最近成功 ${d.last_update ?? "未知"}`;
  if (kind === "nodes") {
    try {
      const u = new URL(String(d.url));
      return (
        u.protocol.replace(":", "") +
        " · " +
        u.hostname +
        (u.port ? ":" + u.port : "")
      );
    } catch {
      return "查看节点详情";
    }
  }
  return String(d.status ?? "未知");
}
function sourceObservation(data: Record<string, any> = {}) {
  const usage = data.upstream_usage;
  const updated = Date.parse(data.last_update ?? "");
  const age = Date.now() - updated;
  const interval = Number(data.refresh_seconds ?? 3600) * 1000;
  if (
    data.last_error ||
    !Number.isFinite(age) ||
    age > interval ||
    age < -60000 ||
    !usage ||
    usage.status !== "available"
  )
    return "未知";
  const bytes = (value: unknown) =>
    typeof value === "string" && /^\d+$/.test(value) ? value + " B" : "未知";
  const expiry =
    usage.expire === "0"
      ? "未设到期"
      : typeof usage.expire === "string" &&
          /^\d+$/.test(usage.expire) &&
          Number(usage.expire) < 8640000000000
        ? new Date(Number(usage.expire) * 1000).toLocaleString()
        : "未知";
  return `上传 ${bytes(usage.upload)} / 下载 ${bytes(usage.download)} / 总量 ${bytes(usage.total)} / 到期 ${expiry}`;
}
