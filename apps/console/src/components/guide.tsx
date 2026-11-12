import { createContext, useCallback, useContext, useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useLocation, useNavigate } from "react-router-dom";
import { Button, Card, CardContent, Kbd, Notice } from "@weir/ui";
import { BookOpen, RotateCcw, X } from "lucide-react";
import { snapshot, subscribeContext, type CtxLayer } from "../lib/console-context";
import { contextGuide, onboarding, type Guide, type GuideIdentity } from "../lib/guide-catalog";
import { beginGuide, endGuide, guideStage, guideStep, onGuideRequest } from "../lib/guide-events";

type GuideService = { current: Guide | null; beginner?: Guide; seen: boolean; updated: boolean; active: boolean; welcome: boolean; start: (guide?: Guide) => void; dismissWelcome: () => void; identify: (identity: GuideIdentity) => void };
const GuideContext = createContext<GuideService | null>(null);
export function useGuide() { const value = useContext(GuideContext); if (!value) throw new Error("GuideProvider is missing"); return value; }
type History = { welcome?: boolean; tours: Record<string, number> };
const emptyHistory = (): History => ({ tours: {} });
const storageKey = (identity: GuideIdentity) => `weir-guide:${identity.id}:${identity.role}`;
function readHistory(identity: GuideIdentity): History {
  try { const value = JSON.parse(localStorage.getItem(storageKey(identity)) ?? "null"); if (value && typeof value.tours === "object" && value.tours !== null) return value; } catch { /* 私密浏览或旧值损坏时，本标签仍可使用教程。 */ }
  return emptyHistory();
}
const visible = (el: Element) => el.getBoundingClientRect().width > 0 && el.getBoundingClientRect().height > 0 && !el.closest('[aria-hidden="true"], [inert]');
function topDialog(): HTMLElement | null {
  return [...document.querySelectorAll<HTMLElement>('[data-slot="dialog-content"], [data-slot="sheet-content"]')]
    .filter(el => visible(el) && !el.hasAttribute("data-ending-style"))
    .sort((a, b) => Number(getComputedStyle(a).zIndex) - Number(getComputedStyle(b).zIndex) || Number(a.dataset.depth ?? 0) - Number(b.dataset.depth ?? 0)).at(-1) ?? null;
}
function findTarget(selector: string | undefined, host: HTMLElement | null) {
  if (!selector) return null;
  if (host?.matches(selector)) return host;
  return [...(host ?? document).querySelectorAll<HTMLElement>(selector)].find(visible) ?? null;
}

