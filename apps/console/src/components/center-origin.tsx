// 中心公共地址：管理员第一次经 HTTPS 域名打开控制台时自动记录；之后访问地址与记录不同，
// 顶部常驻提示，可以重新验证身份后改为当前地址（已配对的 Gate 经心跳跟随切换）。
import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Form, Notice, useToast } from "@weir/ui";
import { api } from "../api";
import { emptyReauth, Reauth, useReauthAction } from "./system-settings";

interface PendingGate {
  id: number;
  name: string;
  center_url: string;
}
interface CenterOrigin {
  origin: string;
  current: string;
  recordable: boolean;
  pending_gates: PendingGate[];
}

const dismissKey = (a: string, b: string) => "migate-origin-dismiss:" + a + "→" + b;
function dismissed(a: string, b: string) {
  try {
    return localStorage.getItem(dismissKey(a, b)) === "1";
  } catch {
    return false;
  }
}

export function useCenterOrigin() {
  return useQuery({ queryKey: ["center-origin"], queryFn: () => api("/admin/center-origin") as Promise<CenterOrigin>, refetchInterval: 60000 });
}

export function CenterOriginBanner() {
  const q = useCenterOrigin();
  const client = useQueryClient();
  const toast = useToast();
  const [confirm, setConfirm] = useState(false);
  const [hidden, setHidden] = useState(false);
  const recording = useRef(false);
  const v = q.data;

  // 第一次记录：当前地址是 HTTPS 域名时自动提交一次；经 SSH 隧道或 IP 访问时不记录、不提示。
  useEffect(() => {
    if (!v || v.origin || !v.recordable || recording.current) return;
    recording.current = true;
    api("/admin/center-origin", "PUT", { origin: v.current }).then(
      () => {
        toast("已记录中心地址 " + v.current, { tone: "ok" });
        void client.invalidateQueries({ queryKey: ["center-origin"] });
      },
      () => {
        recording.current = false;
      },
    );
  }, [v, client, toast]);

  if (!v || !v.origin) return null;
  const differs = v.recordable && v.current !== v.origin;
  const pending = v.pending_gates.length;
  if ((!differs || hidden || dismissed(v.origin, v.current)) && pending === 0) return null;
  return (
    <div className="origin-banner">
      {differs && !hidden && !dismissed(v.origin, v.current) && (
        <Notice tone="warn" role="status">
          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <span className="min-w-0 flex-1">当前访问地址 <code>{v.current}</code> 与记录的中心地址 <code>{v.origin}</code> 不同；订阅地址与 Gate 安装命令仍使用 {v.origin}。</span>
            <span className="flex gap-2">
              <Button size="sm" onClick={() => setConfirm(true)}>改为当前地址</Button>
              <Button
                size="sm"
                variant="outline"
                onClick={() => {
                  try {
                    localStorage.setItem(dismissKey(v.origin, v.current), "1");
                  } catch {
                    // 只在本次隐藏
                  }
                  setHidden(true);
                }}
              >
                暂不更新
              </Button>
            </span>
          </div>
        </Notice>
      )}
      {pending > 0 && (
        <Notice tone="info" role="status">
          还有 {pending} 台 Gate 仍连接旧地址（{v.pending_gates.slice(0, 5).map((g) => g.name).join("、")}{pending > 5 ? " 等" : ""}），它们会在下次心跳时用新地址认证后自动切换。列表清空前请不要停用旧域名。
        </Notice>
      )}
      {confirm && <ChangeOrigin value={v} close={() => setConfirm(false)} />}
    </div>
  );
}

function ChangeOrigin({ value, close }: { value: CenterOrigin; close: () => void }) {
  const client = useQueryClient();
  const [re, setRe] = useState(emptyReauth);
  const save = useReauthAction(() => api("/admin/center-origin", "PUT", { origin: value.current, ...re }), {
    ok: "已改为 " + value.current,
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: ["center-origin"] });
      close();
    },
  });
  const gates = value.pending_gates;
  return (
    <Dialog open onOpenChange={(o) => !o && !save.busy && close()}>
      <DialogContent size="lg">
        <DialogTitle>改为当前地址</DialogTitle>
        <DialogDescription>
          把中心地址从 <code>{value.origin}</code> 改为 <code>{value.current}</code>。
        </DialogDescription>
        <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
          <ul className="m-0 list-disc space-y-1 pl-5 text-[13px] text-[var(--mg-ink-2)]">
            <li>用户订阅地址、订阅组分享地址、托管配置地址与「添加 Gate」命令立即改用新地址。</li>
            <li>已导入客户端的旧订阅地址无法自动更新；旧域名停用后，用户需要重新导入。</li>
            <li>已配对的 Gate 在下次心跳时先用新地址完成设备认证，成功后自动切换；失败的 10 分钟后重试。</li>
            <li>通行密钥按访问域名注册，换域名后需要重新注册；dsh 子域名必须与新地址同站。</li>
            <li>「仍连接旧地址」的 Gate 全部切换之前，不要停用旧域名；否则它们只能在 VPS 上重新执行「添加 Gate」命令。</li>
          </ul>
          {gates.length > 0 ? (
            <Notice tone="info">
              目前仍连接其他地址的 Gate：{gates.map((g) => g.name).join("、")}
            </Notice>
          ) : (
            <Notice tone="ok">目前没有仍连接其他地址的 Gate。</Notice>
          )}
          <Reauth value={re} onChange={setRe} />
          {save.error && <Notice tone="bad">{save.error}</Notice>}
          <DialogFooter>
            <Button type="button" variant="outline" disabled={save.busy} onClick={close}>取消</Button>
            <Button type="submit" disabled={save.busy}>{save.busy ? "正在更改…" : "确认更改"}</Button>
          </DialogFooter>
        </Form>
      </DialogContent>
    </Dialog>
  );
}
