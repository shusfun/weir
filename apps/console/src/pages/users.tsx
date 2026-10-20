import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useMatch } from "react-router-dom";
import {
  Button,
  Card,
  CardContent,
  Input,
  Label,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  Table,
  TableHeader,
  TableBody,
  TableRow,
  TableHead,
  TableCell,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@migate/ui";
import {
  Plus,
  Search,
  ArrowLeft,
  ArrowRight,
  Users,
  Mail,
  ShieldCheck,
  MoreHorizontal,
} from "lucide-react";
import { Metric } from "../components/metric";
import { UserSessions } from "../components/user-sessions";
import { api } from "../api";
import { client } from "../lib/client";
import { bytes, date, type Account } from "../lib/models";
import { State, Status } from "../components/feedback";
const units = { B: 1, MiB: 1024 ** 2, GiB: 1024 ** 3, TiB: 1024 ** 4 };
function localDate(n: number) {
  if (!n) return "";
  const d = new Date(n * 1000);
  return new Date(+d - d.getTimezoneOffset() * 60000)
    .toISOString()
    .slice(0, 16);
}
export function UsersPage() {
  const nav = useNavigate();
  const userId = useMatch("/users/:userId")?.params.userId;
  const cache = useQueryClient();
  const [search, S] = useState("");
  const [debounced, DS] = useState("");
  const [filter, F] = useState("all");
  const [page, P] = useState(1);
  const [edit, E] = useState<Account | "new" | null>(null);
  useEffect(() => {
    const t = setTimeout(() => {
      DS(search);
      P(1);
    }, 250);
    return () => clearTimeout(t);
  }, [search]);
  const query = useQuery({
    queryKey: ["users", debounced, filter, page],
    queryFn: () =>
      client.users(
        "?page=" +
          page +
          "&page_size=20&q=" +
          encodeURIComponent(debounced) +
          "&status=" +
          filter,
      ),
  });
  const detail = useQuery({
    queryKey: ["user", userId],
    queryFn: () => api("/admin/users/" + userId) as Promise<Account>,
    enabled: !!userId,
  });
  const grants = useQuery({
    queryKey: ["grants"],
    queryFn: client.grants,
    enabled: !!userId,
  });
  const devices = useQuery({
    queryKey: ["devices"],
    queryFn: client.devices,
    enabled: !!userId,
  });
  const rows = query.data?.items ?? [];
  const total = query.data?.total ?? rows.length;
  async function saved() {
    await cache.invalidateQueries({ queryKey: ["users"] });
    await cache.invalidateQueries({ queryKey: ["user"] });
    await cache.invalidateQueries({ queryKey: ["grants"] });
    E(null);
  }
  if (userId)
    return (
      <div className="stack">
        <Button
          className="back-button"
          variant="ghost"
          onClick={() => nav("/users")}
        >
          <ArrowLeft size={16} />
          全部用户
        </Button>
        {detail.isPending ? (
          <State loading />
        ) : detail.error ? (
          <State error={detail.error} onRetry={() => detail.refetch()} />
        ) : (
          detail.data && (
            <>
              <div className="entity-heading">
                <span className="entity-avatar">
                  <Users size={28} />
                </span>
                <div>
                  <h2>{detail.data.email}</h2>
                  <p>创建于 {date(detail.data.created_at)}</p>
                </div>
                <Button onClick={() => E(detail.data!)}>编辑用户</Button>
              </div>
              <UserSessions userId={Number(userId)} />
              <div className="metric-grid">
                <Metric title="已用流量" value={bytes(detail.data.used)} />
                <Metric title="总额度" value={bytes(detail.data.quota)} />
                <Metric
                  title="有效期"
                  value={
                    detail.data.expires ? date(detail.data.expires) : "长期有效"
                  }
                />
              </div>
              <Card>
                <CardContent>
                  <div className="section-heading">
                    <h2>账户信息</h2>
                    <Status
                      value={detail.data.enabled ? "enabled" : "disabled"}
                    />
                  </div>
                  <div className="detail-grid">
                    <div>
                      <span>角色</span>
                      <strong>
                        {detail.data.role === "admin" ? "管理员" : "普通用户"}
                      </strong>
                    </div>
                    <div>
                      <span>邮箱验证</span>
                      <strong>
                        {detail.data.email_verified ? "已验证" : "未验证"}
                      </strong>
                    </div>
                    <div>
                      <span>二次验证</span>
                      <strong>
                        {detail.data.totp_enrolled
                          ? "已绑定 TOTP"
                          : "未绑定 TOTP"}
                      </strong>
                    </div>
                  </div>
                </CardContent>
              </Card>
              <Card>
                <CardContent>
                  <div className="section-heading">
                    <h2>节点授权</h2>
                    <Button
                      variant="outline"
                      onClick={() => nav("/grants?user=" + userId)}
                    >
                      管理授权
                    </Button>
                  </div>
                  {grants.isPending ? (
                    <State loading />
                  ) : grants.error ? (
                    <State error={grants.error} />
                  ) : (
                    <div className="list-rows">
                      {(grants.data?.items ?? [])
                        .filter((g) => g.user_id === Number(userId))
                        .map((g) => (
                          <div className="list-row" key={g.id}>
                            <div>
                              <strong>
                                {devices.data?.items.find(
                                  (d) => d.id === g.device_id,
                                )?.name ?? "Gate #" + g.device_id}
                              </strong>
                              <small>
                                入站 #{g.inbound_id} · 已用 {bytes(g.used)}
                              </small>
                            </div>
                            <Status value={g.status} />
                          </div>
                        ))}
                      {!(grants.data?.items ?? []).some(
                        (g) => g.user_id === Number(userId),
                      ) && <State empty="尚未分配节点" />}
                    </div>
                  )}
                </CardContent>
              </Card>
            </>
          )
        )}
        <UserEditor value={edit} close={() => E(null)} saved={saved} />
      </div>
    );
  return (
    <div className="stack">
      <div className="page-intro">
        <div>
          <h2>账户与访问权限</h2>
          <p>管理用户、流量额度和有效期。新用户默认没有节点授权。</p>
        </div>
        <Button onClick={() => E("new")}>
          <Plus size={16} />
          创建用户
        </Button>
      </div>
      <Card className="table-card">
        <div className="table-toolbar">
          <div className="search-field">
            <Search size={17} />
            <Input
              aria-label="搜索用户"
              placeholder="搜索邮箱…"
              value={search}
              onChange={(e) => S(e.target.value)}
            />
          </div>
          <Select
            value={filter}
            onValueChange={(v) => {
              F(v ?? "all");
              P(1);
            }}
          >
            <SelectTrigger aria-label="用户状态">
              <SelectValue>
                {
                  (
                    {
                      all: "全部状态",
                      enabled: "已启用",
                      disabled: "已停用",
                    } as Record<string, string>
                  )[filter]
                }
              </SelectValue>
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="all">全部状态</SelectItem>
              <SelectItem value="enabled">已启用</SelectItem>
              <SelectItem value="disabled">已停用</SelectItem>
            </SelectContent>
          </Select>
          <span className="result-count">共 {total} 位用户</span>
        </div>
        {query.isPending ? (
          <State loading />
        ) : query.error ? (
          <State error={query.error} onRetry={() => query.refetch()} />
        ) : !rows.length ? (
          <State empty={search ? "没有匹配的用户" : "还没有用户"} />
        ) : (
          <div className="desktop-user-table scroll">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>用户</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>流量使用</TableHead>
                  <TableHead>有效期</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.map((u) => (
                  <TableRow key={u.id}>
                    <TableCell>
                      <div className="table-identity">
                        <span className="avatar">
                          {u.email[0].toUpperCase()}
                        </span>
                        <div>
                          <Button
                            variant="link"
                            className="identity-link"
                            onClick={() => nav("/users/" + u.id)}
                          >
                            {u.email}
                          </Button>
                          <small>
                            {u.role === "admin" ? "管理员" : "普通用户"} · #
                            {u.id}
                          </small>
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      <Status value={u.enabled ? "enabled" : "disabled"} />
                    </TableCell>
                    <TableCell>
                      <div className="usage-cell">
                        <strong>
                          {bytes(u.used)} <span>/ {bytes(u.quota)}</span>
                        </strong>
                        <div className="usage-track">
                          <span
                            style={{
                              width:
                                Math.min(
                                  100,
                                  u.quota ? (u.used / u.quota) * 100 : 0,
                                ) + "%",
                            }}
                          />
                        </div>
                      </div>
                    </TableCell>
                    <TableCell>
                      {u.expires ? (
                        date(u.expires)
                      ) : (
                        <span className="muted">长期有效</span>
                      )}
                    </TableCell>
                    <TableCell>
                      <div className="actions justify-end">
                        <Button
                          variant="ghost"
                          size="sm"
                          onClick={() => nav("/users/" + u.id)}
                        >
                          详情
                        </Button>
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() => E(u)}
                          aria-label={"编辑 " + u.email}
                        >
                          编辑
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
        <div className="mobile-user-list">
          {!query.isPending &&
            !query.error &&
            rows.map((u) => (
              <article className="mobile-user" key={u.id}>
                <div className="section-heading">
                  <strong>{u.email}</strong>
                  <Status value={u.enabled ? "enabled" : "disabled"} />
                </div>
                <p>
                  {u.role === "admin" ? "管理员" : "普通用户"} · {bytes(u.used)}{" "}
                  / {bytes(u.quota)}
                </p>
                <p>到期：{u.expires ? date(u.expires) : "长期有效"}</p>
                <div className="actions">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => nav("/users/" + u.id)}
                  >
                    查看详情
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => E(u)}>
                    编辑
                  </Button>
                </div>
              </article>
            ))}
        </div>
        <div className="pagination">
          <span>第 {page} 页 · 每页 20 条</span>
          <div className="actions">
            <Button
              variant="outline"
              size="icon-sm"
              aria-label="上一页"
              disabled={page === 1}
              onClick={() => P((p) => p - 1)}
            >
              <ArrowLeft size={15} />
            </Button>
            <Button
              variant="outline"
              size="icon-sm"
              aria-label="下一页"
              disabled={page * 20 >= total}
              onClick={() => P((p) => p + 1)}
            >
              <ArrowRight size={15} />
            </Button>
          </div>
        </div>
      </Card>
      <UserEditor value={edit} close={() => E(null)} saved={saved} />
    </div>
  );
}
function UserEditor({
  value,
  close,
  saved,
}: {
  value: Account | "new" | null;
  close: () => void;
  saved: () => Promise<void>;
}) {
  const [email, M] = useState("");
  const [password, W] = useState("");
  const [quota, Q] = useState("100");
  const [unit, U] = useState<keyof typeof units>("GiB");
  const [expiry, X] = useState("");
  const [enabled, B] = useState("enabled");
  const [error, E] = useState("");
  const [busy, S] = useState(false);
  const [confirm, C] = useState(false);
  useEffect(() => {
    if (!value) return;
    const account = value === "new" ? null : value;
    M(account?.email ?? "");
    W("");
    Q(account ? String(account.quota) : "100");
    U(account ? "B" : "GiB");
    X(localDate(account?.expires ?? 0));
    B(account?.enabled === false ? "disabled" : "enabled");
    E("");
    C(false);
  }, [value]);
  async function submit() {
    E("");
    const bytes = Number(quota) * units[unit];
    const expires = expiry ? Math.floor(+new Date(expiry) / 1000) : 0;
    if (
      !Number.isSafeInteger(bytes) ||
      bytes < 0 ||
      !Number.isSafeInteger(expires) ||
      expires < 0
    ) {
      E("请填写有效的额度和日期。");
      return;
    }
    S(true);
    try {
      if (value === "new")
        await api("/admin/users", "POST", {
          email,
          password,
          quota: bytes,
          expires,
        });
      else if (value)
        await api("/admin/users/" + value.id, "PATCH", {
          enabled: enabled === "enabled",
          quota: bytes,
          expires,
        });
      W("");
      await saved();
    } catch (e) {
      E(String(e));
    } finally {
      S(false);
    }
  }
  return (
    <Dialog
      open={!!value}
      onOpenChange={(o) => {
        if (!o && !busy) close();
      }}
    >
      <DialogContent className="business-dialog">
        <DialogTitle>{value === "new" ? "创建用户" : "编辑用户"}</DialogTitle>
        <DialogDescription>
          {value === "new"
            ? "创建独立账户，随后可为其分配节点。"
            : "修改额度、状态或有效期会撤销该用户现有登录会话并同步节点授权。"}
        </DialogDescription>
        <form
          className="stack"
          onSubmit={(e) => {
            e.preventDefault();
            if (value !== "new" && !confirm) {
              C(true);
              return;
            }
            void submit();
          }}
        >
          <div className="field">
            <Label htmlFor="user-email">邮箱</Label>
            <Input
              id="user-email"
              type="email"
              required
              value={email}
              disabled={value !== "new"}
              onChange={(e) => M(e.target.value)}
            />
          </div>
          {value === "new" && (
            <div className="field">
              <Label htmlFor="user-password">初始密码</Label>
              <Input
                id="user-password"
                type="password"
                minLength={12}
                maxLength={72}
                autoComplete="new-password"
                required
                value={password}
                onChange={(e) => W(e.target.value)}
              />
              <small>至少 12 个字符，用户可在账户安全中修改。</small>
            </div>
          )}
          <div className="field">
            <Label htmlFor="user-quota">流量额度</Label>
            <div className="quota-input">
              <Input
                id="user-quota"
                type="number"
                min="0"
                step="any"
                required
                value={quota}
                onChange={(e) => {
                  Q(e.target.value);
                  C(false);
                }}
              />
              <Select
                value={unit}
                onValueChange={(v) => {
                  U(v as keyof typeof units);
                  C(false);
                }}
              >
                <SelectTrigger aria-label="容量单位">
                  <SelectValue>{unit}</SelectValue>
                </SelectTrigger>
                <SelectContent>
                  {Object.keys(units).map((v) => (
                    <SelectItem value={v} key={v}>
                      {v}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <small>额度为 0 时不允许使用代理流量。</small>
          </div>
          <div className="field">
            <Label htmlFor="user-expiry">到期时间</Label>
            <Input
              id="user-expiry"
              type="datetime-local"
              value={expiry}
              onChange={(e) => {
                X(e.target.value);
                C(false);
              }}
            />
            <small>留空为长期有效，按当前时区显示。</small>
          </div>
          {value !== "new" && (
            <div className="field">
              <Label>账户状态</Label>
              <Select
                value={enabled}
                onValueChange={(v) => {
                  B(v ?? "enabled");
                  C(false);
                }}
              >
                <SelectTrigger aria-label="账户状态">
                  <SelectValue>
                    {enabled === "enabled" ? "启用" : "停用"}
                  </SelectValue>
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="enabled">启用</SelectItem>
                  <SelectItem value="disabled">停用</SelectItem>
                </SelectContent>
              </Select>
            </div>
          )}
          {confirm && (
            <div className="notice" role="status">
              请确认保存。此变更会影响该用户的登录与节点访问。
            </div>
          )}
          {error && (
            <div className="notice error" role="alert">
              {error}
            </div>
          )}
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              disabled={busy}
              onClick={close}
            >
              取消
            </Button>
            <Button type="submit" disabled={busy}>
              {busy
                ? "正在保存…"
                : value === "new"
                  ? "创建用户"
                  : confirm
                    ? "确认保存"
                    : "保存变更"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