export function GuideProvider({ children }: { children: ReactNode }) {
  const location = useLocation();
  const navigate = useNavigate();
  const [identity, setIdentity] = useState<GuideIdentity>({ id: "anonymous", role: "guest" });
  const identityRef = useRef(identity);
  const [history, setHistory] = useState<History>(() => readHistory(identity));
  const historyRef = useRef(history);
  historyRef.current = history;
  const [context, setContext] = useState<CtxLayer>({});
  const [run, setRun] = useState<{ guide: Guide; index: number; route: string } | null>(null);
  const [host, setHost] = useState<HTMLElement | null>(null);
  const [rect, setRect] = useState<{ x: number; y: number; w: number; h: number } | null>(null);
  const [missing, setMissing] = useState(false);
  const [ready, setReady] = useState(false);
  const [retry, setRetry] = useState(0);
  const previousFocus = useRef<HTMLElement | null>(null);
  const expectedRoute = useRef<string | null>(null);
  const runRef = useRef(run);
  runRef.current = run;
  const current = contextGuide(location.pathname, host ? context : { ...context, dialog: undefined }, identity);
  const step = run?.guide.steps[run.index];
  const contextRoute = location.pathname + location.search;

  const save = useCallback((next: History) => {
    historyRef.current = next;
    setHistory(next);
    try { localStorage.setItem(storageKey(identityRef.current), JSON.stringify(next)); } catch { /* 存储不可用时仅本标签生效。 */ }
  }, []);
  const stop = useCallback((reason: string, mark = false) => {
    const running = runRef.current;
    if (running && mark) {
      const latest = historyRef.current;
      save({ ...latest, welcome: true, tours: { ...latest.tours, [running.guide.id]: running.guide.version } });
    }
    endGuide(reason);
    runRef.current = null;
    setRun(null);
    expectedRoute.current = null;
    setRect(null);
    if (previousFocus.current?.isConnected) previousFocus.current.focus({ preventScroll: true });
    else topDialog()?.querySelector<HTMLElement>('[data-tour="form"] input, [data-slot="select-trigger"], [data-slot="dialog-close"]')?.focus({ preventScroll: true });
  }, [save]);
  const identify = useCallback((next: GuideIdentity) => {
    if (identityRef.current.id === next.id && identityRef.current.role === next.role) return;
    stop("account-change");
    identityRef.current = next;
    setIdentity(next);
    setHistory(readHistory(next));
  }, [stop]);
  const dismissWelcome = useCallback(() => { save({ ...historyRef.current, welcome: true }); }, [save]);
  const start = useCallback((guide?: Guide) => {
    const chosen = guide ?? current;
    if (!chosen) return;
    if (runRef.current) endGuide("restart");
    previousFocus.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    beginGuide(chosen.id);
    setRun({ guide: chosen, index: 0, route: contextRoute });
    dismissWelcome();
  }, [current, contextRoute, dismissWelcome]);
  const move = useCallback((delta: number) => {
    const running = runRef.current;
    if (!running) return;
    const index = running.index + delta;
    if (index >= running.guide.steps.length) return stop("complete", true);
    if (index < 0) return;
    const next = running.guide.steps[index];
    if (next.route && next.route !== location.pathname) {
      expectedRoute.current = next.route;
      navigate(next.route);
    }
    setRun({ ...running, index });
  }, [location.pathname, navigate, stop]);

  useEffect(() => subscribeContext(change => { if (change === "structure") setContext(snapshot()); }), []);
  useEffect(() => { setContext(snapshot()); }, [contextRoute]);
  useEffect(() => {
    const update = () => setHost(topDialog());
    update();
    const observer = new MutationObserver(update);
    observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["hidden", "aria-hidden", "inert", "data-open", "data-ending-style"] });
    return () => observer.disconnect();
  }, []);
  useEffect(() => {
    if (!runRef.current) return;
    if (expectedRoute.current === location.pathname) {
      expectedRoute.current = null;
      setRun(r => r ? { ...r, route: contextRoute } : r);
      return;
    }
    // 同页打开资源详情会改变查询参数，主线仍需等待其真实操作结果。
    if (runRef.current.route.split("?")[0] !== location.pathname || (runRef.current.guide.id !== "getting-started" && !runRef.current.guide.id.startsWith("dialog:") && runRef.current.route !== contextRoute)) stop("navigation");
  }, [contextRoute, location.pathname, stop]);
  useEffect(() => {
    if (!run) return;
    const escape = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopImmediatePropagation();
      stop("skipped", true);
    };
    // window 捕获先于弹窗的 document 监听；同一次 Esc 只能退出教程。
    window.addEventListener("keydown", escape, true);
    return () => window.removeEventListener("keydown", escape, true);
  }, [!!run, stop]);
  useEffect(() => {
    if (!step) return;
    guideStep(step.id);
    setReady(!step.wait && !step.condition);
    return onGuideRequest(request => {
      if (!step.wait || request.method !== step.wait.method) return;
      const match = step.wait.exact ? request.path === step.wait.path : request.path.startsWith(step.wait.path);
      if (match && request.status >= 200 && request.status < 300 && request.status !== 202) { setReady(true); guideStage("operation-complete"); }
    });
  }, [run?.guide.id, run?.index]);
  useEffect(() => {
    if (!run || !step) return;
    let frame = 0;
    let scrolled: HTMLElement | null = null;
    setMissing(false);
    let found = false;
    let stateReady = false;
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        if (step.condition && !stateReady && findTarget(step.condition, null)) { stateReady = true; setReady(true); guideStage("state-ready"); }
        const el = findTarget(step.target, run.guide.id.startsWith("dialog:") ? host : null);
        found = !step.target || !!el;
        if (!el) { setRect(null); return; }
        if (scrolled !== el) { scrolled = el; el.scrollIntoView({ block: "nearest", inline: "nearest", behavior: "instant" }); guideStage("target-ready"); }
        const r = el.getBoundingClientRect();
        const next = { x: Math.max(4, r.left - 4), y: Math.max(4, r.top - 4), w: Math.max(0, Math.min(innerWidth - 8, r.width + 8)), h: Math.max(0, Math.min(innerHeight - Math.max(4, r.top - 4) - 4, r.height + 8)) };
        setRect(prev => prev && Object.keys(next).every(k => prev[k as keyof typeof next] === next[k as keyof typeof next]) ? prev : next);
        setMissing(false);
      });
    };
    update();
    const observer = new MutationObserver(update);
    observer.observe(document.body, { childList: true, subtree: true, attributes: true, attributeFilter: ["hidden", "aria-busy", "data-state"] });
    const resize = new ResizeObserver(update);
    resize.observe(document.documentElement);
    window.addEventListener("resize", update);
    document.addEventListener("scroll", update, true);
    const timer = window.setTimeout(() => { if (!found) { setMissing(true); guideStage("target-missing"); } }, 8000);
    return () => { clearTimeout(timer); cancelAnimationFrame(frame); observer.disconnect(); resize.disconnect(); window.removeEventListener("resize", update); document.removeEventListener("scroll", update, true); };
  }, [run?.guide.id, run?.index, host, retry, contextRoute]);
  useEffect(() => {
    if (!run) return;
    const frame = requestAnimationFrame(() => document.querySelector<HTMLElement>('[data-tour-running]')?.focus({ preventScroll: true }));
    return () => cancelAnimationFrame(frame);
  }, [!!run, host]);
  useEffect(() => {
    const sync = (event: StorageEvent) => { if (event.key === storageKey(identityRef.current)) setHistory(readHistory(identityRef.current)); };
    window.addEventListener("storage", sync);
    return () => { window.removeEventListener("storage", sync); endGuide("unmount"); };
  }, []);

  const seenVersion = current ? history.tours[current.id] : undefined;
  const value: GuideService = { current, beginner: identity.role === "admin" ? onboarding : undefined, seen: seenVersion !== undefined, updated: seenVersion !== undefined && seenVersion < (current?.version ?? 0), active: !!run, welcome: !history.welcome, start, dismissWelcome, identify };
  const dialogRun = !!host;
  return <GuideContext.Provider value={value}>
    {children}
    {host && !run && current && createPortal(<Button className="guide-dialog-entry" size="sm" variant="ghost" onClick={() => start()} aria-label="当前弹窗教程"><BookOpen />教程</Button>, host)}
    {run && step && <>
      {rect && createPortal(<div className={"guide-highlight" + (host ? " guide-ring-only" : "")} aria-hidden style={{ left: rect.x, top: rect.y, width: rect.w, height: rect.h }} />, document.body)}
      {createPortal(<Card className={"guide-popover" + (dialogRun ? " guide-in-dialog" : "")} tabIndex={-1} role="region" aria-label="新手教程" data-tour-running={run.guide.id}>
        <CardContent className="guide-card-content">
          <div className="guide-heading"><span><BookOpen className="inline size-4" /> {run.guide.title}</span><Button variant="ghost" size="icon-sm" aria-label="跳过教程" onClick={() => stop("skipped", true)}><X /></Button></div>
          <div className="guide-text" aria-live="polite"><strong>{step.title}</strong><p>{step.text}</p></div>
          {step.route && location.pathname !== step.route && <Button size="sm" variant="outline" onClick={() => { expectedRoute.current = step.route!; navigate(step.route!); }}>{step.action ?? "打开对应页面"}</Button>}
          {missing && !(ready && step.wait) && <Notice tone="warn">当前操作区尚未出现，可能仍在加载、缺少前置资源或已经关闭。可重试或跳过此步。<Button size="sm" variant="ghost" onClick={() => setRetry(n => n + 1)}>重新定位</Button></Notice>}
          {(step.wait || step.condition) && <small role="status">{ready ? "已确认操作结果或找到已有资源，可继续。" : "等待实际操作结果；已有资源可在页面选择，也可跳过此步。"}</small>}
          <div className="guide-footer"><span>{run.index + 1} / {run.guide.steps.length} · <Kbd>Esc</Kbd> 跳过</span><div className="actions"><Button size="sm" variant="ghost" disabled={run.index === 0} onClick={() => move(-1)}>上一步</Button><Button size="sm" disabled={!ready || (!!step.target && !rect && !missing && !step.wait)} onClick={() => move(1)}>{run.index === run.guide.steps.length - 1 ? "完成" : "下一步"}</Button></div></div>
          {!host && identity.role === "admin" && run.guide.id !== onboarding.id && <Button size="sm" variant="ghost" onClick={() => start(onboarding)}>重走入门主线</Button>}
          {(missing || ((step.wait || step.condition) && !ready)) && <Button size="sm" variant="ghost" onClick={() => { guideStage("step-skipped"); move(1); }}>跳过此步</Button>}
        </CardContent>
      </Card>, host ?? document.body)}
    </>}
  </GuideContext.Provider>;
}

