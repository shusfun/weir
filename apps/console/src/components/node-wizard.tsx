// 四步建节点：协议 → VPS → 链式代理 → 完成。伪装、传输参数由内置方案给出，选完方案可展开「高级」改具体字段；
// REALITY 目标由 VPS 实测选择；证书由系统按 VPS 准备（手动选择在「高级」里）；第 ④ 步勾选用户。
// 保存后显示要放行的端口（对照本机防火墙）与出口实测结果，勾选了用户时再给出他们的链接与二维码。
import { useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge, Button, CheckboxField, ChoiceCard, Collapsible, CopyField, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, FormGrid, Input, Notice, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Step, Steps, Radio, RadioGroup, SwitchField } from "@weir/ui";
import { QRCodeSVG } from "qrcode.react";
import { api } from "../api";
import { useFormAction } from "../lib/action";
import { field, useConsoleContext } from "../lib/console-context";
import type { ChainSpec, Gate, NodeItem, Preset, PresetField, UserItem } from "../lib/core";
import { useCerts } from "./cert-manager";
import { State } from "./feedback";
import { ChainExitFields, exitErrorFields, exitKindName, probeText } from "./chain-exit";
import { NodeAccess } from "./node-access";
import { Link } from "react-router-dom";
import { objectHref } from "../lib/objects";

const stepNames = ["协议", "VPS", "链式代理", "完成"] as const;

interface Share {
  user_id: number;
  email: string;
  name: string;
  links: { link: string; uri: string; error?: string }[];
  error?: string;
}

