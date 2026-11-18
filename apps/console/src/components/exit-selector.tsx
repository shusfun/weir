import { ExitSummary, NodeSummary } from "./node-summary";
import { useEffect, useState } from "react";
import { ChoiceCard, Field, Input, Notice, Radio, RadioGroup, Segmented, Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@weir/ui";
import { exitChoices, useExitCatalog } from "../lib/exits";
import { ExitFields } from "./chain-exit";
import { State } from "./feedback";

type TopMode = "direct" | "relay" | "external";

export function ExitSelector(props: Parameters<typeof ExitFields>[0] & { gateId: number; exitId?: number; onExitId: (id: number | undefined) => void; ownerUserId?: number }) {
  const { gateId, exitId, onExitId, ownerUserId, ...fields } = props;
  const exits = useExitCatalog(fields.admin || !!ownerUserId);
  const items = exitChoices(exits.data?.items ?? [], gateId, ownerUserId ?? (fields.admin ? null : undefined));
  const currentSaved = exitId ? items.find((e) => e.id === exitId) : undefined;
  const externalSaved = items.filter((e) => e.kind !== "freedom" && e.kind !== "blackhole" && e.kind !== "node");

  const deriveMode = (): TopMode => {
    if (currentSaved) {
      if (currentSaved.kind === "freedom") return "direct";
      if (currentSaved.kind === "node") return "relay";
      return "external";
    }
    if (exitId) return "external";
    if (fields.value.kind === "node") return "relay";
    if (fields.value.kind === "freedom") return fields.admin ? "direct" : "external";
    return "external";
  };

  const [mode, setMode] = useState<TopMode>(deriveMode);
  const [extSource, setExtSource] = useState<"saved" | "new">(exitId && currentSaved?.kind !== "freedom" && currentSaved?.kind !== "node" ? "saved" : "new");

  // 编辑已有节点时，目录异步加载完校正一次初始模式（不改表单值，不触发未保存提示）
  useEffect(() => {
    if (!currentSaved) return;
    if (currentSaved.kind === "freedom") setMode("direct");
    else if (currentSaved.kind === "node") setMode("relay");
    else {
      setMode("external");
      setExtSource("saved");
    }
  }, [currentSaved?.id, currentSaved?.kind]);

  const selectMode = (next: TopMode) => {
    setMode(next);
    if (next === "direct") {
      onExitId(undefined);
      fields.onChange({ kind: "freedom" });
      return;
    }
    if (next === "relay") {
      onExitId(undefined);
      fields.onChange({ kind: "node", node_id: fields.nodes[0]?.id, remark: fields.value.remark });
      return;
    }
    if (externalSaved.length > 0) {
      setExtSource("saved");
      onExitId(externalSaved[0].id);
    } else {
      setExtSource("new");
      onExitId(undefined);
      if (fields.value.kind === "freedom" || fields.value.kind === "node") {
        fields.onChange({ kind: "socks5", remark: fields.value.remark });
      }
    }
  };

  const selectedRelayNodeId = currentSaved?.kind === "node" ? currentSaved.node_id : fields.value.node_id;

  const modeCards: [TopMode, string, string][] = [
    ...(fields.admin ? [["direct", "直接出站", "从当前 VPS 直接访问目标网站"] as [TopMode, string, string]] : []),
    ["relay", "经另一台 VPS 中转", "转发给其他 VPS 节点落地（套娃中转）"],
    ["external", "经外部代理或链接", "复用已存出口，或填 SOCKS5 / 节点链接 / 订阅源"],
  ];

  return (
    <div className="stack">
      <Field label="出口方式">
        <RadioGroup aria-label="出口方式" value={mode} onValueChange={(v) => selectMode(String(v) as TopMode)} className={`grid gap-2 ${modeCards.length === 3 ? "sm:grid-cols-3" : "sm:grid-cols-2"}`}>
          {modeCards.map(([v, title, desc]) => (
            <ChoiceCard key={v} selected={mode === v} render={<label />} className="min-w-0">
              <span className="flex items-center gap-2">
                <Radio value={v} />
                <strong className="truncate">{title}</strong>
              </span>
              <small className="text-[var(--weir-ink-3)]">{desc}</small>
            </ChoiceCard>
          ))}
        </RadioGroup>
      </Field>

      {mode === "relay" && (
        fields.nodes.length === 0 ? (
          <State empty="没有可作落地的其他 VPS 节点" detail={fields.nodeHint} />
        ) : (
          <div className="stack">
            <Field label="落地 VPS 节点" name="exitNode" required>
              <Select
                required
                value={selectedRelayNodeId ? String(selectedRelayNodeId) : ""}
                onValueChange={(v) => {
                  onExitId(undefined);
                  fields.onChange({ kind: "node", node_id: Number(v), remark: fields.value.remark });
                }}
              >
                <SelectTrigger>
                  <SelectValue>
                    {fields.nodes.find((n) => n.id === selectedRelayNodeId)
                      ? <NodeSummary node={fields.nodes.find((n) => n.id === selectedRelayNodeId)!} compact />
                      : "请选择另一台 VPS 上的节点"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {fields.nodes.map((n) => (
                    <SelectItem key={n.id} value={String(n.id)}><NodeSummary node={n} /></SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
            <Field label="备注（可选）" name="remark" hint="会显示在订阅的节点名里">
              <Input
                maxLength={100}
                value={currentSaved?.kind === "node" ? (currentSaved.remark ?? "") : (fields.value.remark ?? "")}
                onChange={(e) => {
                  onExitId(undefined);
                  fields.onChange({ kind: "node", node_id: selectedRelayNodeId, remark: e.target.value });
                }}
              />
            </Field>
            <Notice role="status">由入口 VPS 实测连通后生效；用户流量只在入口计量一次。</Notice>
          </div>
        )
      )}

      {mode === "external" && (
        <div className="stack">
          {externalSaved.length > 0 && (
            <div className="flex flex-wrap items-center justify-between gap-2">
              <Segmented
                aria-label="代理来源"
                size="sm"
                value={extSource}
                onValueChange={(v) => {
                  const next = v as "saved" | "new";
                  setExtSource(next);
                  if (next === "saved") {
                    onExitId(externalSaved[0]?.id);
                  } else {
                    onExitId(undefined);
                    if (fields.value.kind === "freedom" || fields.value.kind === "node") {
                      fields.onChange({ kind: "socks5", remark: fields.value.remark });
                    }
                  }
                }}
                options={[["saved", `复用已存代理（${externalSaved.length}）`], ["new", "新填一个"]] as const}
              />
            </div>
          )}
          {extSource === "saved" && externalSaved.length > 0 ? (
            <Field label="已存代理" name="exit_id" required>
              <Select required value={exitId ? String(exitId) : ""} onValueChange={(v) => onExitId(Number(v))}>
                <SelectTrigger>
                  <SelectValue>
                    {externalSaved.find((e) => e.id === exitId)
                      ? <ExitSummary exit={externalSaved.find((e) => e.id === exitId)!} />
                      : "请选择出口"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {externalSaved.map((e) => (
                    <SelectItem key={e.id} value={String(e.id)}><ExitSummary exit={e} expanded /></SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </Field>
          ) : (
            <ExitFields {...fields} hideNodeOption />
          )}
        </div>
      )}
    </div>
  );
}
