// 应用入口：启动检测 → 初始化 / 登录 / 控制台。路由按功能分区。
import { lazy, Suspense, useEffect, useState, type ComponentType, type ReactNode } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Button, PageState, useToast } from "@migate/ui";
import { Workspace } from "./components/workspace";
import { GlobalAnnouncement } from "./components/global-announcement";
import { api } from "./api";
import "./style.css";

// 按需加载页面并保留组件自身的 props 类型
const page = <M extends Record<K, ComponentType<any>>, K extends keyof M>(load: () => Promise<M>, name: K) =>
  lazy(() => load().then((m) => ({ default: m[name] })));
const GatesPage = page(() => import("./pages/gates"), "GatesPage");
const SubscriptionsPage = page(() => import("./pages/subscriptions"), "SubscriptionsPage");
const PublicPagesPage = page(() => import("./pages/public-pages"), "PublicPagesPage");
const AuditPage = page(() => import("./pages/audit"), "AuditPage");
const KernelsPage = page(() => import("./pages/kernels"), "KernelsPage");
const SystemPage = page(() => import("./pages/system"), "SystemPage");
const SubPage = page(() => import("./pages/sub-page"), "SubPage");
const UsersPage = page(() => import("./pages/users"), "UsersPage");
const SubscriptionPage = page(() => import("./pages/subscription"), "SubscriptionPage");
const SecurityPage = page(() => import("./pages/security"), "SecurityPage");
const LoginPage = page(() => import("./pages/login"), "LoginPage");
const SetupPage = page(() => import("./pages/setup"), "SetupPage");
const PublicPage = page(() => import("./pages/public-page"), "PublicPage");

type Account = { email?: string; role?: string };
const loading = (title: string) => <PageState kind="loading" title={title} />;

function Console() {
  const queryClient = useQueryClient();
  const toast = useToast();
  const [status, setStatus] = useState<"loading" | "setup" | "login" | "ready" | "error">("loading");
  const [me, setMe] = useState<Account>({});
  const [failure, setFailure] = useState("");
  async function boot() {
    setStatus("loading");
    try {
      const state = await api("/setup/status");
      if (!state.installed) return setStatus("setup");
      try {
        setMe(await api("/me"));
        setStatus("ready");
      } catch {
        setStatus("login");
      }
    } catch (e) {
      setFailure(e instanceof Error ? e.message : String(e));
      setStatus("error");
    }
  }
  useEffect(() => {
    void boot();
  }, []);
  const loginRequired = () => {
    queryClient.clear();
    setStatus("login");
  };
  async function logout() {
    try {
      await api("/auth/logout", "POST");
    } catch {
      toast("退出请求未确认", { description: "已清除本页会话；如在公共设备上，请关闭浏览器。", tone: "warn" });
    }
    loginRequired();
  }

  if (status === "loading") return <div className="auth">{loading("正在连接控制中心…")}</div>;
  if (status === "error")
    return (
      <div className="auth">
        <PageState kind="error" title="无法连接控制中心" detail={failure} action={<Button onClick={boot}>重试</Button>} />
      </div>
    );
  if (status !== "ready")
    return <Suspense fallback={<div className="auth">{loading("正在加载账户页面…")}</div>}>{status === "setup" ? <SetupPage done={boot} /> : <LoginPage done={boot} />}</Suspense>;

  const admin = me.role === "admin";
  const home = admin ? "/gates" : "/subscription";
  const guard = (node: ReactNode) => (admin ? node : <Navigate to="/subscription" replace />);
  return (
    <Workspace admin={admin} email={me.email ?? ""} onLogout={logout}>
      <main className="shell-main">
        <div className="stack">
          <GlobalAnnouncement audience="authenticated" />
          <Suspense fallback={loading("正在加载页面…")}>
            <Routes>
              <Route path="/" element={<Navigate to={home} replace />} />
              <Route path="/gates" element={guard(<GatesPage />)} />
              <Route path="/users" element={guard(<UsersPage />)} />
              <Route path="/kernels" element={guard(<KernelsPage />)} />
              <Route path="/system" element={guard(<SystemPage />)} />
              <Route path="/subscriptions" element={guard(<SubscriptionsPage />)} />
              <Route path="/pages" element={guard(<PublicPagesPage />)} />
              <Route path="/ops/audit" element={guard(<AuditPage />)} />
              <Route path="/subscription" element={<SubscriptionPage />} />
              <Route path="/security" element={<SecurityPage loginRequired={loginRequired} />} />
              <Route path="*" element={<Navigate to={home} replace />} />
            </Routes>
          </Suspense>
        </div>
      </main>
    </Workspace>
  );
}

export default function Application() {
  const { pathname } = useLocation();
  if (pathname.startsWith("/sub/"))
    return (
      <Suspense fallback={loading("正在加载订阅…")}>
        <Routes>
          <Route path="/sub/:token" element={<SubPage />} />
        </Routes>
      </Suspense>
    );
  if (pathname.startsWith("/p/"))
    return (
      <Suspense fallback={loading("正在加载页面…")}>
        <PublicPage />
      </Suspense>
    );
  return <Console />;
}