// gateId：从某台 VPS 的「＋ 协议」打开时已选好这台 VPS（第 ② 步仍可改，证书说明也在那一步）。
export function NodeWizard({ open, close, done, gates, nodes, gateId: initialGate }: { open: boolean; close: () => void; done: () => void; gates: Gate[]; nodes: NodeItem[]; gateId?: number }) {
  const presets = useQuery({ queryKey: ["presets"], queryFn: async () => (await api("/admin/presets")).items as Preset[], enabled: open });
  const users = useQuery({ queryKey: ["users"], queryFn: async () => (await api("/admin/users")).items as UserItem[], enabled: open });
  const certs = useCerts();
  const [step, setStep] = useState(0);
  const [preset, setPreset] = useState("vless-reality");
  const [gateId, setGateId] = useState<number | null>(initialGate ?? null);
  // 每次打开时用这一次的 VPS（关闭后状态保留在组件里）
  useEffect(() => {
    if (open) setGateId(initialGate ?? null);
  }, [open, initialGate]);
  const [certId, setCertId] = useState(0);
  const [chain, setChain] = useState<ChainSpec>({ kind: "none" });
  const [name, setName] = useState("");
  const [port, setPort] = useState("");
  const [target, setTarget] = useState("");
  // 高级字段（只放改过的）与第 ④ 步勾选的用户；保存成功后显示各用户的链接。
  const [advanced, setAdvanced] = useState<Record<string, string | number | boolean>>({});
  const [picked, setPicked] = useState<number[]>([]);
  const [shares, setShares] = useState<Share[] | null>(null);
  const [created, setCreated] = useState<NodeItem | null>(null);
  const active = gates.filter((g) => g.status === "active");
  const chosen = presets.data?.find((p) => p.id === preset);
  const gate = active.find((g) => g.id === gateId);
  const chainExit = new Set((presets.data ?? []).filter((p) => p.chain_exit).map((p) => p.id));
  const exits = nodes.filter((n) => n.enabled && n.gate_id !== gateId && chainExit.has(n.preset));
  const allUsers = (users.data ?? []).filter((u) => u.enabled);
  // 「全部节点」的用户本来就会拿到新节点，默认勾上且不能取消；WireGuard 只给不限额度的用户。
  const wgBlocked = (u: UserItem) => chosen?.protocol === "wireguard" && u.quota > 0;
  const autoUser = (u: UserItem) => u.all_nodes && !wgBlocked(u);
  const userIds = allUsers.filter((u) => autoUser(u) || picked.includes(u.id)).map((u) => u.id);
  const save = useFormAction(
    () =>
      api("/admin/nodes", "POST", {
        gate_id: gateId, preset, name: name.trim(), port: Number(port) || 0, cert_id: certId || 0,
        reality_target: target.trim(), chain: chain.kind === "none" ? { kind: "none" } : chain,
        settings: advanced, user_ids: userIds,
      }) as Promise<NodeItem & { shares?: Share[] }>,
    {
      fields: { port_in_use: "port", invalid_port: "port", no_free_port: "port", invalid_reality_target: "target", reality_probe_failed: "target", invalid_node_settings: "advanced", cert_unavailable: "cert",
        gate_no_public_host: "gate", ...exitErrorFields(chain.kind) },
      onSuccess: (n) => {
        done();
        setCreated(n);
        setShares(n.shares ?? []);
      },
    },
  );
  const reset = () => {
    setStep(0);
    setChain({ kind: "none" });
    setName("");
    setPort("");
    setTarget("");
    setCertId(0);
    setAdvanced({});
    setPicked([]);
    setShares(null);
    setCreated(null);
    save.reset();
    close();
  };
  // 页面感知：向导进行到第几步、未保存的填写内容（SOCKS5 密码只标已填 / 未填）。须在下面显示链接的提前返回之前调用。
  useConsoleContext(open && !shares ? {
    dialog: { title: "新建节点", step: `第 ${step + 1} 步 ${stepNames[step]}` },
    object: gate ? { type: "gate", name: gate.name, id: gate.id } : undefined,
    form: [
      field("preset", "协议方案", chosen?.name), field("gate", "VPS", gate?.name), field("chain", "出口", exitKindName[chain.kind]),
      ...(["socks5", "http", "https"].includes(chain.kind) ? [field("chain_address", "代理地址", chain.address), field("chain_port", "代理端口", chain.port), field("chain_password", "代理密码", chain.password, true)] : []),
      ...(chain.kind === "link" ? [field("chain_link", "节点链接", chain.link, true)] : []),
      field("name", "节点名称", name), field("port", "端口", port), field("target", "伪装目标", target),
      ...Object.entries(advanced).map(([k, v]) => field("set." + k, chosen?.fields.find((f) => f.key === k)?.label ?? k, v)),
    ],
  } : null);
  if (shares) {
    return (
      <Dialog open={open} onOpenChange={(o) => !o && reset()}>
        <DialogContent size="lg">
          <DialogTitle>节点已创建</DialogTitle>
          <DialogDescription>VPS 应用配置约需数秒。{shares.length > 0 ? "下面是勾选用户在这个节点上的节点链接；用户的订阅也已包含它。" : ""}</DialogDescription>
          {created && <NodeAccess ports={created.ports} reach={created.reach} />}
          {created && created.chain.kind !== "none" && <small className="text-[var(--weir-ink-3)]">出口 {exitKindName[created.chain.kind]}：{probeText(created.chain.probe)}</small>}
          {shares.length > 0 && <ShareList shares={shares} leave={reset} />}
          <DialogFooter><Button onClick={reset}>完成</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    );
  }
  // 每一步都是一张表单：“下一步”先按本步的必填项校验，未通过时错误标在字段下方
  const blocked = (step === 0 && !chosen) || (step === 1 && (active.length === 0 || !!gate && !gate.public_host)) || (step === 2 && chain.kind === "node" && exits.length === 0) || (step === 2 && chain.kind === "source" && (!chain.source_id || !chain.match));
  return (
    <Dialog open={open} onOpenChange={(o) => !o && !save.busy && reset()}>
      <DialogContent size="lg">
        <DialogTitle>新建节点</DialogTitle>
        <DialogDescription>只需选协议和 VPS；伪装与传输使用内置的稳定方案，保存前由 VPS 实测。</DialogDescription>
        <Steps orientation="horizontal" aria-label="建节点步骤" className="mb-2 shrink-0">
          {stepNames.map((label, i) => (
            <Step key={label} index={i} label={label} status={i < step ? "complete" : i === step ? "current" : "upcoming"} />
          ))}
        </Steps>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => (step < 3 ? setStep(step + 1) : void save.submit())}>
        {step === 0 && (
          <>
            <PresetStep presets={presets} value={preset} onChange={(v) => { setPreset(v); setAdvanced({}); }} />
            {chosen?.note && <Notice tone="warn">{chosen.note}</Notice>}
            {chosen && chosen.fields.length > 0 && (
              <Collapsible title="高级：修改这个方案的字段">
                <Field name="advanced" hint="留空的字段使用方案的默认值；保存前由 VPS 用真实内核实测。">
                  <AdvancedFields fields={chosen.fields} value={advanced} onChange={setAdvanced} />
                </Field>
              </Collapsible>
            )}
          </>
        )}
        {step === 1 && (
          <>
            {active.length === 0 ? (
              <State empty="还没有已连接的 VPS" detail="先在列表页添加 VPS 并执行安装命令。" />
            ) : (
              <Field label="VPS（入口）" name="gate">
                <Select required value={gateId ? String(gateId) : ""} onValueChange={(v) => setGateId(Number(v))}>
                  <SelectTrigger><SelectValue>{gate ? gate.name : "请选择"}</SelectValue></SelectTrigger>
                  <SelectContent>
                    {active.map((g) => (
                      <SelectItem key={g.id} value={String(g.id)}>{g.name}{g.online ? "" : "（离线）"}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            )}
            {chosen && chosen.cert !== "none" && (
              <>
                <Notice role="status">
                  {chosen.cert === "required"
                    ? "证书由系统为这台 VPS 准备：有 1Panel 时经 1Panel 申请 IP 证书，否则 80 端口空闲时由中心申请；都不可用时用「系统设置 → 节点证书」里的节点证书域名。"
                    : "这台 VPS 的 Hysteria2 / TUIC 节点共用一张自签证书，订阅中固定指纹。"}
                </Notice>
                <Collapsible title="高级：手动选择证书">
                  <Field label="证书" name="cert" hint="一般不需要；选了就只用这张证书。">
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
                </Collapsible>
              </>
            )}
            {gate && !gate.online && <Notice tone="warn">这台 VPS 当前离线，无法实测伪装目标与链式代理。</Notice>}
            {gate && !gate.public_host && (
              <Notice tone="warn">
                这台 VPS 还没有公网地址，生成不了节点链接。请先到 <Link className="underline" to={objectHref({ type: "gate", id: gate.id })} onClick={reset}>VPS 详情</Link> 填写{gate.public_host_hint ? `（本机网卡上的公网 IP 是 ${gate.public_host_hint}）` : ""}。
              </Notice>
            )}
          </>
        )}
        {step === 2 && <ChainExitFields value={chain} onChange={setChain} admin sourcePath="/admin/chain-sources" nodes={exits} nodeHint="出口需要另一台 VPS 上能作出口的节点（VLESS / VMess / Trojan 方案）。" />}
        {step === 3 && (
          <>
            <Field label="节点名称" hint="留空时自动命名。">
              <Input maxLength={100} value={name} placeholder={gate && chosen ? `${gate.name} ${chosen.name}` : ""} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field label="端口" name="port" hint="留空时自动选择 VPS 上未被占用的端口。">
              <Input inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))} />
            </Field>
            {chosen?.id === "vless-reality" && (
              <Field label="伪装目标（可选）" name="target" hint="留空时由 VPS 实测候选站点，选 TLS 1.3 + h2 且延迟最低者。">
                <Input value={target} placeholder="www.microsoft.com" onChange={(e) => setTarget(e.target.value)} />
              </Field>
            )}
            <Field label="给哪些用户" hint="「全部节点」的用户会自动拿到新节点；其他用户勾上后把这个节点加给他们。保存后直接显示他们的链接与二维码。">
              {users.isPending || users.error ? <State loading={users.isPending} error={users.error} onRetry={() => users.refetch()} /> : allUsers.length === 0 ? <small>还没有用户。</small> : (
                <div className="grid max-h-56 gap-1.5 overflow-y-auto sm:grid-cols-2">
                  {allUsers.map((u) => (
                    <CheckboxField
                      key={u.id}
                      label={u.name ? `${u.name}（${u.email}）` : u.email}
                      description={wgBlocked(u) ? "有流量额度：WireGuard 没有按用户的流量统计，只对不限额度的用户生效，不会下发" : u.all_nodes ? "全部节点：自动包含" : undefined}
                      disabled={autoUser(u) || wgBlocked(u)}
                      checked={!wgBlocked(u) && (autoUser(u) || picked.includes(u.id))}
                      onCheckedChange={(on) => setPicked(on ? [...picked, u.id] : picked.filter((id) => id !== u.id))}
                    />
                  ))}
                </div>
              )}
            </Field>
          </>
        )}
        <DialogFooter error={save.error}>
          <Button type="button" variant="outline" disabled={save.busy} onClick={() => (step === 0 ? reset() : setStep(step - 1))}>{step === 0 ? "取消" : "上一步"}</Button>
          <Button type="submit" disabled={blocked} busy={save.busy} busyText="正在实测并保存…">{step < 3 ? "下一步" : "创建节点"}</Button>
        </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}