export function GuideIdentitySync({ id, role }: GuideIdentity) {
  const { identify } = useGuide();
  useEffect(() => identify({ id, role }), [id, role, identify]);
  return null;
}
export function GuideButton({ standalone = false }: { standalone?: boolean }) {
  const guide = useGuide();
  if (!guide.current) return null;
  return <Button className={standalone ? "guide-public-entry" : "guide-trigger"} variant="ghost" size="sm" aria-label="页面教程" onClick={() => guide.start()}><BookOpen /><span>{guide.updated ? "教程有更新" : "页面教程"}</span>{guide.updated && <span className="guide-update-dot" aria-hidden />}</Button>;
}
export function GuideWelcome() {
  const guide = useGuide();
  if (!guide.welcome || guide.active || !guide.current) return null;
  return <Card className="guide-welcome"><CardContent className="guide-welcome-content"><div><strong>第一次使用 Weir？</strong><p>跟着页面一步步操作，随时按 Esc 跳过，也可以从右上角重新开始。</p></div><div className="actions"><Button variant="ghost" size="sm" onClick={guide.dismissWelcome}>暂时不用</Button><Button size="sm" onClick={() => guide.start(guide.beginner)}>开始教程</Button></div></CardContent></Card>;
}
export function StartGuideButton() { const guide = useGuide(); return <Button variant="outline" size="sm" onClick={() => guide.start(onboarding)}><RotateCcw />跟着教程操作</Button>; }
