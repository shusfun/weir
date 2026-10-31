// 应用入口：启动检测 → 初始化 / 登录 / 控制台。路由按功能分区。
import { Component, lazy, Suspense, useEffect, useState, type ComponentType, type ReactNode } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import { Button, PageState, useToast } from "@weir/ui";
import { Workspace } from "./components/workspace";
import { GlobalAnnouncement } from "./components/global-announcement";
import { api, ApiError } from "./api";
import "./style.css";

// 升级后旧标签页里的入口脚本还指向已删除的分块，按需加载会 404：整页重新加载一次换到新版本；
// 30 秒内已重新加载过仍失败就交给 LoadBoundary 显示错误，避免反复刷新
const reloadKey = "weir:chunk-reload";
class ChunkLoadError extends Error {}
function reloadForNewBuild(error: unknown): Promise<never> {
  let last = 0;
  try {
    last = Number(sessionStorage.getItem(reloadKey)) || 0;
    if (Date.now() - last > 30_000) sessionStorage.setItem(reloadKey, String(Date.now()));
  } catch {
    last = Date.now();
  }
  if (Date.now() - last > 30_000) {
    window.location.reload();
    return new Promise<never>(() => {});
  }
  return Promise.reject(new ChunkLoadError(error instanceof Error ? error.message : String(error)));
}

// 按需加载页面并保留组件自身的 props 类型
const page = <M extends Record<K, ComponentType<any>>, K extends keyof M>(load: () => Promise<M>, name: K) =>
  lazy(() => load().then((m) => ({ default: m[name] }), reloadForNewBuild));

// 分块加载失败或页面渲染出错时显示可重试的错误与真实原因，而不是整页空白
class LoadBoundary extends Component<{ children: ReactNode }, { error: Error | null }> {
  state: { error: Error | null } = { error: null };
  static getDerivedStateFromError(error: unknown) {
    return { error: error instanceof Error ? error : new Error(String(error)) };
  }
  render() {
    const { error } = this.state;
    if (!error) return this.props.children;
    const chunk = error instanceof ChunkLoadError;
    return (
      <div className="auth">
        <PageState kind="error" title={chunk ? "页面加载失败" : "页面出错"} detail={chunk ? `控制台可能刚升级，或网络中断：${error.message}` : error.message} action={<Button onClick={() => window.location.reload()}>重新加载</Button>} />
      </div>
    );
  }
}
const GatesPage = page(() => import("./pages/gates"), "GatesPage");
const SubscriptionsPage = page(() => import("./pages/subscriptions"), "SubscriptionsPage");
const PublicPagesPage = page(() => import("./pages/public-pages"), "PublicPagesPage");
const AuditPage = page(() => import("./pages/audit"), "AuditPage");
const KernelsPage = page(() => import("./pages/kernels"), "KernelsPage");
const SystemPage = page(() => import("./pages/system"), "SystemPage");
const SubPage = page(() => import("./pages/sub-page"), "SubPage");
const UsersPage = page(() => import("./pages/users"), "UsersPage");
const LinksPage = page(() => import("./pages/links"), "LinksPage");
const ProxyPage = page(() => import("./pages/proxy"), "ProxyPage");
const SecurityPage = page(() => import("./pages/security"), "SecurityPage");
const LinkMapPage = page(() => import("./pages/linkmap"), "LinkMapPage");
const LoginPage = page(() => import("./pages/login"), "LoginPage");
const SetupPage = page(() => import("./pages/setup"), "SetupPage");
const PublicPage = page(() => import("./pages/public-page"), "PublicPage");
// 动效样例页只在开发环境存在：生产构建里 import.meta.env.DEV 为 false，这个分支和页面一起被剔除
const DevUiPage = import.meta.env.DEV ? page(() => import("./pages/dev-ui"), "DevUiPage") : null;

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
      } catch (e) {
        // 只有 401（没登录或会话过期）才去登录页；中心 5xx、网络断开时显示可重试的错误，不让人白白重新登录
        if (e instanceof ApiError && e.status === 401) return setStatus("login");
        throw e;
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
  const home = admin ? "/gates" : "/links";
  const guard = (node: ReactNode) => (admin ? node : <Navigate to="/links" replace />);
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
              <Route path="/linkmap" element={guard(<LinkMapPage admin />)} />
              <Route path="/links" element={<LinksPage admin={admin} />} />
              <Route path="/proxy" element={<ProxyPage admin={admin} />} />
              {/* 旧地址：「我的订阅」并进了订阅管理 */}
              <Route path="/subscription" element={<Navigate to="/links" replace />} />
              <Route path="/my/linkmap" element={<LinkMapPage admin={false} />} />
              <Route path="/security" element={<SecurityPage loginRequired={loginRequired} />} />
              {DevUiPage && <Route path="/dev/ui" element={guard(<DevUiPage />)} />}
              <Route path="*" element={<Navigate to={home} replace />} />
            </Routes>
          </Suspense>
        </div>
      </main>
    </Workspace>
  );
}

export default function Application() {
  return (
    <LoadBoundary>
      <Routed />
    </LoadBoundary>
  );
}

function Routed() {
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