// AdvancedFields 按方案给出的字段说明渲染；只记改过的字段，留空即用默认值。
function AdvancedFields({ fields, value, onChange }: { fields: PresetField[]; value: Record<string, string | number | boolean>; onChange: (v: Record<string, string | number | boolean>) => void }) {
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

// ShareList：每个勾选用户在新节点上的链接；点「二维码」展开。拿不到链接时说明原因。
// ShareList：每个用户可点到用户详情；链接没包含新节点时直接给「去加上」，点了关掉这个窗口再跳过去。
function ShareList({ shares, leave }: { shares: Share[]; leave: () => void }) {
  const [qr, setQr] = useState("");
  return (
    <ul className="stack max-h-[60vh] overflow-y-auto">
      {shares.map((s) => (
        <li key={s.user_id} className="rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3">
          <Link to={objectHref({ type: "user", id: s.user_id })} onClick={leave} className="text-sm font-semibold text-[var(--weir-accent-text)] underline-offset-4 hover:underline">{s.name ? `${s.name}（${s.email}）` : s.email}</Link>
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

function PresetStep({ presets, value, onChange }: { presets: ReturnType<typeof useQuery<Preset[]>>; value: string; onChange: (v: string) => void }) {
  if (presets.isPending || presets.error) return <State loading={presets.isPending} error={presets.error} onRetry={() => presets.refetch()} />;
  return (
    <RadioGroup aria-label="协议方案" className="grid gap-2 sm:grid-cols-2" value={value} onValueChange={(v) => onChange(String(v))}>
      {presets.data.map((p) => (
        <ChoiceCard key={p.id} selected={p.id === value} render={<label />} className="cursor-pointer">
          <span className="flex items-center gap-2">
            <Radio value={p.id} />
            <strong>{p.name}</strong>
          </span>
          <small className="text-[var(--weir-ink-3)]">{p.summary}</small>
        </ChoiceCard>
      ))}
    </RadioGroup>
  );
}
