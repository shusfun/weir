import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import {
  Badge,
  Button,
  Card,
  CardContent,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@migate/ui";
import { api } from "../api";
import { State } from "./feedback";
import { date } from "../lib/models";

type Cleanup = {
  id: number;
  job_id: number;
  name: string;
  status: string;
  attempts: number;
  retry_after: number;
  cleaned_at?: string;
  last_error?: string;
  created_at: string;
};
const labels: Record<string, string> = {
  pending: "等待恢复检查",
  presented: "等待验证与清理",
  cleanup_failed: "清理失败",
  cleaned: "已确认清理",
};
export function DNSCleanupPanel({ deviceId }: { deviceId: number }) {
  const cache = useQueryClient(),
    [page, setPage] = useState(1),
    [busy, setBusy] = useState<number | null>(null),
    [notice, setNotice] = useState(""),
    [error, setError] = useState("");
  const query = useQuery({
    queryKey: ["dns-cleanup", deviceId, page],
    queryFn: () =>
      api(`/admin/devices/${deviceId}/dns-challenges?page=${page}`) as Promise<{
        items: Cleanup[];
        total: number;
      }>,
    refetchInterval: 10000,
  });
  async function retry(row: Cleanup) {
    setBusy(row.id);
    setNotice("");
    setError("");
    try {
      await api(
        `/admin/devices/${deviceId}/dns-challenges/${row.id}/retry`,
        "POST",
      );
      setNotice(
        "清理请求已排队。后台将核对 TXT 的归属并更新结果，请等待实际状态。",
      );
      await cache.invalidateQueries({ queryKey: ["dns-cleanup", deviceId] });
    } catch (e) {
      setError(e instanceof Error ? e.message : "清理请求失败，请重试。");
    } finally {
      setBusy(null);
    }
  }
  return (
    <Card>
      <CardContent className="stack">
        <div className="section-heading">
          <div>
            <h3>DNS 验证记录清理</h3>
            <p>
              仅清理本项目创建且归属一致的
              TXT。签发中断后自动检查遗留记录；失败时保留状态，不标成成功。
            </p>
          </div>
          <Button
            variant="outline"
            onClick={() => query.refetch()}
            disabled={query.isFetching}
          >
            <RefreshCw size={14} />
            刷新记录
          </Button>
        </div>
        {notice && (
          <p className="notice" role="status">
            {notice}
          </p>
        )}
        {error && (
          <p className="notice error-message" role="alert">
            {error}
          </p>
        )}
        {query.isPending ? (
          <State loading />
        ) : query.isError ? (
          <State error={query.error} onRetry={() => query.refetch()} />
        ) : !query.data.items.length ? (
          <State
            empty="尚无 TXT 验证记录"
          />
        ) : (
          <>
            <div className="table-scroll">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>验证域名 / 签发单</TableHead>
                    <TableHead>清理状态</TableHead>
                    <TableHead>尝试次数</TableHead>
                    <TableHead>最近结果 / 下次检查</TableHead>
                    <TableHead>操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {query.data.items.map((row) => (
                    <TableRow key={row.id}>
                      <TableCell>
                        <strong className="break-all">{row.name}</strong>
                        <small className="muted block">
                          签发单 #{row.job_id}
                        </small>
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline">
                          {labels[row.status] ?? "未知"}
                        </Badge>
                      </TableCell>
                      <TableCell>{row.attempts}</TableCell>
                      <TableCell>
                        {row.status === "cleaned"
                          ? date(row.cleaned_at)
                          : row.status === "cleanup_failed"
                            ? "上次清理未确认，等待重试"
                            : date(
                                new Date(row.retry_after * 1000).toISOString(),
                              )}
                      </TableCell>
                      <TableCell>
                        {row.status !== "cleaned" && (
                          <Button
                            variant="outline"
                            disabled={busy !== null}
                            onClick={() => retry(row)}
                          >
                            {busy === row.id ? "提交中…" : "请求重新清理"}
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
            <div className="pagination">
              <span>
                共 {query.data.total} 条 · 第 {page} 页
              </span>
              <div className="actions">
                <Button
                  variant="outline"
                  disabled={page === 1}
                  onClick={() => setPage((v) => v - 1)}
                >
                  上一页
                </Button>
                <Button
                  variant="outline"
                  disabled={page * 20 >= query.data.total}
                  onClick={() => setPage((v) => v + 1)}
                >
                  下一页
                </Button>
              </div>
            </div>
          </>
        )}
      </CardContent>
    </Card>
  );
}
