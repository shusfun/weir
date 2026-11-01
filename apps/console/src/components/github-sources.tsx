// GitHub 加速：全项目共用一份按优先级排好的下载来源（直连 GitHub、加速镜像、经某台 VPS 中转）。
// 内核、Gate 安装包、中心与 Gate 自更新、客户端安装包、添加 VPS 的安装命令都按这个顺序取用；
// 连续 15 秒低于 100 KiB/s 换下一个（最后一个不限）。内容一律按 GitHub 官方摘要校验，摘要只由中心从 GitHub API 取。
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input, Notice, ReorderButtons, SectionHeader,
  Select, SelectContent, SelectItem, SelectTrigger, SelectValue, StatusLamp,
} from "@weir/ui";
import { Gauge, Plus, RotateCcw, Save, Trash2 } from "lucide-react";
import { api, ApiError, isConflict } from "../api";
import { useFormAction } from "../lib/action";
import { speed } from "../lib/download";
import type { Gate } from "../lib/core";
import { ConflictNotice, State } from "./feedback";
import { dataObject, ObjectLink } from "./object-link";
import { emptyReauth, Reauth, useReauthAction } from "./system-settings";
import { ActionsWait, WaitText } from "./wait";
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

// 这些错误说的是来源列表本身：关闭确认框，标在列表下方
const listErrors = new Set(["invalid_mirror", "invalid_source", "invalid_source_gate", "source_limit", "http_source_required"]);
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

// 测速结果的一行：没有测过时写「未测速」，测过后同一位置换成结果，不增减行
function probeText(r?: ProbeResult) {
  if (!r) return { tone: "unknown" as const, text: "未测速" };
  if (r.error) return { tone: "bad" as const, text: r.error };
  const ttfb = typeof r.ttfb_ms === "number" ? `首字节 ${r.ttfb_ms} ms` : "首字节未知";
  return { tone: "ok" as const, text: `${ttfb} · ${speed(r.speed)}` };
}

