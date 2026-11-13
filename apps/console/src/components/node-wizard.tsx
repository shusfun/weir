import { NodeTestControl } from "./node-test";
// 建节点：一页选 VPS、协议、出口就能创建。伪装、传输参数由内置方案给出，名称、端口、证书与方案字段都在「高级」里；
// REALITY 目标由 VPS 实测选择；证书由系统按 VPS 准备；这台 VPS 现在用不了的方案标出原因。
// 保存后显示要放行的端口（对照本机防火墙）与出口实测结果，再给出管理员本人的链接与二维码；谁能用节点在用户页分配。
import { useRef, useState } from "react";
import { useQuery as useStrictQuery, useQueryClient } from "@tanstack/react-query";
import { useUnsavedChanges } from "../lib/dirty";
import { useQuery } from "../lib/query";
import { Badge, Button, ChoiceCard, Collapsible, CopyField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Radio, RadioGroup, Step, Steps, SwitchField, useConfirm } from "@weir/ui";
import { QRCodeSVG } from "qrcode.react";
import { ExitSelector } from "./exit-selector";
import { api, ApiError } from "../api";
import { useFormAction } from "../lib/action";
import { WaitText } from "./wait";
import { field, useConsoleContext } from "../lib/console-context";
import type { ExitSpec, Gate, NodeItem, Preset, PresetField } from "../lib/core";
import { nodeReachSettled } from "../lib/core";
import { useCerts } from "./cert-manager";
import { State } from "./feedback";
import { exitErrorFields, exitKindName, probeText } from "./chain-exit";
import { NodeAccess } from "./node-access";
import { Link } from "react-router-dom";
import { objectHref } from "../lib/objects";
import { PendingExits, type PendingExit } from "./pending-exits";

// 默认只列最常用的三个，其余收在「更多协议」里。
const recommended = ["vless-reality", "hysteria2", "ss2022"];

interface Share {
  user_id: number;
  email: string;
  name: string;
  links: { link: string; uri: string; error?: string }[];
  error?: string;
}

// gateId：从某台 VPS 的「＋ 节点」打开时已选好这台 VPS（仍可改）。
type NodeWizardProps = { open: boolean; close: () => void; done: () => void; gates: Gate[]; nodes: NodeItem[]; gateId?: number };
export function NodeWizard(props: NodeWizardProps) {
  return props.open ? <NodeWizardForm {...props} /> : null;
}

