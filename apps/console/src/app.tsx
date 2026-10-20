import {GlobalAnnouncement} from "./components/global-announcement";
import { lazy, Suspense, useState, useEffect, type ReactNode } from "react";
import {
  useLocation,
  useNavigate,
  Navigate,
  Routes,
  Route,
} from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Workspace } from "./components/workspace";
const OverviewPage = lazy(() =>
  import("./pages/overview").then((m) => ({ default: m.OverviewPage })),
);
const GrantsPage = lazy(() =>
  import("./pages/grants").then((m) => ({ default: m.GrantsPage })),
);
const UsersPage = lazy(() =>
  import("./pages/users").then((m) => ({ default: m.UsersPage })),
);
const DevicesPage = lazy(() =>
  import("./pages/devices").then((m) => ({ default: m.DevicesPage })),
);
const SubscriptionPage = lazy(() =>
  import("./pages/subscription").then((m) => ({ default: m.SubscriptionPage })),
);
import {
  Button,
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  Badge,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "@migate/ui";
import {
  LayoutDashboard,
  Users,
  Server,
  Link,
  ShieldCheck,
  Settings,
  ScrollText,
  RefreshCw,
  Layers,
} from "lucide-react";
const Login = lazy(() =>
  import("./pages/login").then((m) => ({ default: m.LoginPage })),
);
const Setup = lazy(() =>
  import("./pages/setup").then((m) => ({ default: m.SetupPage })),
);

import { api } from "./api";
const InventoryPage = lazy(() =>
  import("./pages/inventory").then((m) => ({ default: m.InventoryPage })),
);
const RoutingPage = lazy(() =>
  import("./pages/routing").then((m) => ({ default: m.RoutingPage })),
);
const TasksPage = lazy(() =>
  import("./pages/tasks").then((m) => ({ default: m.TasksPage })),
);
const SecurityPage = lazy(() =>
  import("./pages/security").then((m) => ({ default: m.SecurityPage })),
);
const EventsPage = lazy(() =>
  import("./pages/events").then((m) => ({ default: m.EventsPage })),
);
const SettingsPage = lazy(() =>
  import("./pages/settings").then((m) => ({ default: m.SettingsPage })),
);
const AuditPage = lazy(() =>
  import("./pages/audit").then((m) => ({ default: m.AuditPage })),
);
import "./style.css";

type Row = Record<string, any>;
function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  );
}
const navigation = [
  ["overview", "概览", LayoutDashboard],
  ["users", "用户管理", Users],
  ["devices", "Gate 设备", Server],
  ["routing", "出站与路由", Layers],
  ["grants", "授权管理", Link],
  ["subscriptions", "订阅管理", Layers],
  ["tasks", "执行任务", RefreshCw],
  ["audit", "操作审计", ScrollText],
  ["events", "安全事件", ShieldCheck],
  ["settings", "系统设置", Settings],
  ["subscription", "我的订阅", Link],
  ["security", "账户安全", ShieldCheck],
] as const;
function App() {
  const location = useLocation();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const page = location.pathname.split("/")[1] || "overview";
  const P = (value: string) => navigate("/" + value);
  const [pending, Q] = useState<{
    path: string;
    method: string;
    body?: unknown;
  } | null>(null);
  const [status, S] = useState<
    "loading" | "setup" | "login" | "ready" | "error"
  >("loading");
  const [me, M] = useState<Row>({});
  const [data, D] = useState<Row>({});
  const [loadedKey, LK] = useState("");
  const [busy, B] = useState(false);
  const [notice, N] = useState("");
  const [kind, K] = useState("sources");
  const [revision, R] = useState(0);
  const [resourceEdit, RE] = useState<Row | null>(null);
  const [profilePreview, PP] = useState("");
  async function boot() {
    S("loading");
    try {
      const state = await api("/setup/status");
      if (!state.installed) {
        S("setup");
        return;
      }
      try {
        const u = await api("/me");
        M(u);
        if (
          location.pathname === "/" ||
          (u.role !== "admin" && !["subscription", "security"].includes(page))
        )
          navigate(u.role === "admin" ? "/overview" : "/subscription", {
            replace: true,
          });
        S("ready");
      } catch {
        S("login");
      }
    } catch (e) {
      N(String(e));
      S("error");
    }
  }
  useEffect(() => {
    boot();
  }, []);
  const paths: Row = {
    overview: "/admin/overview",
    users: "/admin/users",
    devices: "/admin/devices",
    routing: "/admin/resources/device-routing",
    grants: "/admin/grants",
    tasks: "/admin/tasks",
    audit: "/admin/audit",
    events: "/admin/events",
    settings: "/admin/settings",
    subscription: "/subscription",
    security: "/sessions",
    subscriptions: "/admin/resources/" + kind,
  };
  useEffect(() => {
    if (status !== "ready") return;
    if (!["overview", "subscription"].includes(page)) {
      B(false);
      N("");
      LK(`${page}:${kind}:${revision}`);
      return;
    }
    let alive = true;
    B(true);
    D({});
    N("");
    queryClient
      .fetchQuery({
        queryKey: ["legacy", page, kind, revision],
        queryFn: () => api(paths[page] ?? paths.overview),
        staleTime: 0,
      })
      .then((v) => {
        if (alive) D(v);
      })
      .catch((e) => {
        if (alive) N(String(e));
      })
      .finally(() => {
        if (alive) {
          B(false);
          LK(`${page}:${kind}:${revision}`);
        }
      });
    return () => {
      alive = false;
    };
  }, [page, status, kind, revision]);
  async function act(path: string, method = "POST", body?: unknown) {
    const result = await api(path, method, body);
    await queryClient.invalidateQueries({ queryKey: ["devices"] });
    await queryClient.invalidateQueries({ queryKey: ["device-resources"] });
    R((v) => v + 1);
    return result;
  }
  async function action(
    path: string,
    method = "POST",
    body?: unknown,
    confirmed = false,
  ) {
    if (
      !confirmed &&
      (method === "DELETE" || method === "PATCH" || path.endsWith("/revoke"))
    ) {
      Q({ path, method, body });
      return;
    }
    try {
      await act(path, method, body);
      N("操作已提交");
    } catch (e) {
      N(String(e));
    }
  }
  if (status === "loading")
    return (
      <div className="auth" role="status">
        正在连接控制中心…
      </div>
    );
  if (status === "error")
    return (
      <div className="auth">
        <div role="alert">{notice}</div>
        <Button onClick={boot}>重试</Button>
      </div>
    );
  if (status === "setup" || status === "login")
    return (
      <Suspense
        fallback={
          <div className="auth" role="status">
            正在加载账户页面…
          </div>
        }
      >
        {status === "setup" ? <Setup done={boot} /> : <Login done={boot} />}
      </Suspense>
    );
  if (!navigation.some(([key]) => key === page))
    return (
      <Navigate
        to={me.role === "admin" ? "/overview" : "/subscription"}
        replace
      />
    );
  if (me.role !== "admin" && !["subscription", "security"].includes(page))
    return <Navigate to="/subscription" replace />;
  return (
    <div className="app-root">
      <Dialog
        open={!!pending}
        onOpenChange={(open) => {
          if (!open) Q(null);
        }}
      >
        <DialogContent>
          <DialogTitle>确认变更</DialogTitle>
          <DialogDescription>
            此操作会影响账户、授权或当前连接。任务提交后，请检查 Gate
            的实际执行状态。
          </DialogDescription>
          <DialogFooter>
            <Button variant="outline" onClick={() => Q(null)}>
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={() => {
                if (pending) {
                  const request = pending;
                  Q(null);
                  void action(request.path, request.method, request.body, true);
                }
              }}
            >
              确认操作
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Workspace
        navigation={navigation}
        admin={me.role === "admin"}
        email={me.email ?? ""}
        onLogout={async () => {
          await api("/auth/logout", "POST");
          queryClient.clear();
          S("login");
        }}
      >
        <main>
          <header className="topbar">
            <div>
              <h1>{navigation.find(([key]) => key === page)?.[1]}</h1>
            </div>
            <div className="actions">
              <Badge variant="outline">
                {me.role === "admin" ? "管理员" : "用户"}
              </Badge>
              <Button
                variant="outline"
                disabled={busy}
                onClick={() => {
                  void queryClient.invalidateQueries();
                  R((v) => v + 1);
                }}
              >
                <RefreshCw size={16} />
                刷新
              </Button>
            </div>
          </header>
          <GlobalAnnouncement audience="authenticated"/>
          {notice && (
            <div className="notice" role="status">
              {notice}
            </div>
          )}
          {busy || loadedKey !== `${page}:${kind}:${revision}` ? (
            <div className="empty" role="status">
              加载中…
            </div>
          ) : (
            <Suspense
              fallback={
                <div role="status" className="page-state">
                  正在加载页面…
                </div>
              }
            >
              <Routes>
                <Route
                  path="/overview"
                  element={<OverviewPage data={data} />}
                />
                <Route path="/users/*" element={<UsersPage />} />
                <Route path="/devices/*" element={<DevicesPage />} />
                <Route path="/routing" element={<RoutingPage />} />
                <Route path="/grants" element={<GrantsPage />} />
                <Route path="/tasks" element={<TasksPage />} />
                <Route path="/audit" element={<AuditPage />} />
                <Route path="/events" element={<EventsPage />} />
                <Route path="/subscriptions" element={<InventoryPage />} />
                <Route
                  path="/subscription"
                  element={<SubscriptionPage data={data} />}
                />
                <Route
                  path="/security"
                  element={
                    <SecurityPage
                      loginRequired={() => {
                        queryClient.clear();
                        S("login");
                      }}
                    />
                  }
                />
                <Route
                  path="/settings"
                  element={
                    <SettingsPage
                      loginRequired={() => {
                        queryClient.clear();
                        S("login");
                      }}
                    />
                  }
                />
                <Route
                  path="*"
                  element={
                    <Navigate
                      to={me.role === "admin" ? "/overview" : "/subscription"}
                      replace
                    />
                  }
                />
              </Routes>
            </Suspense>
          )}
        </main>
      </Workspace>
    </div>
  );
}
function sourceObservation(data: Row = {}) {
  const usage = data.upstream_usage;
  const updated = Date.parse(data.last_update ?? "");
  const age = Date.now() - updated;
  const interval = Number(data.refresh_seconds ?? 3600) * 1000;
  if (
    data.last_error ||
    !Number.isFinite(age) ||
    age > interval ||
    age < -60000 ||
    !usage ||
    usage.status !== "available"
  )
    return "未知";
  const bytes = (value: unknown) =>
    typeof value === "string" && /^\d+$/.test(value) ? value + " B" : "未知";
  const expiry =
    usage.expire === "0"
      ? "未设到期"
      : typeof usage.expire === "string" &&
          /^\d+$/.test(usage.expire) &&
          Number(usage.expire) < 8640000000000
        ? new Date(Number(usage.expire) * 1000).toLocaleString()
        : "未知";
  return `上传 ${bytes(usage.upload)} / 下载 ${bytes(usage.download)} / 总量 ${bytes(usage.total)} / 到期 ${expiry}`;
}
const PublicPage = lazy(() =>
  import("./pages/public-page").then((m) => ({ default: m.PublicPage })),
);
export default function Application() {
  return location.pathname.startsWith("/p/") ? (
    <Suspense
      fallback={
        <div role="status" className="page-state">
          正在加载页面…
        </div>
      }
    >
      <PublicPage />
    </Suspense>
  ) : (
    <App />
  );
}