export function GitHubSourcesCard() {
  const query = useQuery({ queryKey: ["github-sources-admin"], queryFn: () => api("/admin/github-sources") as Promise<Sources> });
  const gates = useQuery({ queryKey: ["gates"], queryFn: async () => (await api("/admin/gates")).items as Gate[] });
  const [draft, setDraft] = useState<GitHubSource[]>([]);
  const [prefix, setPrefix] = useState("");
  const [gatePick, setGatePick] = useState("");
  const [confirm, setConfirm] = useState(false);
  const [re, setRe] = useState(emptyReauth);
  const [notice, setNotice] = useState("");
  const [listError, setListError] = useState("");
  const [results, setResults] = useState<ProbeResult[] | null>(null);
  useEffect(() => {
    if (query.data) setDraft(query.data.sources);
  }, [query.data?.revision]);

  const save = useReauthAction(
    () => api("/admin/github-sources", "PUT", { sources: plain(draft), revision: query.data!.revision, ...re }) as Promise<Sources>,
    {
      invalidate: [["github-sources-admin"], ["github-sources"]],
      onSuccess: () => {
        setNotice("GitHub 加速来源已保存。");
        setConfirm(false);
        setRe(emptyReauth);
      },
      onError: (e) => {
        // 验证码只能用一次，失败后清空
        setRe((v) => ({ ...v, code: "" }));
        if (e instanceof ApiError && listErrors.has(e.code)) {
          setConfirm(false);
          setListError(e.message);
        }
      },
    },
  );
  // 中心按已保存的列表测速，在线但没加入的 VPS 排在最后一并测出来；编辑中还没保存的来源显示「未测速」
  const test = useFormAction((track) => api("/admin/github-sources/test", "POST", {}, track) as Promise<{ results: ProbeResult[] }>, {
    wait: { label: "正在测速各个来源", max: 60 },
    onSuccess: (r) => setResults(r.results),
  });

  const edit = (next: GitHubSource[]) => {
    setDraft(next);
    setNotice("");
    setListError("");
  };
  const move = (i: number, d: number) => {
    const next = [...draft];
    [next[i], next[i + d]] = [next[i + d], next[i]];
    edit(next);
  };
  const activeGates = (gates.data ?? []).filter((g) => g.status === "active");
  const addable = activeGates.filter((g) => !draft.some((s) => s.kind === "gate" && s.gate_id === g.id));
  const resultOf = (s: GitHubSource) => results?.find((r) => keyOf(r) === keyOf(s));
  const extra = (results ?? []).filter((r) => !r.configured && !draft.some((s) => keyOf(s) === keyOf(r)));
  const pError = prefixError(prefix.trim(), draft);
  const label = (s: GitHubSource) => (s.kind === "mirror" ? "镜像 " + s.prefix : s.kind === "gate" ? "VPS " + (s.gate_name || s.gate_id) : "GitHub 直连");

  useConsoleContext({
    dialog: confirm ? { title: "确认保存 GitHub 加速来源" } : undefined,
    form: [field("sources", "GitHub 加速来源", draft.map(label).join(" → ")), field("prefix", "加镜像前缀", prefix), field("password", "当前密码", re.password, true)],
  });
  if (query.isPending || query.error) return <State loading={query.isPending} error={query.error} onRetry={() => query.refetch()} />;
  const dirty = !same(draft, query.data.sources);
  const defaults = query.data.defaults?.length ? query.data.defaults : builtinDefaults;
  // 至少留一个直连或镜像：经 VPS 中转时，VPS 也按它们去 GitHub 下载
  const httpCount = draft.filter(isHTTP).length;
  const conflict = isConflict(save.raw);

  return (
    <div className="stack">
      <SectionHeader
        flush
        level="h3"
        title="GitHub 加速"
        description="内核、Gate 安装包、中心与 Gate 自更新、客户端安装包和添加 VPS 的安装命令都按这个顺序从 GitHub 下载：排在前面的先用，连续 15 秒低于 100 KiB/s 就换下一个（最后一个不限）。内容一律按 GitHub 官方摘要校验，不一致即丢弃。镜像是「前缀 + GitHub 地址」的写法，兼容 gh-proxy，可以自建。"
        actions={query.data.default && !dirty ? <Badge variant="soft" tone="neutral">默认来源</Badge> : undefined}
      />
      <ol className="stack" aria-label="GitHub 加速来源（按优先级）">
        {draft.map((s, i) => {
          const p = probeText(resultOf(s));
          return (
            <li key={keyOf(s)} className="list-row" {...(s.kind === "gate" && s.gate_id ? dataObject({ type: "gate", id: s.gate_id }) : {})}>
              <div className="min-w-0">
                <strong className="block"><span className="text-[var(--weir-ink-3)]">{i + 1}. </span><SourceName s={s} gates={gates.data} /></strong>
                <small className="flex items-center gap-1.5 text-[var(--weir-ink-3)]"><StatusLamp tone={p.tone} label={p.text} />{p.text}</small>
              </div>
              <div className="actions">
                <ReorderButtons label={label(s)} onUp={() => move(i, -1)} onDown={() => move(i, 1)} upDisabled={i === 0} downDisabled={i === draft.length - 1} />
                <Button variant="ghost" size="icon-sm" aria-label={"删除 " + label(s)} disabled={isHTTP(s) && httpCount === 1} onClick={() => edit(draft.filter((_, j) => j !== i))}><Trash2 /></Button>
              </div>
            </li>
          );
        })}
      </ol>
      {listError && <Notice tone="bad">{listError}</Notice>}
      <Form
        onSubmit={() => {
          if (pError || !prefix.trim()) return;
          edit([...draft, { kind: "mirror", prefix: prefix.trim() }]);
          setPrefix("");
        }}
      >
        <div className="flex flex-wrap items-start gap-2">
          <Field className="min-w-0 grow basis-64" label="加镜像前缀" hint="HTTPS 地址，以 / 结尾" customError={pError}>
            <Input inputMode="url" autoComplete="off" value={prefix} placeholder="https://gh-proxy.com/" onChange={(e) => setPrefix(e.target.value)} />
          </Field>
          <Button type="submit" variant="outline" className="sm:mt-[26px]" disabled={!prefix.trim() || !!pError}><Plus />加入镜像</Button>
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
          disabled={!addable.some((g) => String(g.id) === gatePick)}
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
        <Button variant="outline" disabled={test.busy || !draft.length} busy={test.busy} busyText="正在测速…" onClick={() => void test.submit()}><Gauge />测速</Button>
        <Button variant="outline" disabled={same(draft, defaults)} onClick={() => edit(defaults)}><RotateCcw />恢复默认</Button>
        <Button disabled={!dirty || !httpCount} onClick={() => { save.reset(); setConfirm(true); }}><Save />保存</Button>
        <ActionsWait wait={test.wait} />
      </div>
      <small className="text-[var(--weir-ink-3)]">{dirty ? "有未保存的改动；测速按已保存的列表进行。" : "测速取 1 MiB，逐个进行。"}</small>
      {test.error && <Notice tone="bad">测速没有完成：{test.error}</Notice>}
      {notice && <Notice role="status">{notice}</Notice>}
      {extra.length > 0 && (
        <div className="stack">
          <small className="text-[var(--weir-ink-3)]">这些在线的 VPS 还没加入列表：</small>
          {extra.map((r) => {
            const p = probeText(r);
            return (
              <div key={keyOf(r)} className="list-row">
                <div className="min-w-0">
                  <strong className="block"><SourceName s={r} gates={gates.data} /></strong>
                  <small className="flex items-center gap-1.5 text-[var(--weir-ink-3)]"><StatusLamp tone={p.tone} label={p.text} />{p.text}</small>
                </div>
                <Button size="sm" variant="outline" onClick={() => edit([...draft, { kind: "gate", gate_id: r.gate_id, gate_name: r.gate_name }])}><Plus />加入</Button>
              </div>
            );
          })}
        </div>
      )}
      <Dialog open={confirm} onOpenChange={(v) => !save.busy && !v && setConfirm(false)}>
        <DialogContent size="md">
          <DialogTitle>确认保存 GitHub 加速来源</DialogTitle>
          <DialogDescription>共 {draft.length} 个来源：{draft.map(label).join(" → ")}。其他管理员已修改时会拒绝覆盖。</DialogDescription>
          <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
            <Reauth value={re} onChange={setRe} reason="改下载来源要验证管理员身份。" />
            <ConflictNotice
              error={save.raw}
              onReload={() => {
                setConfirm(false);
                void query.refetch();
              }}
            />
            <DialogFooter error={save.error && !conflict ? save.error : undefined} status={save.wait && <WaitText wait={save.wait} />}>
              <Button type="button" variant="outline" disabled={save.busy} onClick={() => setConfirm(false)}>取消</Button>
              <Button type="submit" busy={save.busy} busyText="正在保存…">确认保存</Button>
            </DialogFooter>
          </Form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
