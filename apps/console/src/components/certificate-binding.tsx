import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import {
  Button,
  Card,
  CardContent,
  Label,
  Input,
  Checkbox,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@migate/ui";
import type { Device, Resource, Task } from "../lib/models";
import { date } from "../lib/models";
import { api } from "../api";
import { Status } from "./feedback";

export function CertificateBinding({
  device,
  deviceId,
  certificates,
  inbounds,
  tasks,
}: {
  device?: Device;
  deviceId: number;
  certificates: Resource[];
  inbounds: Resource[];
  tasks: Task[];
}) {
  const cache = useQueryClient();
  const [open, O] = useState(false),
    [cert, C] = useState(""),
    [query, Q] = useState(""),
    [selected, S] = useState<Resource[]>([]),
    [confirm, F] = useState(false),
    [busy, B] = useState(false),
    [error, E] = useState(""),
    [notice, N] = useState("");
  const capable = (
    device?.metadata.capabilities as string[] | undefined
  )?.includes("certificate.bind.v1");
  const eligible = inbounds.filter(
    (r) =>
      r.data.security === "tls" ||
      (r.data.protocol === "trojan" && r.data.security !== "reality") ||
      ["hysteria2", "tuic"].includes(String(r.data.protocol)),
  );
  const certName = (id: number) =>
    String(
      certificates.find((r) => Number(r.data.id) === id)?.data.name ||
        (id ? `证书 #${id}` : "未绑定"),
    );
  const pending = tasks.some(
    (t) =>
      t.device_id === deviceId &&
      ["pending", "running"].includes(t.status) &&
      (t.operation.startsWith("inbound.") ||
        t.operation.startsWith("certificate.")),
  );
  const valid = certificates.filter(
    (r) =>
      Date.parse(String(r.data.not_before)) <= Date.now() &&
      Date.parse(String(r.data.not_after)) > Date.now(),
  );
  async function submit() {
    B(true);
    E("");
    try {
      const result = await api(`/admin/devices/${deviceId}/tasks`, "POST", {
        operation: "certificate.bind",
        confirm: true,
        payload: {
          certificate_id: Number(cert),
          inbounds: selected.map((r) => ({
            id: Number(r.data.id),
            previous_certificate_id: Number(r.data.certificate_id ?? 0),
          })),
        },
      });
      N(`绑定任务 #${result.id} 已提交，等待设备整体校验与回执。`);
      O(false);
      F(false);
      await cache.invalidateQueries({ queryKey: ["tasks"] });
    } catch (e) {
      E(String(e));
    } finally {
      B(false);
    }
  }
  return (
    <Card>
      <CardContent className="stack certificate-binding-panel">
        <div className="section-heading">
          <div>
            <h3>入站证书绑定</h3>
            <p>
              批量更换所选 TLS
              入站的证书。全部校验通过才应用；不会更改入站端口或用户凭据。
            </p>
          </div>
          <Button
            variant="outline"
            disabled={!capable || pending || !eligible.length || !valid.length}
            onClick={() => {
              S([]);
              C("");
              Q("");
              E("");
              F(false);
              O(true);
            }}
          >
            批量绑定证书
          </Button>
        </div>
        {!capable && (
          <p className="notice">请升级 Agent 以使用证书批量绑定。</p>
        )}
        {pending && (
          <p className="notice">入站或证书任务执行中，请等待回执后操作。</p>
        )}
        {!eligible.length && (
          <p className="muted">
            暂无可绑定的 TLS 入站；REALITY 入站使用独立密钥。
          </p>
        )}
        {!!eligible.length && !valid.length && (
          <p className="notice">
            先导入仍有效的证书，或等待 DNS 签发配置完成。
          </p>
        )}
        <div className="certificate-binding-summary">
          {certificates.map((c) => (
            <div className="list-row" key={c.id}>
              <div>
                <strong>{String(c.data.name || `证书 #${c.data.id}`)}</strong>
                <small>到期 {date(String(c.data.not_after ?? ""))}</small>
              </div>
              <span>
                {
                  inbounds.filter(
                    (r) => Number(r.data.certificate_id) === Number(c.data.id),
                  ).length
                }{" "}
                个入站
              </span>
            </div>
          ))}
        </div>
        {notice && (
          <p role="status" className="notice">
            {notice}
          </p>
        )}
        {tasks
          .filter(
            (t) =>
              t.device_id === deviceId && t.operation === "certificate.bind",
          )
          .slice(0, 3)
          .map((t) => (
            <div key={t.id} className="list-row">
              <span>
                绑定任务 #{t.id} · {date(t.created_at)}
              </span>
              <Status value={t.status} />
            </div>
          ))}
        <Dialog
          open={open}
          onOpenChange={(v) => {
            if (!busy) O(v);
          }}
        >
          <DialogContent className="business-dialog certificate-binding-dialog">
            <DialogTitle>批量绑定证书</DialogTitle>
            <DialogDescription>
              选择同一 Gate 已生效的 TLS
              入站；提交后等待任务回执，失败保留原配置。
            </DialogDescription>
            <div className="stack">
              <div className="field">
                <Label>目标证书</Label>
                <Select
                  value={cert}
                  onValueChange={(v) => {
                    C(String(v));
                    F(false);
                  }}
                  disabled={confirm || busy}
                >
                  <SelectTrigger aria-label="目标证书">
                    <SelectValue placeholder="选择有效证书">
                      {cert ? certName(Number(cert)) : "选择有效证书"}
                    </SelectValue>
                  </SelectTrigger>
                  <SelectContent>
                    {valid.map((c) => (
                      <SelectItem key={c.id} value={String(c.data.id)}>
                        {certName(Number(c.data.id))} ·{" "}
                        {date(String(c.data.not_after))}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {!confirm && (
                <div className="field">
                  <Label htmlFor="certificate-inbound-search">
                    搜索 TLS 入站
                  </Label>
                  <Input
                    id="certificate-inbound-search"
                    value={query}
                    onChange={(e) => Q(e.target.value)}
                    placeholder="名称、协议或端口"
                  />
                </div>
              )}
              <div className="certificate-binding-list">
                {(confirm
                  ? selected
                  : eligible.filter((r) =>
                      [r.data.remark, r.data.protocol, r.data.port]
                        .join(" ")
                        .toLowerCase()
                        .includes(query.toLowerCase()),
                    )
                ).map((r) => (
                  <label className="certificate-binding-target" key={r.id}>
                    <Checkbox
                      aria-label={`选择入站 ${r.data.remark || r.data.id}`}
                      checked={selected.some((v) => v.id === r.id)}
                      disabled={confirm || busy}
                      onCheckedChange={(v) =>
                        S((old) =>
                          v ? [...old, r] : old.filter((x) => x.id !== r.id),
                        )
                      }
                    />
                    <span>
                      <strong>
                        {String(r.data.remark || `入站 #${r.data.id}`)}
                      </strong>
                      <small>
                        {String(r.data.protocol)} · {String(r.data.port)} ·{" "}
                        {String(r.data.tls_sni || "未配置 SNI")}
                      </small>
                      <small>
                        {certName(Number(r.data.certificate_id ?? 0))}
                        {confirm ? ` → ${certName(Number(cert))}` : ""}
                      </small>
                    </span>
                  </label>
                ))}
              </div>
              <p className="muted">
                已选 {selected.length} 个，最多 100
                个；域名、有效期及当前绑定由中心和 Gate 双重检查。
              </p>
              {error && (
                <p role="alert" className="error-message">
                  {error}
                </p>
              )}
            </div>
            <DialogFooter>
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => (confirm ? F(false) : O(false))}
              >
                {confirm ? "返回修改" : "取消"}
              </Button>
              <Button
                disabled={
                  busy || !cert || !selected.length || selected.length > 100
                }
                onClick={() => (confirm ? void submit() : F(true))}
              >
                {busy ? "正在提交…" : confirm ? "确认下发绑定任务" : "核对绑定"}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  );
}
