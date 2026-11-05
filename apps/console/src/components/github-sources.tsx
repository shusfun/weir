// GitHub 加速：全项目共用一份按优先级排好的下载来源（直连 GitHub、加速镜像、经某台 VPS 中转）。
// 内核、Gate 安装包、中心与 Gate 自更新、客户端安装包、添加 VPS 的安装命令都按这个顺序取用；
// 连续 15 秒低于 100 KiB/s 换下一个（最后一个不限）。内容一律按 GitHub 官方摘要校验，摘要只由中心从 GitHub API 取。
// 调整顺序、增删来源后立即保存；保存失败退回保存前的列表。
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Badge, Button, Field, Form, Input, Notice, SectionHeader,
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue, SortableList, StatusLamp,
} from "@weir/ui";
import { Gauge, Plus, RotateCcw, Trash2 } from "lucide-react";
import { api, isConflict } from "../api";
import { useFormAction, type WaitSpec } from "../lib/action";
import type { Job } from "../lib/jobs";
import { speed } from "../lib/download";
import type { Gate } from "../lib/core";
import { ConflictNotice, State } from "./feedback";
import { dataObject, ObjectLink } from "./object-link";
import { ActionsWait } from "./wait";
import { field, useConsoleContext } from "../lib/console-context";

export interface GitHubSource {
  kind: "direct" | "mirror" | "gate";
  prefix?: string;
  gate_id?: number;
  gate_name?: string;
}
// defaults：中心内置的默认来源（中心给了就用，没给时用与 ghrelease.DefaultSources 相同的内置列表）
interface Sources { sources: GitHubSource[]; revision: string; default: boolean; defaults?: GitHubSource[] }
interface ProbeResult extends GitHubSource {
  configured: boolean;
  ttfb_ms?: number;
  speed?: number;
  error?: string;
}

const builtinDefaults: GitHubSource[] = [{ kind: "direct" }, ...["gh-proxy.com", "ghfast.top", "ghproxy.net", "gh.llkk.cc"].map((h) => ({ kind: "mirror" as const, prefix: `https://${h}/` }))];
const isHTTP = (s: GitHubSource) => s.kind !== "gate";
const keyOf = (s: GitHubSource) => (s.kind === "mirror" ? "mirror:" + s.prefix : s.kind === "gate" ? "gate:" + s.gate_id : "direct");
const same = (a: GitHubSource[], b: GitHubSource[]) => a.map(keyOf).join("\n") === b.map(keyOf).join("\n");
// 发给中心的只有 kind 与前缀 / VPS 编号，名称由中心按编号填
const plain = (list: GitHubSource[]) => list.map((s) => (s.kind === "mirror" ? { kind: s.kind, prefix: s.prefix } : s.kind === "gate" ? { kind: s.kind, gate_id: s.gate_id } : { kind: s.kind }));

function prefixError(v: string, list: GitHubSource[]) {
  if (!v) return "";
  let u: URL;
  try {
    u = new URL(v);
  } catch {
    return "请填写完整地址，如 https://gh-proxy.com/";
  }
  if (u.protocol !== "https:") return "镜像前缀须为 HTTPS 地址";
  if (!v.endsWith("/")) return "镜像前缀须以 / 结尾";
  if (u.username || u.password || u.search || u.hash) return "镜像前缀不能包含账号、查询参数或 #";
  if (list.some((s) => s.kind === "mirror" && s.prefix === v)) return "这个镜像已在列表里";
  return "";
}

function SourceName({ s, gates }: { s: GitHubSource; gates?: Gate[] }) {
  if (s.kind === "direct") return <>GitHub 直连</>;
  if (s.kind === "mirror") return <>镜像 <code className="break-all">{s.prefix}</code></>;
  const name = s.gate_name || gates?.find((g) => g.id === s.gate_id)?.name;
  if (!name) return <>经 VPS（未知 VPS，编号 {s.gate_id}）</>;
  return <>经 VPS <ObjectLink to={{ type: "gate", id: s.gate_id, name }} /></>;
}

const sourceLabel = (s: GitHubSource) => (s.kind === "mirror" ? "镜像 " + s.prefix : s.kind === "gate" ? "VPS " + (s.gate_name || s.gate_id) : "GitHub 直连");