function NodeWizardForm({ open, close, done, gates, nodes, gateId: initialGate }: NodeWizardProps) {
  const active = gates.filter((g) => g.status === "active");
  const [gateId, setGateId] = useState<number | null>(initialGate ?? (active.length === 1 ? active[0].id : null));
  const [savedGate, setSavedGate] = useState<Gate | null>(null);
  const currentGate = active.find((g) => g.id === gateId);
  const gate = savedGate && currentGate && savedGate.id === currentGate.id && savedGate.config_revision >= currentGate.config_revision ? savedGate : currentGate;
  const [hostDraft, setHostDraft] = useState<string | null>(null);
  const host = hostDraft ?? gate?.public_host_hint ?? "";
  const [traceId] = useState(() => crypto.randomUUID().replaceAll("-", ""));
  const client = useQueryClient();
  const confirm = useConfirm();
  const hostSaving = useRef(false);
  // 地址是方案可用性的前置条件；刷新失败不能沿用旧成功结果解锁创建。
  const presets = useStrictQuery({ queryKey: ["presets", gateId ?? 0, gate?.public_host ?? ""], queryFn: async ({ signal }) => (await api("/admin/presets?gate_id=" + gateId, "GET", undefined, { traceId, signal })).items as Preset[], enabled: !!gateId, staleTime: 0 });
  const me = useQuery({ queryKey: ["me-account"], queryFn: () => api("/me") as Promise<{ id: number }>, enabled: open });
  const certs = useCerts();
  const [preset, setPreset] = useState("vless-reality");
  const [certId, setCertId] = useState(0);
  const [exitId,setExitId]=useState(0);
  const [chain, setChain] = useState<ExitSpec>({ kind: "freedom" });
  const [name, setName] = useState("");
  const [port, setPort] = useState("");
  const [target, setTarget] = useState("");
  // 高级字段（只放改过的）；保存成功后显示管理员本人的链接。
  const [advanced, setAdvanced] = useState<Record<string, string | number | boolean>>({});
  const [shares, setShares] = useState<Share[] | null>(null);
  const [created, setCreated] = useState<NodeItem | null>(null);
  const [candidate, setCandidate] = useState<PendingExit | null>(null);
  const chosen = presets.data?.find((p) => p.id === preset);
  const chainExit = new Set((presets.data ?? []).filter((p) => p.chain_exit).map((p) => p.id));
  const exits = nodes.filter((n) => n.enabled && n.gate_id !== gateId && chainExit.has(n.preset));
  const exitFields = exitErrorFields(chain.kind);
  const errorFields: Record<string, string> = { port_in_use: "port", invalid_port: "port", no_free_port: "port", invalid_reality_target: "target", reality_probe_failed: "target", invalid_node_settings: "advanced", cert_unavailable: "cert",
    gate_no_public_host: "gate", ...exitFields };
  // 错误落在「高级」里的字段时自动展开，否则看不到。
  const [more, setMore] = useState(false);
  const save = useFormAction(
    (track) =>
      api("/admin/nodes", "POST", {
        gate_id: gateId, preset, name: name.trim(), port: Number(port) || 0, cert_id: certId || 0,
        reality_target: target.trim(), ...(exitId ? {exit_id:exitId}:{exit:chain}),
        settings: advanced, user_ids: me.data ? [me.data.id] : [],
      }, { ...track, traceId }) as Promise<(NodeItem & { shares?: Share[] }) | { pending: PendingExit }>,
    {
      wait: () => ({ label: `正在 ${gate?.name ?? "VPS"} 上新建 ${chosen?.name ?? ""} 节点` }),
      fields: errorFields,
      onError: (e) => {
        const f = e instanceof ApiError ? errorFields[e.code] : undefined;
        if (f && ["advanced", "cert", "port", "target"].includes(f)) setMore(true);
      },
      onSuccess: (n) => {
        done();
        if ("pending" in n) { setCandidate(n.pending); return; }
        setCreated(n);
        setShares(n.shares ?? []);
      },
    },
  );
  const address = useFormAction(
    () => api("/admin/gates/" + gateId, "PATCH", { public_host: host.trim() }, { traceId }) as Promise<Gate>,
    {
      fields: { invalid_public_host: "public_host", public_host_not_public: "public_host", public_host_unresolved: "public_host", public_host_lookup_failed: "public_host" },
      onSuccess: async (updated) => {
        setSavedGate(updated);
        setHostDraft(null);
        client.setQueryData<Gate[]>(["gates"], (items) => items?.map((g) => g.id === updated.id ? updated : g));
        await client.invalidateQueries({ queryKey: ["presets", updated.id] });
        done();
      },
    },
  );
  const addressNeeded = !!gate && !gate.public_host;
  const dirty = !shares && !candidate && (hostDraft !== null || preset !== "vless-reality" || name !== "" || port !== "" || target !== "" || certId !== 0 || exitId !== 0 || chain.kind !== "freedom" || Object.keys(advanced).length > 0);
  const clearDirty = useUnsavedChanges(dirty);
  const discard = async () => !dirty || await confirm({ title: "放弃未保存修改？", description: "已保存的公网地址会保留，尚未创建的节点填写内容会丢失。", confirmText: "放弃修改", tone: "danger" });
  const changeGate = async (id: number) => {
    if (id === gateId || hostSaving.current || save.busy || !await discard()) return;
    clearDirty();
    setHostDraft(null); setSavedGate(null); address.reset(); save.reset();
    setPreset("vless-reality"); setAdvanced({}); setCertId(0); setChain({ kind: "freedom" }); setExitId(0);
    setName(""); setPort(""); setTarget(""); setMore(false);
    setGateId(id);
  };
  const saveAddress = async () => {
    if (hostSaving.current || !host.trim()) return;
    hostSaving.current = true;
    try { await address.submit(); } finally { hostSaving.current = false; }
  };
  const reset = () => {
    setMore(false);
    setChain({ kind: "freedom" });setExitId(0);
    setName("");
    setPort("");
    setTarget("");
    setCertId(0);
    setAdvanced({});
    setShares(null);
    setCreated(null);
    setCandidate(null);
    save.reset();
    close();
  };
  const requestClose = async () => {
    if (hostSaving.current || (save.busy && !save.detachable)) return;
    if (save.busy || await discard()) { clearDirty(); save.leave(); reset(); }
  };
  // 页面感知：向导进行到第几步、未保存的填写内容（SOCKS5 密码只标已填 / 未填）。须在下面显示链接的提前返回之前调用。
  useConsoleContext(open && !shares ? {
    dialog: { title: "新建节点" },
    object: gate ? { type: "gate", name: gate.name, id: gate.id } : undefined,
    form: [
      field("preset", "协议方案", chosen?.name), field("gate", "VPS", gate?.name), field("public_host", "公网地址", gate?.public_host || host), field("chain", "出口", exitKindName[chain.kind]),
      ...(["socks5", "http", "https"].includes(chain.kind) ? [field("chain_address", "代理地址", chain.address), field("chain_port", "代理端口", chain.port), field("chain_password", "代理密码", chain.password, true)] : []),
      ...(chain.kind === "link" ? [field("chain_link", "节点链接", chain.link, true)] : []),
      field("name", "节点名称", name), field("port", "端口", port), field("target", "伪装目标", target),
      ...Object.entries(advanced).map(([k, v]) => field("set." + k, chosen?.fields?.find((f) => f.key === k)?.label ?? k, v)),
    ],
  } : null);
  if (candidate) return <Dialog open={open} onOpenChange={(o) => !o && reset()}><DialogContent size="lg">
    <DialogTitle>出口已保存，等待验证</DialogTitle>
    <DialogDescription>节点尚未创建，验证通过后自动创建并下发。可以关闭窗口，稍后在 VPS 或出口页面查看任务。</DialogDescription>
    <PendingExits candidate={candidate.id} />
    <DialogFooter><Button onClick={reset}>完成</Button></DialogFooter>
  </DialogContent></Dialog>;
  if (shares) {
    return (
      <Dialog open={open} onOpenChange={(o) => !o && reset()}>
        <DialogContent size="lg">
          <DialogTitle>节点已创建</DialogTitle>
          <DialogDescription>VPS 应用配置约需数秒。已进入你和「全部节点」用户的订阅；其他用户到 <Link className="underline" to="/users" onClick={reset}>用户</Link> 里分配。</DialogDescription>
          {created && <CreatedAccess node={created} />}
          {created && created.exit.kind !== "freedom" && <small className="text-[var(--weir-ink-3)]">出口 {exitKindName[created.exit.kind]}：{probeText(created.exit.probe)}</small>}
          {shares.length > 0 && <ShareList shares={shares} leave={reset} />}
          <DialogFooter><Button onClick={reset}>完成</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    );
  }
  const gateReady = !!gate?.public_host;
  const protocolReady = gateReady && !presets.isPending && !presets.isFetching && !presets.error && !!chosen && chosen.available !== false;
  const blocked = !protocolReady || address.busy || (chain.kind === "node" && exits.length === 0) || (chain.kind === "source" && (!chain.source_id || !chain.match));
  return (
    <Dialog open={open} onOpenChange={(o) => { if (!o) void requestClose(); }}>
      <DialogContent size="lg">
        <DialogTitle>新建节点</DialogTitle>
        <Steps orientation="horizontal" aria-label="新建节点">
          <Step index={0} label="VPS" status={gateReady ? "complete" : "current"} />
          <Step index={1} label="协议" status={!gateReady ? "upcoming" : protocolReady ? "complete" : "current"} />
          <Step index={2} label="出口" status={protocolReady ? "current" : "upcoming"} />
          <Step index={3} label="创建" status="upcoming" />
        </Steps>
        <DialogDescription>创建后显示端口并后台实测，失败保留节点，可放行防火墙后重试；链式出口验证通过后才启用。</DialogDescription>
        <Form busy={save.busy || address.busy} errors={{ ...save.errors, ...address.errors }} onSubmit={() => { if (addressNeeded) void saveAddress(); else if (!blocked) void save.submit(); }}>
          {active.length === 0 ? (
            <State empty="还没有已连接的 VPS" detail="先在列表页添加 VPS 并执行安装命令。" />
          ) : (
            <Field label="VPS" name="gate" required>
              <Select required value={gateId ? String(gateId) : ""} onValueChange={(v) => void changeGate(Number(v))}>
                <SelectTrigger><SelectValue>{gate ? gate.name : "请选择"}</SelectValue></SelectTrigger>
                <SelectContent>
                  {active.map((g) => (
                    <SelectItem key={g.id} value={String(g.id)}>{g.name}{g.online ? "" : "（离线）"}</SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          )}
          {gate && !gate.online && <Notice tone="warn">这台 VPS 当前离线，无法实测伪装目标与链式代理。</Notice>}
          {addressNeeded && (
            <Field label="节点连接地址" name="public_host" required data-tour="node-public-host" hint={gate.public_host_hint ? "已填入本机识别的公网 IP，请核对后保存；也可以改为其他公网 IP 或直连域名。" : "填写客户端连接这台 VPS 的公网 IP 或直连域名。保存后可在此继续创建节点。"}>
              <Input required value={host} onChange={(e) => { setHostDraft(e.target.value); address.reset(); }} placeholder="公网 IP 或域名" />
            </Field>
          )}
          <Field label="协议" data-tour="node-preset">
            <PresetChoice presets={presets} value={preset} onChange={(v) => { setPreset(v); setAdvanced({}); }} />
          </Field>
          {chosen?.note && <Notice tone="warn">{chosen.note}</Notice>}
          <ExitSelector gateId={gateId ?? 0} exitId={exitId} onExitId={id=>setExitId(id??0)} value={chain} onChange={setChain} admin sourcePath="/admin/chain-sources" nodes={exits} nodeHint="出口需要另一台 VPS 上能作出口的节点（VLESS / VMess / Trojan 方案）。" />
          <small className="text-[var(--weir-ink-3)]">这里的出口管这个节点的全部流量。按网站分流在 VPS 的「出口与分流」，按协议类型统一加出口在 <Link className="underline" to="/proxy">代理</Link>。</small>
          <div data-tour="node-options"><Collapsible key={more ? "more" : "less"} title="高级：名称、端口、证书" defaultOpen={more}>
            <div className="stack">
              <Field label="节点名称" hint="留空时自动命名。">
                <Input maxLength={100} value={name} placeholder={gate && chosen ? `${gate.name} ${chosen.name}` : ""} onChange={(e) => setName(e.target.value)} />
              </Field>
              <Field label="端口" name="port" hint="留空时自动选择 VPS 上未被占用的端口。">
                <Input inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))} />
              </Field>
              {chosen?.id === "vless-reality" && (
                <Field label="伪装目标" name="target" hint="留空时由这台 VPS 用 DoH 解析候选站点，再选 TLS 1.3、h2 且握手最快的。DoH 失败的候选不算合适。">
                  <Input value={target} placeholder="www.microsoft.com" onChange={(e) => setTarget(e.target.value)} />
                </Field>
              )}
              {chosen && chosen.cert !== "none" && (
                <Field label="证书" name="cert" hint={chosen.cert === "required" ? "默认由系统准备：有 1Panel 时经 1Panel 申请 IP 证书，否则 80 端口空闲时由中心申请；都不可用时用「系统设置 → 节点证书」里的域名。" : "默认这台 VPS 的 Hysteria2 / TUIC 节点共用一张自签证书，订阅中固定指纹。"}>
                  <Select value={String(certId)} onValueChange={(v) => setCertId(Number(v))}>
                    <SelectTrigger><SelectValue>{certs.data?.find((c) => c.id === certId)?.domain ?? "由系统准备"}</SelectValue></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="0">由系统准备</SelectItem>
                      {(certs.data ?? []).map((c) => (
                        <SelectItem key={c.id} value={String(c.id)}>{c.domain}（{c.name}）</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
              )}
              {chosen && !!chosen.fields?.length && (
                <Field label="方案字段" name="advanced" hint="留空的字段使用方案的默认值。">
                  <AdvancedFields fields={chosen.fields} value={advanced} onChange={setAdvanced} />
                </Field>
              )}
            </div>
          </Collapsible></div>
          <DialogFooter error={address.error || save.error} status={save.wait && <WaitText wait={save.wait} />}>
            <Button type="button" variant="outline" disabled={save.busy || address.busy} onClick={() => void requestClose()}>取消</Button>
            {addressNeeded
              ? <Button data-tour="node-address-submit" type="submit" disabled={!host.trim()} busy={address.busy} busyText="正在保存地址…">保存地址并继续</Button>
              : <Button data-tour="node-submit" type="submit" disabled={blocked} busy={save.busy} busyText="正在保存并下发…">创建节点</Button>}
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}

// CreatedAccess：外部连接在中心后台检查（等 VPS 应用了新配置再连，最多约 1 分钟）：结果出来之前每 2 秒刷新，
// 不停在创建那一刻的「还没有检查过」。超过 100 秒还没有结果时如实写明，不再刷新。
function CreatedAccess({ node }: { node: NodeItem }) {
  const [since] = useState(() => Date.now());
  const checked = nodeReachSettled;
  const q = useQuery({
    queryKey: ["node", node.id, "reach"],
    queryFn: () => api("/admin/nodes/" + node.id) as Promise<NodeItem>,
    initialData: node,
    refetchInterval: (query) => (checked(query.state.data) && query.state.data?.test?.status !== "pending" || Date.now() - since > 100_000 ? false : 2000),
  });
  const n = q.data ?? node;
  const done = checked(n);
  const reach = n.readiness?.state === "credentials" ? { status: "unknown" as const, why: n.readiness.why } : done ? n.reach : { status: "unknown" as const, why: Date.now() - since > 100_000 ? n.readiness?.why || "100 秒内没有得到检查结果，稍后在节点详情里查看" : "正在检查：等 VPS 应用新配置后从中心连接一次" };
  return <div className="stack"><Notice tone="ok">节点已保存，端口 {n.port}。配置{n.application?.state === "applied" ? "已应用" : n.application?.state === "failed" ? "应用失败" : "待应用"}，实测会在后台进行。</Notice>{n.warnings?.map(w => <Notice key={w} tone="warn">{w}</Notice>)}<NodeAccess ports={n.ports} reach={reach} /><NodeTestControl node={n} detail /></div>;
}

// AdvancedFields 按方案给出的字段说明渲染；只记改过的字段，留空即用默认值。
export function AdvancedFields({ fields, value, onChange }: { fields: PresetField[]; value: Record<string, string | number | boolean>; onChange: (v: Record<string, string | number | boolean>) => void }) {
  const set = (key: string, v: string | number | boolean | undefined) => {
    const next = { ...value };
    if (v === undefined || v === "") delete next[key];
    else next[key] = v;
    onChange(next);
  };
  return (
    <FormGrid>
      {fields.map((f) => {
        const label = f.unit ? `${f.label}（${f.unit}）` : f.label;
        const v = value[f.key];
        switch (f.kind) {
          case "bool":
            return <SwitchField key={f.key} label={label} description={f.hint} checked={v === true} onCheckedChange={(on) => set(f.key, on)} />;
          case "select":
            return (
              <Field key={f.key} label={label} hint={f.hint}>
                <Select value={v === undefined ? "__default" : String(v)} onValueChange={(x) => (x === "__default" ? set(f.key, undefined) : onChange({ ...value, [f.key]: String(x) }))}>
                  <SelectTrigger><SelectValue>{v === undefined ? "方案默认" : f.options?.find(([o]) => o === v)?.[1] ?? String(v)}</SelectValue></SelectTrigger>
                  <SelectContent>
                    <SelectItem value="__default">方案默认</SelectItem>
                    {(f.options ?? []).map(([o, text]) => <SelectItem key={o || "none"} value={o}>{text}</SelectItem>)}
                  </SelectContent>
                </Select>
              </Field>
            );
          case "number":
            return (
              <Field key={f.key} label={label} hint={f.hint}>
                <Input inputMode="numeric" placeholder="方案默认" value={v === undefined ? "" : String(v)} onChange={(e) => { const d = e.target.value.replace(/\D/g, ""); set(f.key, d ? Number(d) : undefined); }} />
              </Field>
            );
          default:
            return (
              <Field key={f.key} label={label} hint={f.hint}>
                <Input placeholder="方案默认" value={v === undefined ? "" : String(v)} onChange={(e) => set(f.key, e.target.value.trim())} />
              </Field>
            );
        }
      })}
    </FormGrid>
  );
}

// ShareList：新节点上的链接；点「二维码」展开，拿不到链接时说明原因。链接没包含新节点时给「去加上」，点了关掉这个窗口再跳过去。
function ShareList({ shares, leave }: { shares: Share[]; leave: () => void }) {
  const [qr, setQr] = useState("");
  return (
    <ul className="stack">
      {shares.map((s) => (
        <li key={s.user_id} className="rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3">
          <Link to={objectHref({ type: "user", id: s.user_id })} onClick={leave} className="text-body font-semibold weir-hit relative text-[var(--weir-accent-text)] underline-offset-4 transition-opacity hover:underline active:opacity-60">{s.name ? `${s.name}（${s.email}）` : s.email}</Link>
          {s.error ? (
            <div className="mt-1 flex flex-wrap items-center gap-2">
              <small className="text-[var(--weir-warn-ink)]">{s.error}</small>
              <Button size="xs" variant="outline" render={<Link to={objectHref({ type: "user", id: s.user_id })} onClick={leave} />}>去用户详情加上</Button>
            </div>
          ) : s.links.map((l) => (
            <div key={l.link + l.uri} className="mt-2 stack">
              <div className="flex flex-wrap items-center gap-2">
                <Badge variant="soft" tone="neutral">{l.link}</Badge>
                {l.uri ? (
                  <Button size="xs" variant="outline" aria-expanded={qr === l.uri} onClick={() => setQr(qr === l.uri ? "" : l.uri)}>二维码</Button>
                ) : <small className="text-[var(--weir-bad-ink)]">{l.error || "暂时生成不了链接"}</small>}
              </div>
              {l.uri && <CopyField label="节点链接" value={l.uri} />}
              {qr === l.uri && l.uri && <div className="qr-code mx-auto"><QRCodeSVG value={l.uri} size={200} title="节点二维码" /></div>}
            </div>
          ))}
        </li>
      ))}
    </ul>
  );
}

// PresetChoice：常用的三个直接列出，其余折叠；选中的不在常用里时展开。这台 VPS 用不了的方案不能选，写明原因。
function PresetChoice({ presets, value, onChange }: { presets: ReturnType<typeof useQuery<Preset[]>>; value: string; onChange: (v: string) => void }) {
  if (presets.isPending || presets.error) return <State loading={presets.isPending} error={presets.error} onRetry={() => presets.refetch()} />;
  const card = (p: Preset) => {
    const off = p.available === false;
    return (
      <ChoiceCard key={p.id} selected={p.id === value} render={<label />} className={off ? "opacity-60" : "cursor-pointer"}>
        <span className="flex items-center gap-2">
          <Radio value={p.id} disabled={off} />
          <strong>{p.name}</strong>
          {p.id === recommended[0] && !off && <Badge variant="soft" tone="ok">推荐</Badge>}
        </span>
        <small className={off ? "text-[var(--weir-warn-ink)]" : "text-[var(--weir-ink-3)]"}>{off ? "这台 VPS 现在用不了：" + p.why : p.summary.replace("（推荐）", "")}</small>
      </ChoiceCard>
    );
  };
  const top = presets.data.filter((p) => recommended.includes(p.id));
  const rest = presets.data.filter((p) => !recommended.includes(p.id));
  return (
    <RadioGroup aria-label="协议方案" className="stack" value={value} onValueChange={(v) => onChange(String(v))}>
      <div className="grid gap-2 sm:grid-cols-3">{top.map(card)}</div>
      {rest.length > 0 && (
        <Collapsible title={`更多协议（${rest.length}）`} defaultOpen={rest.some((p) => p.id === value)}>
          <div className="grid gap-2 sm:grid-cols-2">{rest.map(card)}</div>
        </Collapsible>
      )}
    </RadioGroup>
  );
}
