// VPS 与节点：每台 VPS 一张卡片，最上面是在不在线，直接显示主机观测；它上面的每个节点一行，点开看细节；
// 「＋ 协议」直接在这台 VPS 上建节点。VPS 用 SSH 一次装好，或复制安装命令自己执行。
import { useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { useQuery } from "../lib/query";
import {
  Badge, Button, Card, CardContent, Chip, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input,
  Notice, PageHeader, Readout, SkeletonCards, StatusLamp, SwitchField, useToast, useConfirm, type LampTone,
} from "@weir/ui";
import { Pencil, Plus, Route, Server, Sparkles, Trash2 } from "lucide-react";
import { openDsh } from "./dsh";
import { ObjectLink, dataObject, useDialogParam } from "./object-link";
import { exitKindName, probeText } from "./chain-exit";
import { NodeAccess } from "./node-access";

import { GateDetail, pct, size } from "./gate-detail";
import { api } from "../api";
import { useFormAction, useRowAction } from "../lib/action";
import { WaitText } from "./wait";
import { GateUpdateLine, GateVersion } from "./gate-version";
import { State } from "./feedback";
import { InviteGate } from "./invite-gate";
import { CertManager } from "./cert-manager";
import { NodeWizard } from "./node-wizard";
import { date } from "../lib/models";
import { rateText } from "../lib/linkmap";
import { field, useConsoleContext } from "../lib/console-context";
import { nodeStatus, protocolNames, relativeTime, type Gate, type NodeItem } from "../lib/core";
import { GettingStarted } from "./getting-started";

export function GateEdit({ gate, close, saved }: { gate: Gate; close: () => void; saved: () => void }) {
  const [name, setName] = useState(gate.name);
  const [host, setHost] = useState(gate.public_host);
  const save = useFormAction(() => api("/admin/gates/" + gate.id, "PATCH", { name: name.trim(), public_host: host.trim() }), {
    fields: { invalid_public_host: "public_host", public_host_not_public: "public_host", public_host_unresolved: "public_host", public_host_lookup_failed: "public_host" },
    onSuccess: saved,
  });
  useConsoleContext({ object: { type: "gate", name: gate.name, id: gate.id }, dialog: { title: "编辑 VPS" }, form: [field("name", "名称", name), field("public_host", "公网地址", host)] });
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="md">
        <DialogTitle>编辑 VPS</DialogTitle>
        <DialogDescription>公网地址会写进订阅；改动后用户更新订阅即可生效。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <Field label="名称"><Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <Field label="公网地址" name="public_host" hint={gate.public_host_hint && !host ? `IP 或解析到这台 VPS 的域名，粘贴网址也可以；本机网卡上的公网 IP 是 ${gate.public_host_hint}。域名开了 CDN 代理时节点连不上，请填 IP。` : "IP 或解析到这台 VPS 的域名，粘贴网址也可以；域名开了 CDN 代理时节点连不上，请填 IP。"}>
            <Input value={host} placeholder={gate.public_host_hint} onChange={(e) => setHost(e.target.value)} />
          </Field>
          {gate.public_host_hint && !host && <Button type="button" size="sm" variant="outline" onClick={() => setHost(gate.public_host_hint ?? "")}>填入 {gate.public_host_hint}</Button>}
          <DialogFooter error={save.error}>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