// 测速结果的一行：没有测过时写「未测速」，正在测时写「正在测速」，测过后同一位置换成结果，不增减行
function probeText(r: ProbeResult | undefined, active: boolean) {
  if (active) return { tone: "info" as const, text: "正在测速" };
  if (!r) return { tone: "unknown" as const, text: "未测速" };
  if (r.error) return { tone: "bad" as const, text: r.error };
  const ttfb = typeof r.ttfb_ms === "number" ? `首字节 ${r.ttfb_ms} ms` : "首字节未知";
  return { tone: "ok" as const, text: `${ttfb} · ${speed(r.speed)}` };
}

function mergeResults(prev: ProbeResult[] | null, incoming: ProbeResult[]) {
  const map = new Map((prev ?? []).map((r) => [keyOf(r), r]));
  for (const r of incoming) map.set(keyOf(r), r);
  return [...map.values()];
}

export function GitHubSourcesCard() {
  const query = useQuery({ queryKey: ["github-sources-admin"], queryFn: () => api("/admin/github-sources") as Promise<Sources> });
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[] });
  const [draft, setDraft] = useState<GitHubSource[]>([]);
  const [prefix, setPrefix] = useState("");
  const [gatePick, setGatePick] = useState("");
  const [notice, setNotice] = useState("");
  const [results, setResults] = useState<ProbeResult[] | null>(null);
  // 正在测的那一项：全部测速时由任务推来，单测时点下去就标上
  const [current, setCurrent] = useState<GitHubSource | null>(null);
  useEffect(() => {
    if (query.data) setDraft(query.data.sources);
  }, [query.data?.revision]);

  // 要保存的列表：用 ref 传给请求，改完立即提交时读不到还没生效的 state
  const next = useRef<GitHubSource[]>([]);
  const save = useFormAction(
    () => api("/admin/github-sources", "PUT", { sources: plain(next.current), revision: query.data!.revision }) as Promise<Sources>,
    {
      invalidate: [["github-sources-admin"], ["github-sources"]],
      onSuccess: () => setNotice("GitHub 加速来源已保存。"),
      // 没保存上：列表退回中心里保存着的那份，原因显示在列表下方
      onError: () => setDraft(query.data?.sources ?? []),
    },
  );
  // 要测的一项：空表示测全部。用 ref 传给请求，点下去就能读到
  const one = useRef<GitHubSource | null>(null);
  const waitSpec = useRef<WaitSpec>({ label: "正在测速各个来源", max: 60 });
  const applyOutput = (j: Job) => {
    const out = j.output as { results?: ProbeResult[]; current?: GitHubSource } | undefined;
    if (!out) return;
    if (out.results) setResults((prev) => (one.current ? mergeResults(prev, out.results!) : out.results!));
    setCurrent(out.current ?? null);
  };
  // 中心按已保存的列表测速，在线但没加入的 VPS 排在最后一并测出来；编辑中还没保存的来源显示「未测速」。
  // 每测完一个来源，任务的 output 就推来，对应行马上换成结果。
  const test = useFormAction((track) => api("/admin/github-sources/test", "POST", one.current ? { source: plain([one.current])[0] } : {}, track) as Promise<{ results: ProbeResult[] }>, {
    wait: () => waitSpec.current,
    onJob: applyOutput,
    onSuccess: (r) => {
      setResults((prev) => (one.current ? mergeResults(prev, r.results) : r.results));
      setCurrent(null);
    },
    onError: () => setCurrent(null),
  });
  const startTest = (source: GitHubSource | null) => {
    one.current = source;
    waitSpec.current = source
      ? { label: `正在测速（${sourceLabel(source)}）`, max: 30 }
      : { label: "正在测速各个来源", max: 60 };
    if (source) setCurrent(source);
    else {
      setResults([]);
      setCurrent(null);
    }
    void test.submit();
  };

  const edit = (list: GitHubSource[]) => {
    setDraft(list);
    setNotice("");
    next.current = list;
    void save.submit();
  };

  const activeGates = (gates.data ?? []).filter((g) => g.status === "active");
  const addable = activeGates.filter((g) => !draft.some((s) => s.kind === "gate" && s.gate_id === g.id));
  const resultOf = (s: GitHubSource) => results?.find((r) => keyOf(r) === keyOf(s));
  const extra = (results ?? []).filter((r) => !r.configured && !draft.some((s) => keyOf(s) === keyOf(r)));
  const pError = prefixError(prefix.trim(), draft);
  const label = sourceLabel;
  const active = (s: GitHubSource) => !!current && keyOf(current) === keyOf(s);

  useConsoleContext({
    form: [field("sources", "GitHub 加速来源", draft.map(label).join(" → ")), field("prefix", "加镜像前缀", prefix)],
  });
  if (query.isPending || query.error) return <State loading={query.isPending} error={query.error} onRetry={() => query.refetch()} />;
  const defaults = query.data.defaults?.length ? query.data.defaults : builtinDefaults;
  // 至少留一个直连或镜像：经 VPS 中转时，VPS 也按它们去 GitHub 下载
  const httpCount = draft.filter(isHTTP).length;
  const busy = save.busy;

  return (
    <div className="stack">
      <SectionHeader
        flush
        level="h3"
        title="GitHub 加速"
        description="内核、Gate 安装包、中心与 Gate 自更新、客户端安装包和添加 VPS 的安装命令都按这个顺序从 GitHub 下载：排在前面的先用，连续 15 秒低于 100 KiB/s 就换下一个（最后一个不限）。内容一律按 GitHub 官方摘要校验，不一致即丢弃。镜像是「前缀 + GitHub 地址」的写法，兼容 gh-proxy，可以自建。"
        actions={query.data.default && same(draft, query.data.sources) ? <Badge variant="soft" tone="neutral">默认来源</Badge> : undefined}
      />
      {/* 拖动行首把手或用上移 / 下移调整顺序；放下即保存 */}
      <SortableList
        label="GitHub 加速来源（按优先级）"
        items={draft}
        getId={keyOf}
        disabled={busy}
        onReorder={(next) => edit(next)}
        itemLabel={(s, i) => `第 ${i + 1} 位 ${label(s)}`}
        className="gap-0"
        itemClassName="items-center border-b border-[var(--weir-line-soft)] last:border-b-0"
        renderItem={(s, { index: i, reorder }) => {
          const p = probeText(resultOf(s), active(s));
          return (
            <div className="list-row" {...(s.kind === "gate" && s.gate_id ? dataObject({ type: "gate", id: s.gate_id }) : {})}>
              <div className="min-w-0">
                <strong className="block"><span className="text-[var(--weir-ink-3)]">{i + 1}. </span><SourceName s={s} gates={gates.data} /></strong>
                <small className="flex items-center gap-1.5 text-[var(--weir-ink-3)]"><StatusLamp tone={p.tone} label={p.text} />{p.text}</small>
              </div>
              <div className="actions">
                {reorder}
                <Button variant="ghost" size="icon-sm" aria-label={"测速 " + label(s)} disabled={busy || test.busy} busy={test.busy && active(s)} busyText="正在测速" onClick={() => startTest(s)}><Gauge /></Button>
                <Button variant="ghost" size="icon-sm" aria-label={"删除 " + label(s)} disabled={busy || test.busy || (isHTTP(s) && httpCount === 1)} onClick={() => edit(draft.filter((_, j) => j !== i))}><Trash2 /></Button>
              </div>
            </div>
          );
        }}
      />
      <ConflictNotice error={save.raw} onReload={() => void query.refetch()} />
      {save.error && !isConflict(save.raw) && <Notice tone="bad">没有保存，已退回保存前的列表：{save.error}</Notice>}
      <Form
        onSubmit={() => {
          if (busy || pError || !prefix.trim()) return;
          edit([...draft, { kind: "mirror", prefix: prefix.trim() }]);
          setPrefix("");
        }}
      >
        <div className="flex flex-wrap items-start gap-2">
          <Field className="min-w-0 grow basis-64" label="加镜像前缀" hint="HTTPS 地址，以 / 结尾" customError={pError}>
            <Input inputMode="url" autoComplete="off" value={prefix} placeholder="https://gh-proxy.com/" onChange={(e) => setPrefix(e.target.value)} />
          </Field>
          <Button type="submit" variant="outline" className="sm:mt-[26px]" disabled={busy || !prefix.trim() || !!pError}><Plus />加入镜像</Button>
        </div>
      </Form>
      <div className="flex flex-wrap items-start gap-2">
        <Field className="min-w-0 grow basis-64" label="加 VPS 中转" hint="中心让这台 VPS 去 GitHub 下载，再分块传回中心。">
          <Select value={gatePick} onValueChange={(v) => setGatePick(String(v))} disabled={!addable.length}>
            <SelectTrigger>
              <SelectValue>{addable.find((g) => String(g.id) === gatePick)?.name ?? (gates.isPending ? "正在读取 VPS…" : gates.error ? "VPS 列表读取失败" : addable.length ? "选择一台 VPS" : "没有可加入的 VPS")}</SelectValue>
            </SelectTrigger>
            <SelectContent>{addable.map((g) => <SelectItem key={g.id} value={String(g.id)}>{g.name}{g.online ? "" : "（离线）"}</SelectItem>)}</SelectContent>
          </Select>
        </Field>
        <Button
          variant="outline"
          className="sm:mt-[26px]"
          disabled={busy || !addable.some((g) => String(g.id) === gatePick)}
          onClick={() => {
            const g = addable.find((x) => String(x.id) === gatePick)!;
            edit([...draft, { kind: "gate", gate_id: g.id, gate_name: g.name }]);
            setGatePick("");
          }}
        >
          <Plus />加入 VPS
        </Button>
      </div>
      <div className="actions">
        <Button variant="outline" disabled={test.busy || !draft.length} busy={test.busy && !one.current} busyText="正在测速…" onClick={() => startTest(null)}><Gauge />测速</Button>
        <Button variant="outline" disabled={busy || same(draft, defaults)} onClick={() => edit(defaults)}><RotateCcw />恢复默认</Button>
        <ActionsWait wait={test.wait || save.wait} />
      </div>
      <small className="text-[var(--weir-ink-3)]">{busy ? "正在保存…" : "拖动行首把手或用上移 / 下移调整顺序，增删后立即保存。测速取 1 MiB，完成一个就显示一个，也可以只测某一行。"}</small>
      {test.error && <Notice tone="bad">测速没有完成：{test.error}</Notice>}
      {notice && <Notice role="status">{notice}</Notice>}
      {(extra.length > 0 || (current?.kind === "gate" && !draft.some((s) => keyOf(s) === keyOf(current)) && !extra.some((r) => keyOf(r) === keyOf(current)))) && (
        <div className="stack">
          <small className="text-[var(--weir-ink-3)]">这些在线的 VPS 还没加入列表：</small>
          {current?.kind === "gate" && !draft.some((s) => keyOf(s) === keyOf(current)) && !extra.some((r) => keyOf(r) === keyOf(current)) && (
            <div className="list-row">
              <div className="min-w-0">
                <strong className="block"><SourceName s={current} gates={gates.data} /></strong>
                <small className="flex items-center gap-1.5 text-[var(--weir-ink-3)]"><StatusLamp tone="info" label="正在测速" />正在测速</small>
              </div>
            </div>
          )}
          {extra.map((r) => {
            const p = probeText(r, active(r));
            return (
              <div key={keyOf(r)} className="list-row">
                <div className="min-w-0">
                  <strong className="block"><SourceName s={r} gates={gates.data} /></strong>
                  <small className="flex items-center gap-1.5 text-[var(--weir-ink-3)]"><StatusLamp tone={p.tone} label={p.text} />{p.text}</small>
                </div>
                <div className="actions">
                  <Button variant="ghost" size="icon-sm" aria-label={"测速 " + label(r)} disabled={busy || test.busy} busy={test.busy && active(r)} busyText="正在测速" onClick={() => startTest(r)}><Gauge /></Button>
                  <Button size="sm" variant="outline" disabled={busy || test.busy} onClick={() => edit([...draft, { kind: "gate", gate_id: r.gate_id, gate_name: r.gate_name }])}><Plus />加入</Button>
                </div>
              </div>
            );
          })}
        </div>
      )}
    </div>
  );
}
