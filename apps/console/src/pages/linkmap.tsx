// 链路页：平铺世界地图上的 VPS 与出口，连线是「入口 VPS → 出口」，粗细与流动随实时速率；右侧（窄屏在下方）是详情，
// 下面是全部路径的列表（键盘与读屏用它）。管理员看全部，可按用户、VPS 筛选；普通用户只看自己的。
import { useSearchParams } from "react-router-dom";
import { Badge, Card, CardContent, PageHeader, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@weir/ui";
import { State } from "../components/feedback";
import { LinkMapCanvas } from "../components/link-map-canvas";
import { LinkMapDetail } from "../components/link-map-detail";
import { field, useConsoleContext } from "../lib/console-context";
import { bytes } from "../lib/models";
import { placeText, rateText, useLinkMap } from "../lib/linkmap";

export function LinkMapPage({ admin }: { admin: boolean }) {
  const { map: m, error, store, reload } = useLinkMap(admin);
  const [params, setParams] = useSearchParams();
  const selected = params.get("sel");
  const user = params.get("user") ? Number(params.get("user")) : undefined;
  const gate = params.get("gate") ? Number(params.get("gate")) : undefined;
  const set = (patch: Record<string, string | undefined>) => {
    const next = new URLSearchParams(params);
    for (const [k, v] of Object.entries(patch)) {
      if (v === undefined || v === "") next.delete(k);
      else next.set(k, v);
    }
    setParams(next, { replace: true });
  };
  useConsoleContext({
    page: admin ? "链路" : "我的链路",
    notices: m ? [`${m.gates.length} 台 VPS，${m.paths.length} 条路径`, m.geo.ready ? "IP 定位数据已就绪" : `IP 定位：${m.geo.error ?? "未就绪"}`] : [],
    form: [field("user", "筛选用户", m?.users?.find((u) => u.id === user)?.name), field("gate", "筛选 VPS", m?.gates.find((g) => g.id === gate)?.name), field("sel", "选中", selected)],
  });
  const header = (
    <PageHeader
      eyebrow={admin ? "工作区" : "个人"}
      title={admin ? "链路" : "我的链路"}
      description={admin ? "全部 VPS、节点与出口在地图上的位置与实时流量。速率每 2 秒更新一次，只在有人打开这个页面时采集。" : "你的节点与链式代理在地图上的位置与实时流量；只显示你自己的。"}
    />
  );
  if (!m) return <div className="stack">{header}<State loading={!error} error={error} onRetry={() => void reload()} /></div>;
  const visible = m.paths.filter((p) => (user === undefined || p.user_id === user) && (gate === undefined || p.gate_id === gate));
  const unknownPlaces = [
    ...m.gates.filter((g) => !g.place.known).map((g) => ({ id: "g:" + g.id, name: g.name, why: g.place.why })),
    ...m.exits.filter((x) => x.kind !== "direct" && x.kind !== "node" && x.kind !== "block" && !x.place.known).map((x) => ({ id: "x:" + x.id, name: x.label, why: x.place.why })),
  ];
  const tooltip = (id: string) => {
    if (id.startsWith("g:")) {
      const g = m.gates.find((y) => "g:" + y.id === id);
      return g ? `${g.name}\n${placeText(g.place)}${g.online ? "" : " · 离线"}` : "";
    }
    const x = m.exits.find((y) => "x:" + y.id === id);
    if (!x) return "";
    const v = store.stale(x.from_gate) ? null : store.smooth(store.exits.get(x.id));
    return `${x.label}\n${placeText(x.place)}\n实时 ${rateText(v)}`;
  };
  return (
    <div className="stack">
      {header}
      {admin && (m.users?.length || m.gates.length > 1) ? (
        <div className="flex flex-wrap gap-2">
          <Select value={user === undefined ? "" : String(user)} onValueChange={(v) => set({ user: String(v) || undefined, sel: undefined })}>
            <SelectTrigger aria-label="按用户筛选" className="w-auto min-w-40"><SelectValue>{m.users?.find((u) => u.id === user)?.name ?? "全部用户"}</SelectValue></SelectTrigger>
            <SelectContent>
              <SelectItem value="">全部用户</SelectItem>
              {(m.users ?? []).map((u) => <SelectItem key={u.id} value={String(u.id)}>{u.name}</SelectItem>)}
            </SelectContent>
          </Select>
          <Select value={gate === undefined ? "" : String(gate)} onValueChange={(v) => set({ gate: String(v) || undefined, sel: undefined })}>
            <SelectTrigger aria-label="按 VPS 筛选" className="w-auto min-w-40"><SelectValue>{m.gates.find((g) => g.id === gate)?.name ?? "全部 VPS"}</SelectValue></SelectTrigger>
            <SelectContent>
              <SelectItem value="">全部 VPS</SelectItem>
              {m.gates.map((g) => <SelectItem key={g.id} value={String(g.id)}>{g.name}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
      ) : null}
      {/* 宽屏：详情不参与行高（contain:size），高度跟左边的地图一列一样、内部滚动；选中别的对象时地图与下面的列表不移动 */}
      <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_320px]">
        <div className="stack gap-2 min-w-0">
          <LinkMapCanvas map={m} store={store} focus={{ user, gate }} selected={selected} onSelect={(id) => set({ sel: id ?? undefined })} tooltip={tooltip} />
          <small className="text-[var(--weir-ink-3)]">
            {store.connected ? "实时连接正常" : "实时连接中断，正在重连"} · 圆点是 VPS，菱形是出口，VPS 外的虚线圈是直接出去；线越粗流动越快表示速率越高，灰色虚线表示速率未知。
            {m.geo.ready ? " IP 定位数据来自 DB-IP（CC BY 4.0）。" : ` IP 定位：${m.geo.error ?? "未就绪"}。`}
          </small>
          {unknownPlaces.length > 0 && (
            <div className="flex flex-wrap items-center gap-1.5 text-[12.5px]">
              <span className="text-[var(--weir-ink-3)]">位置未知：</span>
              {unknownPlaces.map((u) => (
                <button key={u.id} type="button" className="rounded-[var(--weir-radius-sm)] border border-[var(--weir-line)] px-1.5 py-0.5 hover:bg-[var(--weir-well)]" title={u.why} onClick={() => set({ sel: u.id })}>{u.name}</button>
              ))}
            </div>
          )}
        </div>
        <Card className="lg:overflow-y-auto lg:[contain:size]" data-linkmap-detail><CardContent><LinkMapDetail m={m} store={store} selected={selected} /></CardContent></Card>
      </div>
      <Card>
        <CardContent className="stack gap-2">
          <h2 className="font-semibold">路径</h2>
          {visible.length === 0 ? (
            <State empty={admin ? "还没有用户在用节点" : "你的链接里还没有节点"} detail={admin ? "给用户分配节点后，这里列出每个用户经哪个入口、从哪个出口出去。" : "管理员给你分配节点后会出现在这里。"} />
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    {admin && <TableHead>用户</TableHead>}
                    <TableHead>入口</TableHead>
                    <TableHead>出口</TableHead>
                    <TableHead>实时</TableHead>
                    <TableHead>今天</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {visible.map((p) => {
                    const x = m.exits.find((e) => e.id === p.exit_id);
                    const v = store.stale(p.gate_id) ? null : store.smooth(store.paths.get(p.id));
                    const on = selected === p.id;
                    return (
                      <TableRow key={p.id} tabIndex={0} aria-selected={on} className={on ? "bg-[var(--weir-accent-soft)]" : "cursor-pointer"}
                        onClick={() => set({ sel: p.id })} onKeyDown={(e) => (e.key === "Enter" || e.key === " ") && (e.preventDefault(), set({ sel: p.id }))}>
                        {admin && <TableCell>{p.user}</TableCell>}
                        <TableCell>{m.gates.find((g) => g.id === p.gate_id)?.name} · {p.node}</TableCell>
                        <TableCell>{x?.label ?? "未知"}{p.route === "chain" && <Badge variant="soft" tone="info" className="ml-1">自建</Badge>}</TableCell>
                        <TableCell className="whitespace-nowrap tabular-nums">{rateText(v)}</TableCell>
                        <TableCell className="whitespace-nowrap tabular-nums">{p.today_bytes === null ? "未知" : bytes(p.today_bytes)}</TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
