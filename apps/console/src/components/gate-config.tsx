import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Field, Notice, SectionHeader,
  Button,
  Input,
  Label,
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
  Textarea,
  Switch,
} from "@migate/ui";
import { GateACME } from "./gate-acme";
import { DNSCleanupPanel } from "./dns-cleanup";
import { CertificateHistoryPanel } from "./certificate-history";
import { CertificateBinding } from "./certificate-binding";
import { RoutingOrder } from "./routing-order";
import { OutboundTransport } from "./outbound-transport";
import { OutboundTools } from "./outbound-tools";
import { ProxyCatalog } from "./proxy-catalog";
import { Plus } from "lucide-react";
import { api } from "../api";
import type { Device, Resource, Task, Grant } from "../lib/models";
import { date } from "../lib/models";
import { State, Status } from "./feedback";
type Data = Record<string, any>;
const names: Record<string, string> = {
  inbounds: "入站",
  outbounds: "出站",
  routing: "路由",
  certificates: "证书",
};
const opNames: Record<string, string> = {
  inbounds: "inbound",
  outbounds: "outbound",
  routing: "routing",
  certificates: "certificate",
};
export function GateConfig({
  deviceId,
  tab,
  resources,
  tasks,
  device,
  grants,
}: {
  deviceId: number;
  tab: string;
  resources: Record<string, Resource[]>;
  tasks: Task[];
  device: Device;
  grants: Grant[];
}) {
  const cache = useQueryClient();
  const [editor, ED] = useState<Data | null>(null);
  const [removing, RM] = useState<Data | null>(null);
  const [error, E] = useState("");
  const [busy, B] = useState(false);
  const [notice, N] = useState("");
  const [confirm, C] = useState(false);
  const [preview, PV] = useState(false);
  const [clientSearch, CS] = useState("");
  const kind = "device-" + opNames[tab];
  const outboundTransportSupported = (
    device.metadata.capabilities as string[] | undefined
  )?.includes("outbound.transport.v1");
  const inboundEnabledSupported = (
    device?.metadata.capabilities as string[] | undefined
  )?.includes("inbound.enabled.v1");
  const enabledSupported = (
    device.metadata.capabilities as string[] | undefined
  )?.includes("resource.enabled.v1");
  const poolList = useQuery({
    queryKey: ["outbound-pools", deviceId],
    queryFn: () =>
      api(`/admin/devices/${deviceId}/pools`) as Promise<{
        items: { id: number; priority: number; prepend: boolean }[];
      }>,
    enabled: tab === "outbounds",
  });
  const outboundPosition = (r: Resource) => {
    const pool = poolList.data?.items.find(
      (p) => p.id === Number(r.data.subscription_id),
    );
    return r.data.source === "subscription"
      ? [pool?.prepend ? 0 : 2, pool?.priority ?? 0, Number(r.data.sort ?? 0)]
      : [1, Number(r.data.sort ?? 0), 0];
  };
  const rows = [...(resources[kind] ?? [])].sort((a, b) =>
    tab === "outbounds"
      ? (() => {
          const left = outboundPosition(a),
            right = outboundPosition(b);
          return (
            left[0] - right[0] ||
            left[1] - right[1] ||
            left[2] - right[2] ||
            Number(a.data.id) - Number(b.data.id)
          );
        })()
      : tab === "routing"
        ? Number(a.data.sort ?? 0) - Number(b.data.sort ?? 0) ||
          Number(a.data.id) - Number(b.data.id)
        : 0,
  );
  const title = names[tab];
  const label = (r: Data) =>
    String(
      r.remark ??
        r.tag ??
        r.name ??
        (tab === "certificates" ? "证书" : "配置") + " #" + r.id,
    );
  async function submit() {
    B(true);
    E("");
    try {
      const body: Data = removing ? { id: removing.id } : { ...editor };
      for (const key of Object.keys(body))
        if (key.startsWith("has_")) delete body[key];
      delete body.transport_options_unavailable;
      if (!outboundTransportSupported) delete body.transport_options;
      const result = await api(
        "/admin/devices/" + deviceId + "/tasks",
        "POST",
        {
          operation: opNames[tab] + (removing ? ".delete" : ".upsert"),
          payload: body,
          confirm: true,
        },
      );
      N(
        "任务 #" +
          result.id +
          " 已提交，等待设备执行。" +
          (result.resource_id ? " 资源编号 #" + result.resource_id : ""),
      );
      ED(null);
      RM(null);
      C(false);
      await cache.invalidateQueries({ queryKey: ["tasks"] });
      await cache.invalidateQueries({ queryKey: ["device-resources"] });
    } catch (e) {
      E(String(e));
    } finally {
      B(false);
    }
  }
  function create() {
    E("");
    C(false);
    PV(false);
    ED(
      tab === "inbounds"
        ? {
            id: 0,
            remark: "",
            protocol: "vless",
            core: "xray",
            port: 24446,
            public_host: "",
            network: "tcp",
            security: "reality",
            reality_dest: "www.microsoft.com:443",
            reality_server_names: "www.microsoft.com",
            reality_short_id: "",
            reality_private_key: "",
            reality_public_key: "",
            certificate_id: 0,
            tls_sni: "",
          }
        : tab === "outbounds"
          ? {
              id: 0,
              tag: "",
              protocol: "freedom",
              address: "",
              port: 443,
              username: "",
              password: "",
              ...(outboundTransportSupported
                ? { transport_options: { network: "tcp", security: "none" } }
                : {}),
            }
          : tab === "routing"
            ? {
                id: 0,
                inbound_id: 0,
                client_id: 0,
                outbound_id: 0,
                domain: "",
                ip: "",
                sort: 0,
              }
            : { id: 0, name: "", certificate_pem: "", key_pem: "" },
    );
  }
  const field = (
    key: string,
    label: string,
    type = "text",
    required = false,
  ) => (
    <Field label={label} htmlFor={"gate-" + key}>
      <Input
        id={"gate-" + key}
        type={type}
        required={required}
        value={editor?.[key] ?? ""}
        placeholder={
          editor?.["has_" + key] ? "已保存，留空保留原值" : undefined
        }
        onChange={(e) => {
          ED({
            ...editor,
            [key]: type === "number" ? Number(e.target.value) : e.target.value,
          });
          C(false);
        }}
        autoComplete={type === "password" ? "new-password" : "off"}
      />
    </Field>
  );
  const choice = (key: string, label: string, options: string[][]) => (
    <Field label={label}>
      <Select
        value={String(editor?.[key] ?? "")}
        onValueChange={(v) => {
          const patch: Data = { [key]: key.endsWith("_id") ? Number(v) : v };
          if (key === "protocol" && tab === "inbounds") {
            const quic = ["hysteria2", "tuic"].includes(v ?? "");
            patch.core = quic ? "sing-box" : "xray";
            patch.security = quic || v === "trojan" ? "tls" : "reality";
          }
          if (
            key === "protocol" &&
            tab === "outbounds" &&
            outboundTransportSupported
          ) {
            patch.transport_options = {
              network: "tcp",
              security: [
                "trojan",
                "https",
                "hysteria2",
                "tuic",
                "anytls",
                "shadowtls",
              ].includes(v ?? "")
                ? "tls"
                : "none",
            };
            patch.transport_options_unavailable = false;
          }
          if (key === "inbound_id" && tab === "routing") patch.client_id = 0;
          ED({ ...editor, ...patch });
          C(false);
        }}
      >
        <SelectTrigger aria-label={label}>
          <SelectValue>
            {options.find(([v]) => v === String(editor?.[key] ?? ""))?.[1] ??
              "请选择" + label}
          </SelectValue>
        </SelectTrigger>
        <SelectContent>
          {options.map(([v, n]) => (
            <SelectItem value={v} key={v}>
              {n}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </Field>
  );
  const pending = tasks
    .filter(
      (t) =>
        t.device_id === deviceId && t.operation.startsWith(opNames[tab] + "."),
    )
    .slice(0, 8);
  const certOptions = (resources["device-certificate"] ?? []).map((r) => [
    String(r.data.id),
    String(r.data.name || "证书 #" + r.data.id) +
      " · " +
      date(String(r.data.not_after ?? "")),
  ]);
  return (
    <div className="stack">
      <SectionHeader title={<>{title}配置</>} description="以下配置已由设备回执确认。修改将以异步任务应用。" actions={<Button onClick={create}>
          <Plus size={15} />
          {tab === "certificates" ? "导入证书" : "新增" + title}
        </Button>} />
      {tab === "certificates" && (
        <div className="stack">
          <CertificateBinding
            device={device}
            deviceId={deviceId}
            certificates={resources["device-certificate"] ?? []}
            inbounds={resources["device-inbound"] ?? []}
            tasks={tasks}
          />
          <GateACME
            deviceId={deviceId}
            certificates={resources["device-certificate"] ?? []}
          />
          <DNSCleanupPanel deviceId={deviceId} />
          <CertificateHistoryPanel
            device={device}
            tasks={tasks}
            certificates={resources["device-certificate"] ?? []}
          />
        </div>
      )}
      {tab === "routing" && (
        <RoutingOrder
          deviceId={deviceId}
          rows={rows}
          tasks={tasks}
          outbounds={resources["device-outbound"] ?? []}
        />
      )}
      {tab === "outbounds" && (
        <>
          <ProxyCatalog device={device} />
          <OutboundTools device={device} rows={rows} tasks={tasks} />
        </>
      )}
      {notice && (
        <Notice role="status">
          {notice}
        </Notice>
      )}
      {rows.length ? (
        rows.map((r, index) => (
          <article className="inventory-item" key={r.id}>
            <div>
              <h3>
                {tab === "routing"
                  ? `${index + 1}. ${r.data.domain || r.data.ip || r.data.protocol || "所有流量"}`
                  : label(r.data)}
              </h3>
              <p>
                {[
                  r.data.protocol,
                  r.data.port ? "端口 " + r.data.port : null,
                  r.data.domain,
                  r.data.not_after
                    ? "到期 " + date(String(r.data.not_after))
                    : null,
                ]
                  .filter(Boolean)
                  .join(" · ") || "已应用配置"}
              </p>
              <small>
                资源 #{String(r.data.id)}
                {tab === "routing"
                  ? ` · 优先级 ${r.data.sort ?? 0} · 出站 ${resources["device-outbound"]?.find((o) => o.data.id === r.data.outbound_id)?.data.tag ?? "未知"}`
                  : ""}
              </small>
            </div>
            <div className="actions">
              <Status
                value={
                  r.data.enabled === false &&
                  ["inbounds", "outbounds", "routing"].includes(tab)
                    ? "disabled"
                    : "applied"
                }
              />
              <Button
                variant="outline"
                size="sm"
                disabled={
                  tab === "outbounds" && r.data.source === "subscription"
                }
                onClick={() => {
                  E("");
                  C(false);
                  PV(false);
                  ED(
                    tab === "certificates"
                      ? {
                          id: r.data.id,
                          name: r.data.name ?? "",
                          certificate_pem: r.data.certificate_pem ?? "",
                          key_pem: "",
                        }
                      : {
                          ...r.data,
                          ...((tab === "inbounds"
                            ? inboundEnabledSupported
                            : enabledSupported) &&
                          ["inbounds", "outbounds", "routing"].includes(tab)
                            ? { enabled: r.data.enabled !== false }
                            : {}),
                        },
                  );
                }}
              >
                {tab === "certificates" ? "更新证书" : "编辑"}
              </Button>
              <Button
                variant="destructive"
                size="sm"
                disabled={
                  tab === "outbounds" && r.data.source === "subscription"
                }
                onClick={() => {
                  E("");
                  RM(r.data);
                }}
              >
                删除
              </Button>
            </div>
          </article>
        ))
      ) : (
        <State empty={"暂无已生效" + title} />
      )}
      {pending.length > 0 && (
        <div>
          <h3>相关任务</h3>
          {pending.map((t) => (
            <div className="list-row" key={t.id}>
              <div>
                <strong>
                  #{t.id} · {t.operation}
                </strong>
                <small>
                  {date(t.created_at)} · 版本 {t.revision}
                </small>
                {t.status === "failed" && (
                  <small role="alert">
                    {String(
                      (t.result as Data)?.message ?? "执行失败，请查看任务详情",
                    )}
                  </small>
                )}
              </div>
              <Status value={t.status} />
            </div>
          ))}
        </div>
      )}
      <Dialog
        open={!!editor || !!removing}
        onOpenChange={(v) => {
          if (!v && !busy) {
            ED(null);
            RM(null);
          }
        }}
      >
        <DialogContent size="xl">
          <DialogTitle>
            {removing ? "删除" : editor?.id ? "修改" : "新增"}
            {title}
          </DialogTitle>
          <DialogDescription>
            {removing
              ? "设备会检查资源是否仍被引用。引用中的配置删除会失败并保留原配置。"
              : "保存将创建任务；配置验证或应用失败时保留原运行配置。空白密钥保留已有值。"}
          </DialogDescription>
          <form
            className="stack"
            onSubmit={(e) => {
              e.preventDefault();
              if (!removing && !confirm) {
                C(true);
                return;
              }
              void submit();
            }}
          >
            {editor && (
              <>
                {tab === "inbounds" && (
                  <>
                    {field("remark", "入站名称", "text", true)}
                    <div className="two">
                      {choice("protocol", "入站协议", [
                        ["vless", "VLESS"],
                        ["vmess", "VMess"],
                        ["trojan", "Trojan"],
                        ["hysteria2", "Hysteria 2"],
                        ["tuic", "TUIC"],
                      ])}
                      {field("port", "监听端口", "number", true)}
                    </div>
                    {field("public_host", "客户端连接地址", "text", true)}
                    {!["hysteria2", "tuic"].includes(editor.protocol) &&
                      choice("network", "传输方式", [
                        ["tcp", "TCP"],
                        ["ws", "WebSocket"],
                        ["grpc", "gRPC"],
                        ["xhttp", "XHTTP"],
                      ])}
                    {editor.network === "ws" && (
                      <>
                        {field("ws_path", "WebSocket 路径")}
                        {field("ws_host", "WebSocket Host")}
                      </>
                    )}
                    {editor.network === "grpc" &&
                      field("grpc_service_name", "gRPC 服务名")}
                    {editor.network === "xhttp" && (
                      <>
                        {field("xhttp_path", "XHTTP 路径")}
                        {field("xhttp_mode", "XHTTP 模式")}
                      </>
                    )}
                    {!["hysteria2", "tuic", "trojan"].includes(
                      editor.protocol,
                    ) &&
                      choice("security", "传输安全", [
                        ["reality", "Reality"],
                        ["tls", "TLS"],
                        ["none", "无 TLS"],
                      ])}
                    {editor.security === "reality" && (
                      <>
                        {field(
                          "reality_dest",
                          "Reality 目标（主机:端口）",
                          "text",
                          true,
                        )}
                        {field(
                          "reality_server_names",
                          "Reality SNI",
                          "text",
                          true,
                        )}
                        {field("reality_short_id", "Reality Short ID")}
                        <p>
                          Reality 密钥在中心安全生成；已有密钥不会回传浏览器。
                        </p>
                        {editor.id &&
                          field(
                            "reality_public_key",
                            "Reality 公钥",
                            "text",
                            true,
                          )}
                      </>
                    )}
                    {editor.security === "tls" && (
                      <>
                        {choice("certificate_id", "绑定证书", [
                          ["0", "请选择证书"],
                          ...certOptions,
                        ])}
                        {field("tls_sni", "TLS 域名 SNI", "text", true)}
                        {field("tls_alpn", "TLS ALPN（逗号分隔）")}
                      </>
                    )}
                    {editor.protocol === "hysteria2" && (
                      <>
                        {field("hy2_up_mbps", "上行带宽 Mbps", "number")}
                        {field("hy2_down_mbps", "下行带宽 Mbps", "number")}
                        {choice("hy2_obfs", "混淆", [
                          ["", "无混淆"],
                          ["salamander", "Salamander"],
                        ])}
                        {editor.hy2_obfs === "salamander" &&
                          field(
                            "hy2_obfs_password",
                            "混淆密码",
                            "password",
                            !editor.id,
                          )}
                      </>
                    )}
                    {editor.protocol === "tuic" &&
                      choice("tuic_congestion_control", "拥塞控制", [
                        ["bbr", "BBR"],
                        ["cubic", "Cubic"],
                        ["new_reno", "New Reno"],
                      ])}
                    <Notice tone="warn">
                      端口不能为
                      80、443、8443，也不能占用其他服务。节点客户端凭据由授权流程独立分配。
                    </Notice>
                  </>
                )}
                {["inbounds", "outbounds", "routing"].includes(tab) &&
                  (tab === "inbounds"
                    ? inboundEnabledSupported
                    : enabledSupported) && (
                    <div className="stack">
                      <div className="actions">
                        <Switch
                          id="gate-resource-enabled"
                          checked={editor.enabled !== false}
                          onCheckedChange={(enabled) => {
                            ED({ ...editor, enabled });
                            C(false);
                          }}
                        />
                        <Label htmlFor="gate-resource-enabled">
                          启用{title}
                        </Label>
                      </div>
                      <p className="muted">
                        {tab === "inbounds"
                          ? "停用将关闭该入站连接并从用户订阅中移除；保留独立客户端凭据，启用后恢复。设备回执前保留原状态。"
                          : "停用路由后恢复后续规则；停用出站前须先停用引用它的路由。设备回执前保留原状态。"}
                      </p>
                    </div>
                  )}
                {tab === "outbounds" && (
                  <>
                    {field("tag", "出站名称", "text", true)}
                    {choice("protocol", "出站协议", [
                      ["freedom", "直连"],
                      ["blackhole", "拒绝"],
                      ["socks", "SOCKS5"],
                      ["http", "HTTP"],
                      ["https", "HTTPS"],
                      ["vless", "VLESS"],
                      ["vmess", "VMess"],
                      ["trojan", "Trojan"],
                      ["shadowsocks", "Shadowsocks"],
                      ...((
                        device.metadata.capabilities as string[] | undefined
                      )?.includes("outbound.pool.v1")
                        ? [
                            ["hysteria2", "Hysteria 2 · sing-box"],
                            ["tuic", "TUIC · sing-box"],
                            ["anytls", "AnyTLS · sing-box"],
                            ["shadowtls", "ShadowTLS 3 · sing-box"],
                          ]
                        : []),
                    ])}
                    {!["freedom", "blackhole"].includes(editor.protocol) && (
                      <>
                        {field("address", "远端服务器", "text", true)}
                        {field("port", "远端端口", "number", true)}
                        {editor.protocol === "shadowsocks"
                          ? choice("username", "Shadowsocks 加密方法", [
                              ["aes-128-gcm", "AES-128-GCM"],
                              ["aes-256-gcm", "AES-256-GCM"],
                              [
                                "chacha20-ietf-poly1305",
                                "ChaCha20-IETF-Poly1305",
                              ],
                              ["2022-blake3-aes-128-gcm", "SS2022 AES-128-GCM"],
                              ["2022-blake3-aes-256-gcm", "SS2022 AES-256-GCM"],
                            ])
                          : field(
                              "username",
                              ["vless", "vmess"].includes(editor.protocol)
                                ? "出站 UUID"
                                : "出站用户名",
                            )}
                        {!["vless", "vmess"].includes(editor.protocol) &&
                          field("password", "连接密码", "password")}
                        {outboundTransportSupported &&
                          editor.transport_options && (
                            <OutboundTransport
                              trustSupported={
                                (
                                  device.metadata?.capabilities as
                                    string[] | undefined
                                )?.includes("outbound.trust.v1") ?? false
                              }
                              protocol={editor.protocol}
                              value={editor.transport_options}
                              change={(next) => {
                                ED({ ...editor, transport_options: next });
                                C(false);
                              }}
                            />
                          )}
                        {!outboundTransportSupported && (
                          <Notice>
                            设备尚未支持新版传输表单；升级 Agent 后可编辑 TLS /
                            WebSocket / gRPC / Reality。
                          </Notice>
                        )}
                        {editor.transport_options_unavailable && (
                          <Notice>
                            历史高级设置含未适配字段，已保留原值；请先核对迁移差异再修改协议。
                          </Notice>
                        )}
                      </>
                    )}
                  </>
                )}
                {tab === "routing" && (
                  <>
                    {choice("inbound_id", "源入站", [
                      ["0", "全部入站"],
                      ...(resources["device-inbound"] ?? []).map((r) => [
                        String(r.data.id),
                        String(r.data.remark ?? r.data.id),
                      ]),
                    ])}
                    <Field label="搜索授权用户" htmlFor="route-client-search">
                      <Input
                        id="route-client-search"
                        value={clientSearch}
                        onChange={(e) => CS(e.target.value)}
                        placeholder="邮箱或授权名称"
                      />
                    </Field>
                    {choice("client_id", "匹配客户端授权", [
                      ["0", "此入站的全部客户端"],
                      ...grants
                        .filter(
                          (g) =>
                            g.status === "applied" &&
                            (!editor.inbound_id ||
                              g.inbound_id === Number(editor.inbound_id)) &&
                            ((g.user_email ?? String(g.user_id))
                              .toLowerCase()
                              .includes(clientSearch.toLowerCase()) ||
                              g.id === Number(editor.client_id)),
                        )
                        .map((g) => [
                          String(g.id),
                          `${g.user_email || "未知用户"} · 授权 #${g.id}${g.enabled ? "" : "（已停用）"}`,
                        ]),
                      ...(editor.client_id &&
                      !grants.some(
                        (g) =>
                          g.id === Number(editor.client_id) &&
                          g.status === "applied",
                      )
                        ? [
                            [
                              String(editor.client_id),
                              `授权 #${editor.client_id}（已撤销或未知，请重新选择）`,
                            ],
                          ]
                        : []),
                    ])}
                    {choice("outbound_id", "目标出站", [
                      ["0", "请选择出站"],
                      ...(resources["device-outbound"] ?? []).map((r) => [
                        String(r.data.id),
                        String(r.data.tag ?? r.data.id),
                      ]),
                    ])}
                    {field("domain", "匹配域名（可逗号分隔）")}
                    {field("ip", "匹配 IP / CIDR")}
                    {field("sort", "规则顺序", "number")}
                  </>
                )}
                {tab === "certificates" && (
                  <>
                    {field("name", "证书名称", "text", true)}
                    <Field label="证书链 PEM" htmlFor="certificate-pem"><Textarea
                      id="certificate-pem"
                      mono
                      value={editor.certificate_pem ?? ""}
                      onChange={(e) => {
                        ED({ ...editor, certificate_pem: e.target.value });
                        C(false);
                      }}
                      required
                      rows={6}
                    /></Field>
                    <Field label="私钥 PEM（仅提交，不回显）" htmlFor="certificate-key"><Textarea
                      id="certificate-key"
                      mono
                      value={editor.key_pem ?? ""}
                      onChange={(e) => {
                        ED({ ...editor, key_pem: e.target.value });
                        C(false);
                      }}
                      required
                      rows={6}
                    /></Field>
                    <p>
                      手工证书继续可用。自动签发将使用 DNS-01，真实签发需配置
                      DNS 凭据。
                    </p>
                  </>
                )}
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => PV((v) => !v)}
                >
                  {preview ? "收起配置预览" : "查看配置预览"}
                </Button>
                {preview && (
                  <pre>
                    {JSON.stringify(
                      Object.fromEntries(
                        Object.entries(editor).map(([k, v]) => [
                          k,
                          /(private|password|key_pem|credential)/.test(k)
                            ? "[隐藏]"
                            : v,
                        ]),
                      ),
                      null,
                      2,
                    )}
                  </pre>
                )}
              </>
            )}
            {confirm && (
              <Notice role="status">
                请核对配置后再次确认。应用期间可能重启本项目内核，影响现有连接。
              </Notice>
            )}
            {error && (
              <Notice tone="bad">
                {error}
              </Notice>
            )}
            <DialogFooter>
              <Button
                type="button"
                variant="outline"
                disabled={busy}
                onClick={() => {
                  ED(null);
                  RM(null);
                }}
              >
                取消
              </Button>
              <Button
                type="submit"
                variant={removing ? "destructive" : "default"}
                disabled={busy}
              >
                {busy
                  ? "正在提交…"
                  : removing
                    ? "确认删除"
                    : confirm
                      ? "确认下发任务"
                      : "核对配置"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
}
