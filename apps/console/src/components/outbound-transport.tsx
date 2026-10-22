import { useId } from "react";
import { Field, Notice,
  Input,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  Textarea,
  Switch,
} from "@migate/ui";
type Options = Record<string, any>;

export function OutboundTransport({
  protocol,
  value,
  change,
  trustSupported,
}: {
  trustSupported: boolean;
  protocol: string;
  value: Options;
  change: (next: Options) => void;
}) {
  const id = useId(),
    net = value.network || "tcp",
    security =
      value.security ||
      (["trojan", "https", "hysteria2", "tuic", "anytls", "shadowtls"].includes(
        protocol,
      )
        ? "tls"
        : "none");
  const stream = ["vless", "vmess", "trojan"].includes(protocol);
  const set = (key: string, next: any) => change({ ...value, [key]: next });
  function choice(
    key: string,
    label: string,
    options: [string, string][],
    update?: (v: string) => void,
  ) {
    return (
      <Field label={label} htmlFor={id + key}>
        <Select
          value={String(
            value[key] ??
              (key === "network"
                ? net
                : key === "security"
                  ? security
                  : key === "obfs"
                    ? "none"
                    : key === "congestion_control"
                      ? "default"
                      : ""),
          )}
          onValueChange={(v) => v && (update ? update(v) : set(key, v))}
        >
          <SelectTrigger id={id + key}>
            <SelectValue>
              {options.find(
                ([v]) =>
                  v ===
                  String(
                    value[key] ??
                      (key === "network"
                        ? net
                        : key === "security"
                          ? security
                          : key === "obfs"
                            ? "none"
                            : key === "congestion_control"
                              ? "default"
                              : ""),
                  ),
              )?.[1] ?? String(value[key] ?? "")}
            </SelectValue>
          </SelectTrigger>
          <SelectContent>
            {options.map(([v, name]) => (
              <SelectItem value={v} key={v}>
                {name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </Field>
    );
  }
  function field(key: string, label: string, placeholder?: string) {
    return (
      <Field label={label} htmlFor={id + key}>
        <Input
          id={id + key}
          value={value[key] ?? ""}
          placeholder={placeholder}
          onChange={(e) => set(key, e.target.value)}
        />
      </Field>
    );
  }
  return (
    <section className="stack" aria-label="出站传输设置">
      <h3>传输与安全</h3>
      {["hysteria2", "tuic", "anytls", "shadowtls"].includes(protocol) && (
        <Notice tone="warn">
          此协议仅由 sing-box 执行；Xray 入站不能路由至此出口。
        </Notice>
      )}
      {protocol === "hysteria2" && (
        <>
          {choice(
            "obfs",
            "Hysteria 2 混淆",
            [
              ["none", "不启用"],
              ["salamander", "Salamander"],
            ],
            (v) =>
              change({
                ...value,
                obfs: v === "none" ? undefined : v,
                obfs_password: undefined,
              }),
          )}
          {value.obfs === "salamander" && (
            <Field label="混淆密码" htmlFor={id + "obfs_password"}>
              <Input
                id={id + "obfs_password"}
                type="password"
                value={value.obfs_password ?? ""}
                placeholder="编辑时留空保留原密码"
                onChange={(e) => set("obfs_password", e.target.value)}
              />
            </Field>
          )}
        </>
      )}
      {protocol === "tuic" && (
        <>
          {choice(
            "congestion_control",
            "TUIC 拥塞控制",
            [
              ["default", "默认"],
              ["cubic", "Cubic"],
              ["bbr", "BBR"],
              ["new_reno", "New Reno"],
            ],
            (v) => set("congestion_control", v === "default" ? undefined : v),
          )}
          <label className="list-row">
            启用 TUIC 0-RTT
            <Switch
              aria-label="启用 TUIC 0-RTT"
              checked={!!value.zero_rtt}
              onCheckedChange={(v) => set("zero_rtt", v)}
            />
          </label>
          <p className="muted">0-RTT 可能被重放，仅在了解上游要求时开启。</p>
        </>
      )}
      {["hysteria2", "tuic", "anytls"].includes(protocol) && (
        <p className="muted">
          托管出站保留证书链、SNI
          与有效期校验；仅提供公钥指纹的上游需先取得可信公开证书。
        </p>
      )}
      {stream &&
        choice(
          "network",
          "出站传输方式",
          [
            ["tcp", "TCP"],
            ["ws", "WebSocket"],
            ["grpc", "gRPC"],
          ],
          (network) =>
            change({
              ...value,
              network,
              path: "",
              host: "",
              service_name: "",
              flow: "",
              ...(security === "reality" && network !== "tcp"
                ? {
                    security: "tls",
                    tls: true,
                    reality: false,
                    pbk: "",
                    sid: "",
                  }
                : {}),
            }),
        )}
      {net === "ws" && (
        <>
          {field("path", "出站 WebSocket 路径", "/ws")}
          {field("host", "出站 WebSocket Host", "cdn.example.com")}
        </>
      )}
      {net === "grpc" && field("service_name", "出站 gRPC 服务名")}
      {!["socks", "shadowsocks"].includes(protocol) &&
        choice(
          "security",
          "出站传输安全",
          [
            ...(![
              "trojan",
              "https",
              "hysteria2",
              "tuic",
              "anytls",
              "shadowtls",
            ].includes(protocol)
              ? [["none", "无 TLS"] as [string, string]]
              : []),
            ["tls", "TLS"],
            ...(protocol === "vless" && net === "tcp"
              ? [["reality", "Reality"] as [string, string]]
              : []),
          ],
          (next) =>
            change({
              ...value,
              security: next,
              tls: next === "tls",
              reality: next === "reality",
              sni: "",
              fp: next === "reality" ? "chrome" : "",
              pbk: "",
              sid: "",
              alpn: [],
              allow_insecure: false,
              trusted_certificate: "",
              flow: "",
            }),
        )}
      {security !== "none" && (
        <>
          {field("sni", "出站 TLS SNI", "server.example.com")}
          {!["hysteria2", "tuic", "shadowtls"].includes(protocol) &&
            field("fp", "出站 TLS 指纹（可选）", "chrome")}
          {security === "tls" && (
            <>
              <Field label="出站 ALPN（逗号分隔）" htmlFor={id + "alpn"}>
                <Input
                  id={id + "alpn"}
                  value={(value.alpn ?? []).join(", ")}
                  onChange={(e) =>
                    set(
                      "alpn",
                      e.target.value
                        .split(",")
                        .map((s) => s.trim())
                        .filter(Boolean),
                    )
                  }
                />
              </Field>
              {trustSupported ? (
                <Field label="出站信任证书（可选 PEM）" htmlFor={id + "certificate"}>
                  <Textarea
                    id={id + "certificate"}
                    className="min-h-28 max-h-48 resize-y [field-sizing:fixed] font-mono text-xs"
                    value={value.trusted_certificate ?? ""}
                    onChange={(e) => set("trusted_certificate", e.target.value)}
                    placeholder="-----BEGIN CERTIFICATE-----"
                    rows={4}
                  />
                  <p className="muted">
                    默认使用系统信任链。自签名目标可导入其公开证书；仍校验 SNI
                    与有效期，不支持跳过校验。请通过可信渠道核对证书。
                  </p>
                </Field>
              ) : (
                <Notice>
                  此 Agent 尚不支持自定义信任证书，请先升级设备。
                </Notice>
              )}
            </>
          )}
          {security === "reality" && (
            <>
              {field("pbk", "出站 Reality 公钥")}
              {field("sid", "出站 Reality Short ID")}
            </>
          )}
          {protocol === "vless" &&
            net === "tcp" &&
            choice("flow", "出站 Flow", [
              ["", "标准"],
              ["xtls-rprx-vision", "XTLS Vision"],
            ])}
        </>
      )}
    </section>
  );
}
