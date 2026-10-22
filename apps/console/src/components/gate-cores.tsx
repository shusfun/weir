import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { CodeBlock, Notice, SectionHeader,
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@migate/ui";
import { api } from "../api";
import type { Device, Task } from "../lib/models";
import { date } from "../lib/models";
import { State, Status } from "./feedback";
import { CoreDiagnostics } from "./core-diagnostics";
export function GateCores({
  device,
  tasks,
}: {
  device: Device;
  tasks: Task[];
}) {
  const cache = useQueryClient();
  const [pending, P] = useState<{
    core: string;
    operation: string;
    version?: string;
  } | null>(null);
  const [busy, B] = useState(false);
  const [error, E] = useState("");
  const [notice, N] = useState("");
  const [versions, V] = useState<Record<string, string>>({});
  const data = device.metadata.core_status as
    | {
        observed_at?: string;
        validity?: string;
        items?: {
          core: string;
          state: string;
          version: string;
          installed: boolean;
        }[];
        version_management?: {
          validity: string;
          catalog?: {
            core: string;
            version: string;
            sha256: string;
            compatibility?: string;
            recommended?: boolean;
          }[];
          engines?: Record<
            string,
            {
              current: string;
              previous: string;
              versions: Record<
                string,
                { version: string; sha256: string; installed_at: string }
              >;
            }
          >;
        };
      }
    | undefined;
  const fresh =
    device.status === "approved" &&
    data?.validity !== "unknown" &&
    data?.observed_at &&
    Date.now() - Date.parse(data.observed_at) < 60000;
  const supported = (
    device.metadata.capabilities as string[] | undefined
  )?.includes("core.control.v1");
  const management = data?.version_management;
  const diagnosticSupported = (
    device.metadata.capabilities as string[] | undefined
  )?.includes("core.diagnostics.v1");
  const versionSupported =
    fresh &&
    management?.validity === "fresh" &&
    (device.metadata.capabilities as string[] | undefined)?.includes(
      "core.versions.v1",
    );
  async function submit() {
    if (!pending) return;
    B(true);
    E("");
    try {
      const r = await api("/admin/devices/" + device.id + "/tasks", "POST", {
        operation: pending.operation,
        payload: pending.core
          ? {
              core: pending.core,
              ...(pending.version ? { version: pending.version } : {}),
            }
          : {},
        confirm: true,
      });
      N("任务 #" + r.id + " 已提交，等待设备回执。");
      P(null);
      await cache.invalidateQueries({ queryKey: ["tasks"] });
    } catch (e) {
      E(String(e));
    } finally {
      B(false);
    }
  }
  return (
    <div className="stack">
      <SectionHeader title="托管内核" description={<>观测时间：{date(data?.observed_at)} · 来源：Gate 本地辅助程序</>} actions={<Button
          variant="outline"
          onClick={() => {
            E("");
            P({ core: "", operation: "core.status" });
          }}
        >
          检查状态
        </Button>} />
      {notice && (
        <Notice role="status">
          {notice}
        </Notice>
      )}
      {!fresh && (
        <Notice tone="warn">
          当前内核状态未知；设备离线或观测过期时，不将旧状态显示为正常。
        </Notice>
      )}
      {(data?.items ?? []).map((c) => (
        <div className="core-version-card" key={c.core}>
          <div className="inventory-item">
            <div>
              <h3>{c.core === "xray" ? "Xray" : "sing-box"}</h3>
              <p>{fresh ? c.version : "版本观测已过期"}</p>
              <small>
                {fresh ? (c.installed ? "已安装" : "未安装") : "安装状态未知"}
              </small>
            </div>
            <div className="actions">
              <Status value={fresh ? c.state : "unknown"} />
              <Button
                variant="outline"
                size="sm"
                disabled={!diagnosticSupported}
                onClick={() => {
                  E("");
                  P({ core: c.core, operation: "core.inspect" });
                }}
              >
                只读诊断 {c.core}
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={!supported}
                onClick={() => {
                  E("");
                  P({
                    core: c.core,
                    operation:
                      c.state === "active" ? "core.stop" : "core.start",
                  });
                }}
              >
                {c.state === "active" ? "停止" : "启动"}
              </Button>
            </div>
          </div>
          {(() => {
            const catalog =
              management?.catalog?.filter((p) => p.core === c.core) ?? [];
            const history = management?.engines?.[c.core];
            const currentVersion = history?.versions[history.current]?.version;
            const selected =
              versions[c.core] ??
              catalog.find((p) => p.version === currentVersion)?.version ??
              catalog.find((p) => p.recommended)?.version ??
              catalog[0]?.version ??
              "";
            const compatibility = catalog.find(
              (p) => p.version === selected,
            )?.compatibility;
            return (
              <div className="stack">
                <div className="actions core-version-actions">
                  <Select
                    value={selected}
                    onValueChange={(v) =>
                      V({ ...versions, [c.core]: String(v) })
                    }
                  >
                    <SelectTrigger className="max-w-full min-w-[180px]" aria-label={c.core + " 安装版本"}>
                      <SelectValue placeholder="尚无已校验版本包">
                        {selected}
                      </SelectValue>
                    </SelectTrigger>
                    <SelectContent>
                      {catalog.map((p) => (
                        <SelectItem key={p.version} value={p.version}>
                          {p.version}
                          {p.recommended
                            ? " · 推荐兼容版"
                            : p.compatibility
                              ? " · 兼容受限"
                              : ""}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!versionSupported || !selected}
                    onClick={() => {
                      E("");
                      P({
                        core: c.core,
                        operation: "core.install",
                        version: selected,
                      });
                    }}
                  >
                    安装 / 升级
                  </Button>
                  <Button
                    variant="outline"
                    size="sm"
                    disabled={!versionSupported || !history?.previous}
                    onClick={() => {
                      E("");
                      P({ core: c.core, operation: "core.rollback" });
                    }}
                  >
                    回滚上一版本
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={!versionSupported}
                    onClick={() => {
                      E("");
                      P({ core: c.core, operation: "core.clean" });
                    }}
                  >
                    清理历史版本
                  </Button>
                </div>
                {compatibility && (
                  <Notice role="status">
                    兼容限制：{compatibility}
                  </Notice>
                )}
                <small>
                  仅使用随应用上传的已校验版本包。升级先验证现有配置；失败恢复原二进制与服务状态。停止中的内核不会被升级操作自动启动。
                </small>
                {selected && (
                  <CodeBlock>
                    包 SHA-256：
                    {catalog.find((p) => p.version === selected)?.sha256}
                  </CodeBlock>
                )}
                {history && (
                  <details>
                    <summary>已登记版本与恢复点</summary>
                    {Object.entries(history.versions).map(([hash, v]) => (
                      <div className="list-row" key={hash}>
                        <div>
                          <strong>{v.version}</strong>
                          <small>
                            {date(v.installed_at)} · {hash.slice(0, 12)}
                          </small>
                        </div>
                        <span>
                          {hash === history.current
                            ? "当前"
                            : hash === history.previous
                              ? "回滚保留"
                              : "历史"}
                        </span>
                      </div>
                    ))}
                  </details>
                )}
              </div>
            );
          })()}
        </div>
      ))}
      {!data?.items?.length && (
        <State empty="等待 Agent 上报托管内核版本与运行状态" />
      )}
      <Button
        className="back-button pl-1 text-[var(--mg-ink-3)]"
        variant="outline"
        disabled={!supported}
        onClick={() => {
          E("");
          P({ core: "", operation: "core.restart" });
        }}
      >
        重新应用配置并重启托管内核
      </Button>
      {!supported && <p>此 Agent 未声明内核控制能力，请先升级 Agent。</p>}
      <Notice>
        清理只删除登记的历史版本，保留当前与上一回滚版本。不会操作 x-ui
        内核或发现并删除未登记文件。
      </Notice>
      <h3>内核操作记录</h3>
      {tasks
        .filter(
          (t) => t.device_id === device.id && t.operation.startsWith("core."),
        )
        .slice(0, 10)
        .map((t) => (
          <div className="list-row" key={t.id}>
            <div>
              <strong>
                #{t.id} · {t.operation}
              </strong>
              <small>
                {date(t.created_at)} ·{" "}
                {String((t.result as Record<string, unknown>)?.message ?? "")}
              </small>
            </div>
            <Status value={t.status} />
          </div>
        ))}
      <CoreDiagnostics tasks={tasks} deviceId={device.id} />
      <Dialog
        open={!!pending}
        onOpenChange={(v) => {
          if (!v && !busy) P(null);
        }}
      >
        <DialogContent>
          <DialogTitle>确认内核操作</DialogTitle>
          <DialogDescription>
            {pending?.operation === "core.clean"
              ? `${pending.core}：仅清理已登记且不是当前 / 回滚版本的历史产物，保留业务配置。`
              : pending?.operation === "core.install"
                ? `${pending.core} → ${pending.version}。校验包、验证配置后切换；运行中的连接可能中断，失败自动恢复。`
                : pending?.operation === "core.rollback"
                  ? `${pending.core}：恢复上一登记版本，验证现有配置后切换。可能中断现有连接。`
                  : pending?.operation === "core.inspect"
                    ? `${pending.core}：只读检查落盘配置结构、校验结果与脱敏日志分类。任务回执记录采样时间。`
                    : pending?.operation === "core.status"
                      ? "读取本项目内核版本、服务状态和观测时间。"
                      : `${pending?.core || "托管内核"} · ${pending?.operation}。可能中断本项目现有代理连接。停止状态会保存在 Agent，重启后仍保持，需主动启动。`}
          </DialogDescription>
          {pending?.operation === "core.install" &&
            management?.catalog?.find(
              (p) => p.core === pending.core && p.version === pending.version,
            )?.compatibility && (
              <Notice tone="warn">
                REALITY 客户端兼容受限：sing-box 1.14.3 无法连接 Xray
                26.10.17；混合客户端建议保留
                26.3.27。配置检查通过不代表客户端兼容。
              </Notice>
            )}
          {error && <Notice tone="bad">{error}</Notice>}
          <DialogFooter>
            <Button variant="outline" disabled={busy} onClick={() => P(null)}>
              取消
            </Button>
            <Button disabled={busy} onClick={() => submit()}>
              {busy ? "正在提交…" : "确认执行"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
