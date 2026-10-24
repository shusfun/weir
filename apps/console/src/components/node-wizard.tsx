// 四步建节点：协议 → VPS → 链式代理 → 完成。伪装、传输参数由内置方案固定，REALITY 目标由 VPS 实测选择。
import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Button, ChoiceCard, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Input, Notice, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue, Step, Steps } from "@weir/ui";
import { api } from "../api";
import type { ChainSpec, Gate, NodeItem, Preset } from "../lib/core";
import { useCerts } from "./cert-manager";
import { State } from "./feedback";

const stepNames = ["协议", "VPS", "链式代理", "完成"] as const;

export function NodeWizard({ open, close, done, gates, nodes }: { open: boolean; close: () => void; done: () => void; gates: Gate[]; nodes: NodeItem[] }) {
  const presets = useQuery({ queryKey: ["presets"], queryFn: async () => (await api("/admin/presets")).items as Preset[], enabled: open });
  const certs = useCerts();
  const [step, setStep] = useState(0);
  const [preset, setPreset] = useState("vless-reality");
  const [gateId, setGateId] = useState<number | null>(null);
  const [certId, setCertId] = useState(0);
  const [chain, setChain] = useState<ChainSpec>({ kind: "none" });
  const [name, setName] = useState("");
  const [port, setPort] = useState("");
  const [target, setTarget] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const active = gates.filter((g) => g.status === "active");
  const chosen = presets.data?.find((p) => p.id === preset);
  const gate = active.find((g) => g.id === gateId);
  const exits = nodes.filter((n) => n.enabled && n.gate_id !== gateId && ["vless", "vmess", "trojan"].includes(n.protocol));
  const reset = () => {
    setStep(0);
    setChain({ kind: "none" });
    setName("");
    setPort("");
    setTarget("");
    setCertId(0);
    setError("");
    close();
  };
  const canNext =
    (step === 0 && !!chosen) ||
    (step === 1 && !!gate && (chosen?.cert !== "required" || certId > 0)) ||
    (step === 2 && (chain.kind === "none" || (chain.kind === "socks5" ? !!chain.address && !!chain.port : !!chain.node_id))) ||
    step === 3;
  async function submit() {
    setBusy(true);
    setError("");
    try {
      await api("/admin/nodes", "POST", {
        gate_id: gateId, preset, name: name.trim(), port: Number(port) || 0, cert_id: certId || 0,
        reality_target: target.trim(), chain: chain.kind === "none" ? { kind: "none" } : chain,
      });
      done();
      reset();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open={open} onOpenChange={(o) => !o && !busy && reset()}>
      <DialogContent size="lg">
        <DialogTitle>新建节点</DialogTitle>
        <DialogDescription>只需选协议和 VPS；伪装与传输使用内置的稳定方案，保存前由 VPS 实测。</DialogDescription>
        <Steps orientation="horizontal" aria-label="建节点步骤">
          {stepNames.map((label, i) => (
            <Step key={label} index={i} label={label} status={i < step ? "complete" : i === step ? "current" : "upcoming"} />
          ))}
        </Steps>
        {step === 0 && <PresetStep presets={presets} value={preset} onChange={setPreset} />}
        {step === 1 && (
          <div className="stack">
            {active.length === 0 ? (
              <State empty="还没有已连接的 VPS" detail="先在列表页添加 VPS 并执行安装命令。" />
            ) : (
              <Field label="VPS（入口）">
                <Select value={gateId ? String(gateId) : ""} onValueChange={(v) => setGateId(Number(v))}>
                  <SelectTrigger aria-label="选择 VPS"><SelectValue>{gate ? gate.name : "请选择"}</SelectValue></SelectTrigger>
                  <SelectContent>
                    {active.map((g) => (
                      <SelectItem key={g.id} value={String(g.id)}>{g.name}{g.online ? "" : "（离线）"}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            )}
            {chosen && chosen.cert !== "none" && (
              <Field label="证书" hint={chosen.cert === "required" ? "该方案需要域名证书。" : "不选则自动自签，订阅中固定指纹。"}>
                <Select value={String(certId)} onValueChange={(v) => setCertId(Number(v))}>
                  <SelectTrigger aria-label="选择证书"><SelectValue>{certs.data?.find((c) => c.id === certId)?.domain ?? (chosen.cert === "required" ? "请选择" : "自动自签")}</SelectValue></SelectTrigger>
                  <SelectContent>
                    {chosen.cert !== "required" && <SelectItem value="0">自动自签</SelectItem>}
                    {(certs.data ?? []).map((c) => (
                      <SelectItem key={c.id} value={String(c.id)}>{c.domain}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            )}
            {gate && !gate.online && <Notice tone="warn">这台 VPS 当前离线，无法实测伪装目标与链式代理。</Notice>}
          </div>
        )}
        {step === 2 && <ChainStep chain={chain} onChange={setChain} exits={exits} />}
        {step === 3 && (
          <div className="stack">
            <Field label="节点名称" htmlFor="node-name" hint="留空时自动命名。">
              <Input id="node-name" maxLength={100} value={name} placeholder={gate && chosen ? `${gate.name} ${chosen.name}` : ""} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field label="端口" htmlFor="node-port" hint="留空时自动选择 VPS 上未被占用的端口。">
              <Input id="node-port" inputMode="numeric" value={port} onChange={(e) => setPort(e.target.value.replace(/\D/g, ""))} />
            </Field>
            {chosen?.id === "vless-reality" && (
              <Field label="伪装目标（可选）" htmlFor="node-target" hint="留空时由 VPS 实测候选站点，选 TLS 1.3 + h2 且延迟最低者。">
                <Input id="node-target" value={target} placeholder="www.microsoft.com" onChange={(e) => setTarget(e.target.value)} />
              </Field>
            )}
            <Notice role="status">保存后新节点自动加入「全部节点」的用户，VPS 应用配置约需数秒。</Notice>
          </div>
        )}
        {error && <Notice tone="bad">{error}</Notice>}
        <DialogFooter>
          <Button type="button" variant="outline" disabled={busy} onClick={() => (step === 0 ? reset() : setStep(step - 1))}>{step === 0 ? "取消" : "上一步"}</Button>
          {step < 3 ? (
            <Button disabled={!canNext} onClick={() => setStep(step + 1)}>下一步</Button>
          ) : (
            <Button disabled={busy} onClick={() => void submit()}>{busy ? "正在实测并保存…" : "创建节点"}</Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function PresetStep({ presets, value, onChange }: { presets: ReturnType<typeof useQuery<Preset[]>>; value: string; onChange: (v: string) => void }) {
  if (presets.isPending || presets.error) return <State loading={presets.isPending} error={presets.error} onRetry={() => presets.refetch()} />;
  return (
    <div role="radiogroup" aria-label="协议方案" className="grid gap-2 sm:grid-cols-2">
      {presets.data.map((p) => (
        <ChoiceCard key={p.id} selected={p.id === value} render={<label />} className="cursor-pointer">
          <span className="flex items-center gap-2">
            <input type="radio" name="preset" value={p.id} checked={p.id === value} onChange={() => onChange(p.id)} />
            <strong>{p.name}</strong>
          </span>
          <small className="text-[var(--mg-ink-3)]">{p.summary}</small>
        </ChoiceCard>
      ))}
    </div>
  );
}

function ChainStep({ chain, onChange, exits }: { chain: ChainSpec; onChange: (c: ChainSpec) => void; exits: NodeItem[] }) {
  const set = (patch: Partial<ChainSpec>) => onChange({ ...chain, ...patch });
  return (
    <div className="stack">
      <Segmented
        aria-label="出口"
        value={chain.kind}
        onValueChange={(kind) => onChange({ kind })}
        options={[["none", "直接出去"], ["socks5", "家宽 SOCKS5"], ["node", "另一台 VPS"]] as const}
      />
      {chain.kind === "none" && <p className="text-[var(--mg-ink-3)]">流量从这台 VPS 直接访问目标网站。</p>}
      {chain.kind === "socks5" && (
        <div className="grid gap-3 sm:grid-cols-2">
          <Field label="地址" htmlFor="chain-addr"><Input id="chain-addr" required value={chain.address ?? ""} onChange={(e) => set({ address: e.target.value.trim() })} /></Field>
          <Field label="端口" htmlFor="chain-port"><Input id="chain-port" inputMode="numeric" required value={chain.port ? String(chain.port) : ""} onChange={(e) => set({ port: Number(e.target.value.replace(/\D/g, "")) || undefined })} /></Field>
          <Field label="用户名（可选）" htmlFor="chain-user"><Input id="chain-user" autoComplete="off" value={chain.username ?? ""} onChange={(e) => set({ username: e.target.value })} /></Field>
          <Field label="密码（可选）" htmlFor="chain-pass"><Input id="chain-pass" type="password" autoComplete="new-password" value={chain.password ?? ""} onChange={(e) => set({ password: e.target.value })} /></Field>
        </div>
      )}
      {chain.kind === "node" &&
        (exits.length === 0 ? (
          <State empty="没有可作出口的节点" detail="出口需要另一台 VPS 上的 VLESS / VMess / Trojan 节点。" />
        ) : (
          <Field label="出口节点">
            <Select value={chain.node_id ? String(chain.node_id) : ""} onValueChange={(v) => set({ node_id: Number(v) })}>
              <SelectTrigger aria-label="出口节点"><SelectValue>{exits.find((n) => n.id === chain.node_id)?.name ?? "请选择"}</SelectValue></SelectTrigger>
              <SelectContent>
                {exits.map((n) => (
                  <SelectItem key={n.id} value={String(n.id)}>{n.name}（{n.gate_name}）</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </Field>
        ))}
      {chain.kind !== "none" && <Notice role="status">保存前由入口 VPS 实测出口可用；流量只在入口计量一次。</Notice>}
    </div>
  );
}
