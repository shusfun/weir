// 页面切换不闪：页面代码按需加载，但登录后在浏览器空闲时逐个预加载；导航时先把新页面的代码准备好，
// 再在一次过渡里换上新页面（旧页面一直留着，不出现 Suspense 的「正在加载页面…」）。
// 支持视图过渡的浏览器只给内容区做淡入加小位移（view-transition-name: weir-page，样式在 styles/shell.css），
// 侧边导航与顶栏直接切换；减少动态效果或不支持时直接切换。只有超过 300 ms 还没准备好才显示加载提示。
import { createContext, useContext, useEffect, useRef, useState, useTransition, type ComponentType, type ReactNode } from "react";
import { flushSync } from "react-dom";
import { UNSAFE_LocationContext, useLocation, type Location } from "react-router-dom";
import { prefersReducedMotion } from "@weir/ui";

type Loader = { load: () => Promise<unknown>; ready: () => boolean };

// 按需加载的页面：模块到了以后同步渲染（不再挂起），没到时才交给 Suspense。
// fail 处理按需加载失败（例如升级后旧分块已删除），只在真正渲染时用；空闲预加载失败不报错，下次再试。
function lazyPage<M, K extends keyof M>(load: () => Promise<M>, name: K, fail: (e: unknown) => Promise<never>) {
  type P = M[K] extends ComponentType<infer Props> ? Props : never;
  let mod: M | null = null;
  let pending: Promise<M> | null = null;
  let rendering: Promise<unknown> | null = null;
  let failure: unknown = null;
  const preload = () => {
    if (mod) return Promise.resolve(mod);
    pending ??= load().then(
      (m) => (mod = m),
      (e) => {
        pending = null;
        throw e;
      },
    );
    return pending;
  };
  function Page(props: P) {
    // 失败后抛出错误本身交给 LoadBoundary；再抛同一个已失败的 promise 会让 React 反复重试
    if (failure) throw failure;
    if (!mod) throw (rendering ??= preload().catch(fail).catch((e: unknown) => void (failure = e)));
    const C = mod[name] as unknown as ComponentType<P>;
    return <C {...(props as P & object)} />;
  }
  const loader: Loader = { load: preload, ready: () => mod !== null };
  return Object.assign(Page, { loader });
}

// 登录后空闲时逐个预加载，一次一个，不和首屏请求抢带宽
function preloadWhenIdle(loaders: Loader[]) {
  const idle = (fn: () => void) =>
    typeof window.requestIdleCallback === "function" ? window.requestIdleCallback(fn, { timeout: 3000 }) : window.setTimeout(fn, 200);
  const cancel = (id: number) => (typeof window.cancelIdleCallback === "function" ? window.cancelIdleCallback(id) : window.clearTimeout(id));
  let stopped = false;
  let handle = 0;
  const queue = loaders.filter((l) => !l.ready());
  const next = () => {
    const l = queue.shift();
    if (!l || stopped) return;
    handle = idle(() => {
      if (stopped) return;
      void l.load().catch(() => {}).finally(next);
    });
  };
  next();
  return () => {
    stopped = true;
    cancel(handle);
  };
}

const SlowContext = createContext(false);
const slowAfter = 300;

// 让 Routes 晚一步看到新地址：先等新页面的代码到位，再一次性换上。
// 只有路径变了才等；同一页面里改查询参数（详情窗记在地址栏）立即生效。
function PageSwitch({ match, children }: { match: (pathname: string) => Loader | undefined; children: (location: Location) => ReactNode }) {
  const location = useLocation();
  const [shown, setShown] = useState(location);
  const [isPending, startTransition] = useTransition();
  const [waiting, setWaiting] = useState(false);
  const [slow, setSlow] = useState(false);
  const seq = useRef(0);
  const samePath = shown.pathname === location.pathname;

  useEffect(() => {
    const id = ++seq.current;
    if (samePath) {
      // 等待中又回到了正在显示的页面：放弃那次切换
      setWaiting(false);
      if (shown !== location) setShown(location);
      return;
    }
    const loader = match(location.pathname);
    const swap = () => {
      if (id !== seq.current) return;
      setWaiting(false);
      const doc = document as Document & { startViewTransition?: (cb: () => void) => unknown };
      if (doc.startViewTransition && !prefersReducedMotion() && !document.hidden) {
        // 视图过渡要在回调里同步换好新画面，才能拍到新旧两张快照
        doc.startViewTransition(() => flushSync(() => setShown(location)));
      } else {
        startTransition(() => setShown(location));
      }
    };
    if (!loader || loader.ready()) return swap();
    setWaiting(true);
    // 失败也换上：交给页面渲染时的 Suspense / LoadBoundary 显示真实原因
    void loader.load().then(swap, swap);
  }, [location, samePath, shown, match]);

  const busy = waiting || isPending;
  useEffect(() => {
    if (!busy) return setSlow(false);
    const t = window.setTimeout(() => setSlow(true), slowAfter);
    return () => window.clearTimeout(t);
  }, [busy]);

  const current = samePath ? location : shown;
  const routerContext = useContext(UNSAFE_LocationContext);
  return (
    <SlowContext.Provider value={slow}>
      {/* 页面里的 useLocation 与正在显示的页面一致，不会在新代码到位前就读到新地址 */}
      <UNSAFE_LocationContext.Provider value={{ ...routerContext, location: current }}>{children(current)}</UNSAFE_LocationContext.Provider>
    </SlowContext.Provider>
  );
}

// 切换超过 300 ms 时的提示：固定在内容区上方的一行，不挤动页面
function SlowHint({ children }: { children: ReactNode }) {
  const slow = useContext(SlowContext);
  return (
    <div className="page-switch-hint" role="status" aria-live="polite" data-show={slow || undefined}>
      {slow ? children : null}
    </div>
  );
}

export { lazyPage, preloadWhenIdle, PageSwitch, SlowHint };
export type { Loader };
