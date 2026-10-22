import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { CodeBlock, Field, Notice,
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  Input,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@migate/ui";
import { api } from "../api";
import type { Device } from "../lib/models";
export function DeviceIdentity({
  device,
  devices,
}: {
  device?: Device;
  devices: Device[];
}) {
  const cache = useQueryClient();
  const [open, O] = useState(false);
  const [id, I] = useState("");
  const [code, C] = useState("");
  const [fingerprint, F] = useState("");
  const [password, P] = useState("");
  const [mfa, M] = useState("");
  const [busy, B] = useState(false);
  const [error, E] = useState("");
  const [notice, N] = useState("");
  const selected = devices.find((d) => String(d.id) === id);
  const pending = devices.filter((d) =>
    ["pending", "recovery"].includes(d.status),
  );
  async function submit() {
    B(true);
    E("");
    try {
      if (device)
        await api("/admin/devices/" + device.id + "/revoke", "POST", {});
      else
        await api("/admin/devices/" + id + "/approve", "POST", {
          code,
          fingerprint,
          password,
          mfa_code: mfa,
        });
      C("");
      P("");
      M("");
      F("");
      O(false);
      N(
        device
          ? "设备身份已撤销。原设备凭据不能继续连接中心。"
          : "配对已批准，等待设备首次心跳。",
      );
      await cache.invalidateQueries({ queryKey: ["devices"] });
    } catch (e) {
      E(String(e));
    } finally {
      B(false);
    }
  }
  return (
    <>
      <Button
        variant="outline"
        disabled={!!device && device.status === "revoked"}
        onClick={() => {
          E("");
          O(true);
        }}
      >
        {device ? "撤销设备身份" : "批准设备配对"}
      </Button>
      {notice && (
        <Notice role="status">
          {notice}
        </Notice>
      )}
      <Dialog
        open={open}
        onOpenChange={(v) => {
          if (!busy) {
            O(v);
            if (!v) {
              C("");
              P("");
              M("");
            }
          }
        }}
      >
        <DialogContent size="md">
          <DialogTitle>
            {device ? "撤销设备身份" : "核对并批准配对"}
          </DialogTitle>
          <DialogDescription>
            {device
              ? `${device.name}：撤销后 Agent 无法接收任务或结算预算，现有内核继续按本地预算和到期规则执行。恢复需 VPS 密钥证明及管理员重新批准。`
              : "先在 VPS 执行配对，再逐字核对 VPS 显示的指纹和一次性配对码。"}
          </DialogDescription>
          <form
            className="stack"
            onSubmit={(e) => {
              e.preventDefault();
              void submit();
            }}
          >
            {!device && (
              <>
                <Field label="待配对设备">
                  <Select
                    value={id || null}
                    onValueChange={(v) => {
                      I(v ?? "");
                      C("");
                      F("");
                    }}
                  >
                    <SelectTrigger aria-label="待配对设备">
                      <SelectValue>
                        {selected?.name ?? "请选择设备"}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {pending.map((d) => (
                        <SelectItem key={d.id} value={String(d.id)}>
                          {d.name} ·{" "}
                          {d.status === "recovery" ? "恢复" : "首次配对"}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </Field>
                {!pending.length && (
                  <Notice>
                    暂无配对请求，请先在 VPS 启动 Agent 配对流程。
                  </Notice>
                )}
                {selected && (
                  <>
                    <p>中心收到的公钥指纹</p>
                    <CodeBlock>{selected.fingerprint}</CodeBlock>
                    <Field label="输入 VPS 显示的完整指纹" htmlFor="pair-fingerprint">
                      <Input
                        id="pair-fingerprint"
                        value={fingerprint}
                        onChange={(e) => F(e.target.value)}
                        required
                        autoComplete="off"
                      />
                    </Field>
                    <Field label="一次性配对码" htmlFor="pair-code">
                      <Input
                        id="pair-code"
                        value={code}
                        onChange={(e) => C(e.target.value)}
                        required
                        autoComplete="off"
                      />
                    </Field>
                    {selected.status === "recovery" && (
                      <>
                        <Field label="管理员密码" htmlFor="pair-password">
                          <Input
                            id="pair-password"
                            type="password"
                            autoComplete="current-password"
                            value={password}
                            onChange={(e) => P(e.target.value)}
                            required
                          />
                        </Field>
                        <Field label="二次验证码" htmlFor="pair-mfa">
                          <Input
                            id="pair-mfa"
                            value={mfa}
                            onChange={(e) => M(e.target.value)}
                            autoComplete="one-time-code"
                          />
                        </Field>
                      </>
                    )}
                  </>
                )}
              </>
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
                  O(false);
                  C("");
                  P("");
                  M("");
                }}
              >
                取消
              </Button>
              <Button
                type="submit"
                variant={device ? "destructive" : "default"}
                disabled={
                  busy ||
                  (!device &&
                    (!selected ||
                      fingerprint !== selected.fingerprint ||
                      !code))
                }
              >
                {busy
                  ? "正在提交…"
                  : device
                    ? "确认撤销"
                    : "核对一致，批准配对"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </>
  );
}
