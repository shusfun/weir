// VPS 与节点：每台 VPS 一张卡片，最上面是在不在线，直接显示主机观测；它上面的每个节点一行，点开看细节；
// 「＋ 协议」直接在这台 VPS 上建节点。VPS 用 SSH 一次装好，或复制安装命令自己执行。
import { useState } from "react";
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input } from "@weir/ui";
import { api } from "../api";
import { useFormAction } from "../lib/action";
import { field, useConsoleContext } from "../lib/console-context";
import { type Gate } from "../lib/core";

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
        <DialogTitle>编辑 {gate.name}</DialogTitle>
        <DialogDescription>改名称与公网地址不会重启内核；客户端更新订阅后生效。</DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <Field label="名称"><Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} /></Field>
          <Field
            label="公网地址"
            name="public_host"
            hint={
              gate.public_host_hint && !host ? (
                <span>
                  本机识别 IP 为 <code className="font-mono">{gate.public_host_hint}</code>（
                  <button type="button" className="weir-press cursor-pointer border-0 bg-transparent p-0 font-semibold text-[var(--weir-accent-text)] hover:underline" onClick={() => setHost(gate.public_host_hint ?? "")}>点击填入</button>
                  ）；域名开了 CDN 代理时请改填直连 IP。
                </span>
              ) : "填写公网 IP 或直连域名；域名开启 CDN 代理时节点端口无法直连。"
            }
          >
            <Input value={host} placeholder={gate.public_host_hint} onChange={(e) => setHost(e.target.value)} />
          </Field>
          <DialogFooter error={save.error}>
            <Button type="button" variant="ghost" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy} busy={save.busy} busyText="正在保存…">保存</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
