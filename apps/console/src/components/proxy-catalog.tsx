import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Notice, SearchInput,
  Button,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  Select,
  SelectTrigger,
  SelectContent,
  SelectItem,
  SelectValue,
  Label,
} from "@migate/ui";
import { Globe } from "lucide-react";
import { api } from "../api";
import type { Device, Task } from "../lib/models";
import { date } from "../lib/models";
import { State } from "./feedback";

type Candidate = {
  key: string;
  protocol: string;
  address: string;
  port: number;
  country_code: string;
  country: string;
  city: string;
  organization: string;
  has_credentials: boolean;
};
type Catalog = {
  items: Candidate[];
  total: number;
  page: number;
  regions: { code: string; name: string; count: number }[];
  observed_at: string;
  rejected: number;
};
export function ProxyCatalog({ device }: { device: Device }) {
  const [now, setNow] = useState(Date.now);
  const [open, setOpen] = useState(false),
    [protocol, setProtocol] = useState("socks"),
    [country, setCountry] = useState("all"),
    [search, setSearch] = useState(""),
    [page, setPage] = useState(1),
    [confirm, setConfirm] = useState<Candidate | null>(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(""),
    [notice, setNotice] = useState(""),
    [probe, setProbe] = useState<{ id: number; key: string } | null>(null);
  const cache = useQueryClient(),
    capable = (device.metadata.capabilities as string[] | undefined)?.includes(
      "outbound.tools.v1",
    );
  useEffect(() => {
    if (!open) return;
    setNow(Date.now());
    const timer = setInterval(() => setNow(Date.now()), 30000);
    return () => clearInterval(timer);
  }, [open]);
  const query = useQuery({
    queryKey: ["proxy-catalog", protocol, country, search, page],
    enabled: open,
    queryFn: () =>
      api(
        `/admin/proxy-catalog/${protocol}?page=${page}&country=${country === "all" ? "" : country}&search=${encodeURIComponent(search)}`,
      ) as Promise<Catalog>,
    retry: false,
  });
  const tasks = useQuery({
    queryKey: ["catalog-probe", device.id, probe?.id],
    enabled: !!probe && open,
    queryFn: () =>
      api(
        `/admin/tasks?device_id=${device.id}&operation=outbound.catalog.probe&page_size=20`,
      ) as Promise<{ items: Task[] }>,
    refetchInterval: (q) =>
      q.state.data?.items.some(
        (t) =>
          t.id === probe?.id &&
          ["applied", "failed", "expired"].includes(t.status),
      )
        ? false
        : 2000,
  });
  const task = tasks.data?.items.find((t) => t.id === probe?.id),
    observation = task?.result as
      | {
          observed_at?: string;
          items?: { status: string; latency_ms?: number }[];
        }
      | undefined;
  async function perform(p: Candidate, action: "probe" | "import") {
    setBusy(true);
    setError("");
    try {
      const t = await api(
        `/admin/devices/${device.id}/proxy-catalog/${action}`,
        "POST",
        { protocol: p.protocol, key: p.key, confirm: true },
      );
      if (action === "probe") setProbe({ id: t.id, key: p.key });
      else {
        setConfirm(null);
        setNotice(
          `导入任务 #${t.id} 已提交，等待设备回执；可在已生效出站中查看。`,
        );
        await cache.invalidateQueries({ queryKey: ["tasks"] });
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : "操作失败，请重试");
    } finally {
      setBusy(false);
    }
  }
  const currentCountry = query.data?.regions.find((r) => r.code === country);
  return (
    <>
      <Button
        variant="outline"
        onClick={() => {
          setOpen(true);
          setError("");
        }}
      >
        <Globe size={15} />
        公共代理目录
      </Button>
      <Dialog
        open={open}
        onOpenChange={(v) => {
          if (!busy) {
            setOpen(v);
            if (!v) setConfirm(null);
          }
        }}
      >
        <DialogContent size="md">
          <DialogTitle>公共代理目录</DialogTitle>
          <DialogDescription>
            EDT-Pages
            固定公开源；地区为来源元数据。第三方代理不提供可用或隐私保证，先探测再决定导入。导入只创建管理员出站，不授权用户；路由须单独设置。
          </DialogDescription>
          <div className="stack">
            <div>
              <Label>代理类型</Label>
              <Select
                value={protocol}
                onValueChange={(v) => {
                if(!v)return;
                  setProtocol(v);
                  setCountry("all");
                  setPage(1);
                  setConfirm(null);
                  setProbe(null);
                }}
              >
                <SelectTrigger aria-label="公共代理类型">
                  <SelectValue>
                    {protocol === "socks" ? "SOCKS5" : protocol.toUpperCase()}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {[
                    ["socks", "SOCKS5"],
                    ["http", "HTTP"],
                    ["https", "HTTPS"],
                  ].map(([v, label]) => (
                    <SelectItem value={v} key={v}>
                      {label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div>
              <Label>来源地区</Label>
              <Select
                value={country}
                onValueChange={(v) => {
                if(!v)return;
                  setCountry(v);
                  setPage(1);
                  setConfirm(null);
                }}
              >
                <SelectTrigger aria-label="公共代理地区">
                  <SelectValue>
                    {country === "all"
                      ? "全部地区"
                      : `${currentCountry?.name || country} (${currentCountry?.count ?? "未知"})`}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部地区</SelectItem>
                  {query.data?.regions.map((r) => (
                    <SelectItem key={r.code} value={r.code}>
                      {r.name || r.code} ({r.count})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <SearchInput
              aria-label="搜索公共代理"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
                setPage(1);
                setConfirm(null);
              }}
              placeholder="搜索 IP、地区、城市或运营商"
            />
            {query.isPending ? (
              <State loading />
            ) : query.error ? (
              <State
                error={query.error}
                detail="固定公开来源暂不可用，目录状态未知；现有出站不变。"
                onRetry={() => void query.refetch()}
              />
            ) : (
              <>
                <p>
                  来源采样 {date(query.data.observed_at)} · {query.data.total}{" "}
                  项 · 拒绝无效记录 {query.data.rejected} 项
                </p>
                {!query.data.items.length ? (
                  <State
                    empty="没有匹配的公共代理"
                    detail="请调整地区或搜索条件。"
                  />
                ) : (
                  query.data.items.map((p) => (
                    <div className="inventory-item" key={p.key}>
                      <div>
                        <strong>
                          {p.address}:{p.port}
                        </strong>
                        <p>
                          {p.country || p.country_code} · {p.city || "城市未知"}
                        </p>
                        <small>
                          {p.organization || "运营商未知"}
                          {p.has_credentials ? " · 凭据在服务端保留" : ""}
                        </small>
                        {probe?.key === p.key && (
                          <p role="status">
                            {tasks.error
                              ? "探测状态读取失败，结果未知"
                              : !task ||
                                  ["pending", "running"].includes(task.status)
                                ? "等待 Gate TCP 探测…"
                                : task.status !== "applied"
                                  ? "探测失败，结果未知"
                                  : !observation?.observed_at ||
                                      now -
                                        Date.parse(observation.observed_at) >
                                        300000
                                    ? "采样过期，结果未知"
                                    : observation.items?.[0]?.status ===
                                        "reachable"
                                      ? `TCP 可达 · ${observation.items[0].latency_ms} ms`
                                      : observation.items?.[0]?.status ===
                                          "unreachable"
                                        ? "TCP 未连接"
                                        : "未知或安全限制"}
                          </p>
                        )}
                      </div>
                      <div className="row-actions">
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={busy || !capable}
                          onClick={() => void perform(p, "probe")}
                        >
                          TCP 探测
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          disabled={busy}
                          onClick={() => {
                            setConfirm(p);
                            setError("");
                          }}
                        >
                          导入出站
                        </Button>
                      </div>
                    </div>
                  ))
                )}
                <div className="row-actions">
                  <Button
                    variant="outline"
                    disabled={page <= 1}
                    onClick={() => setPage(page - 1)}
                  >
                    上一页
                  </Button>
                  <span>
                    第 {page} / {Math.max(1, Math.ceil(query.data.total / 20))}{" "}
                    页
                  </span>
                  <Button
                    variant="outline"
                    disabled={page * 20 >= query.data.total}
                    onClick={() => setPage(page + 1)}
                  >
                    下一页
                  </Button>
                </div>
              </>
            )}
            {notice && (
              <Notice role="status">
                {notice}
              </Notice>
            )}
            {confirm && (
              <Notice>
                <strong>
                  确认导入 {confirm.address}:{confirm.port}？
                </strong>
                <p>
                  不会修改路由或用户授权。HTTPS
                  出站始终校验证书，证书名称不符时连接会失败。
                </p>
                <Button
                  disabled={busy}
                  onClick={() => void perform(confirm, "import")}
                >
                  确认导入此代理
                </Button>
                <Button
                  variant="ghost"
                  disabled={busy}
                  onClick={() => setConfirm(null)}
                >
                  返回目录
                </Button>
              </Notice>
            )}
            {error && (
              <Notice tone="bad">
                {error}
              </Notice>
            )}
            <p className="muted">
              TCP 探测只检查 Gate
              到公网端口的连接，不代表代理协议、吞吐或业务可用性。
            </p>
          </div>
          <DialogFooter>
            <Button
              variant="outline"
              disabled={busy}
              onClick={() => {
                setOpen(false);
                setConfirm(null);
              }}
            >
              关闭
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
