import { Component, createRef, type ReactNode } from "react";
import { Minimize2 } from "lucide-react";
import { Button } from "./button";
import { readMotionTier, subscribeMotion } from "./motion";

export type FolderMotionRecord = {
  trace: string;
  operation: "folder.toggle";
  target: string | null;
  stage: "start" | "measure" | "finish" | "interrupt";
  ms: number;
  reason?: string;
};
type FolderItem = { id: string; label: string; folder: (expanded: boolean) => ReactNode; content: ReactNode };
type Props = { items: FolderItem[]; expandedId: string | null; onExpandedChange: (id: string | null) => void; children?: ReactNode; onMotion?: (record: FolderMotionRecord) => void };
type Phase = "closed" | "opening" | "open" | "closing";
type State = { target: string | null; present: Set<string>; phases: Record<string, Phase> };
type Box = { rect: DOMRect; opacity: string };
type Snapshot = { boxes: Map<string, Box>; height: number; scrollY: number };

// Layout is committed once. Only the surface scales; controls and text move at
// their natural size. The old content stays mounted until its return completes.
export class FolderGrid extends Component<Props, State, Snapshot | null> {
  state: State = {
    target: this.props.expandedId,
    present: new Set(this.props.expandedId ? [this.props.expandedId] : []),
    phases: this.props.expandedId ? { [this.props.expandedId]: "open" } : {},
  };
  private root = createRef<HTMLDivElement>();
  private animations: Animation[] = [];
  private sequence = 0;
  private interaction: { trace: string; started: number; target: string | null } | null = null;
  private unsubscribe?: () => void;
  private observer?: ResizeObserver;
  private width = 0;

  static getDerivedStateFromProps(props: Props, state: State): Partial<State> | null {
    if (props.expandedId === state.target) return null;
    const present = new Set(state.present);
    if (props.expandedId) present.add(props.expandedId);
    return { target: props.expandedId, present, phases: Object.fromEntries([...present].map(id => [id, id === props.expandedId ? "opening" : "closing"])) };
  }

  componentDidMount() {
    this.width = this.root.current?.clientWidth ?? 0;
    this.observer = new ResizeObserver(() => {
      const width = this.root.current?.clientWidth ?? 0;
      if (width !== this.width) { this.width = width; this.settle("resize"); }
    });
    if (this.root.current) this.observer.observe(this.root.current);
    this.unsubscribe = subscribeMotion(() => { if (this.staticMotion()) this.settle("reduced-motion"); });
    window.addEventListener("resize", this.resize);
    document.addEventListener("visibilitychange", this.visibility);
    document.addEventListener("keydown", this.keydown);
  }

  componentWillUnmount() {
    this.cancel("unmount");
    this.observer?.disconnect();
    this.unsubscribe?.();
    window.removeEventListener("resize", this.resize);
    document.removeEventListener("visibilitychange", this.visibility);
    document.removeEventListener("keydown", this.keydown);
  }

  private resize = () => this.settle("resize");
  private visibility = () => { if (document.hidden) this.settle("hidden"); };
  private staticMotion = () => readMotionTier() === "lite" || window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  private keydown = (event: KeyboardEvent) => {
    if (event.key !== "Escape" || event.defaultPrevented || !this.state.target) return;
    // Portalled editors and menus own Escape before the folder does.
    if (document.querySelector('[role="dialog"], [role="alertdialog"], [role="menu"], [role="listbox"]')) return;
    if (this.root.current?.contains(document.activeElement)) {
      event.preventDefault();
      this.props.onExpandedChange(null);
    }
  };
  private record(stage: FolderMotionRecord["stage"], reason?: string) {
    if (!this.interaction) return;
    this.props.onMotion?.({ trace: this.interaction.trace, operation: "folder.toggle", target: this.interaction.target, stage, ms: Math.round(performance.now() - this.interaction.started), ...(reason ? { reason } : {}) });
  }
  private elements() {
    return [...(this.root.current?.querySelectorAll<HTMLElement>("[data-folder-motion]") ?? [])];
  }
  getSnapshotBeforeUpdate(previous: Props): Snapshot | null {
    if (previous.expandedId === this.props.expandedId) return null;
    const boxes = new Map<string, Box>();
    for (const el of this.elements()) boxes.set(el.dataset.folderMotion!, { rect: el.getBoundingClientRect(), opacity: getComputedStyle(el).opacity });
    return { boxes, height: this.root.current?.getBoundingClientRect().height ?? 0, scrollY: window.scrollY };
  }
  componentDidUpdate(previous: Props, _state: State, snapshot: Snapshot | null) {
    if (!snapshot) return;
    this.cancel("reversed");
    this.interaction = { trace: crypto.randomUUID().replaceAll("-", ""), started: performance.now(), target: this.props.expandedId };
    this.record("start");
    const root = this.root.current;
    if (!root) return;
    const generation = this.sequence;
    const target = this.props.expandedId;
    const cells = [...root.querySelectorAll<HTMLElement>("[data-folder-cell]")];
    for (const cell of cells) {
      const id = cell.dataset.folderCell!;
      const trigger = cell.querySelector<HTMLElement>(".weir-folder-trigger");
      const oldTrigger = snapshot.boxes.get(`${id}:trigger`)?.rect;
      if (trigger) trigger.style.width = id === target && oldTrigger ? `${oldTrigger.width}px` : "";
      const body = cell.querySelector<HTMLElement>(".weir-folder-content");
      if (body && id !== target) {
        const old = snapshot.boxes.get(`${id}:content`)?.rect;
        const previousCell = snapshot.boxes.get(`${id}:cell`)?.rect;
        if (old && previousCell) Object.assign(body.style, { width: `${old.width}px`, left: `${old.left - previousCell.left}px`, top: `${old.top - previousCell.top}px` });
      } else if (body) body.removeAttribute("style");
    }
    const targetHeight = root.getBoundingClientRect().height;
    // Animate the occupied space too, so following content and the scroll limit
    // return continuously instead of jumping when closing content unmounts.
    root.style.height = `${snapshot.height}px`;
    window.scrollTo({ top: snapshot.scrollY, behavior: "instant" });
    const time = getComputedStyle(root).getPropertyValue("--weir-spring-dur").trim();
    const duration = parseFloat(time) * (time.endsWith("ms") ? 1 : 1000);
    const easing = getComputedStyle(root).getPropertyValue("--weir-spring").trim();
    const still = this.staticMotion() || typeof root.animate !== "function";
    const play = (el: HTMLElement, frames: Keyframe[]) => {
      if (!still) this.animations.push(el.animate(frames, { duration, easing, fill: "both" }));
    };
    play(root, [{ height: `${snapshot.height}px` }, { height: `${targetHeight}px` }]);
    for (const cell of cells) {
      const id = cell.dataset.folderCell!;
      const now = cell.getBoundingClientRect();
      const before = snapshot.boxes.get(`${id}:cell`)?.rect ?? now;
      const dx = before.left - now.left, dy = before.top - now.top;
      const trigger = cell.querySelector<HTMLElement>(".weir-folder-trigger")!;
      const triggerRect = trigger.getBoundingClientRect();
      const oldTrigger = snapshot.boxes.get(`${id}:trigger`)?.rect ?? triggerRect;
      const surface = cell.querySelector<HTMLElement>(".weir-folder-surface");
      const body = cell.querySelector<HTMLElement>(".weir-folder-content");
      // Read all descendant geometry before installing the parent's transform.
      if (surface) {
        const rect = surface.getBoundingClientRect();
        const old = snapshot.boxes.get(`${id}:surface`);
        const start = old?.rect ?? oldTrigger;
        const end = id === target ? rect : triggerRect;
        const transform = (r: DOMRect, x = 0, y = 0) => `translate(${r.left - rect.left - x}px, ${r.top - rect.top - y}px) scale(${r.width / rect.width}, ${r.height / rect.height})`;
        play(surface, [{ transform: transform(start, dx, dy), opacity: old?.opacity ?? "0" }, { transform: transform(end), opacity: id === target ? 1 : 0 }]);
      }
      play(trigger, [{ transform: `translate(${oldTrigger.left - triggerRect.left - dx}px, ${oldTrigger.top - triggerRect.top - dy}px)` }, { transform: "none" }]);
      if (body) {
        const rect = body.getBoundingClientRect();
        const old = snapshot.boxes.get(`${id}:content`);
        const originX = oldTrigger.left + oldTrigger.width / 2 - rect.left - rect.width / 2;
        const originY = oldTrigger.top + oldTrigger.height / 2 - rect.top;
        const endX = triggerRect.left + triggerRect.width / 2 - rect.left - rect.width / 2;
        const endY = triggerRect.top + triggerRect.height / 2 - rect.top;
        play(body, [
          { transform: `translate(${old ? old.rect.left - rect.left - dx : originX - dx}px, ${old ? old.rect.top - rect.top - dy : originY - dy}px)`, opacity: old?.opacity ?? 0 },
          { transform: id === target ? "none" : `translate(${endX}px, ${endY}px)`, opacity: id === target ? 1 : 0 },
        ]);
      }
      play(cell, [{ transform: `translate(${dx}px, ${dy}px)` }, { transform: "none" }]);
    }
    this.record("measure");
    if (previous.expandedId && !target) {
      root.querySelector<HTMLElement>(`[data-folder-cell="${CSS.escape(previous.expandedId)}"] .weir-folder-trigger button`)?.focus({ preventScroll: true });
    } else if (previous.expandedId && root.querySelector('[data-phase="closing"]')?.contains(document.activeElement)) {
      root.querySelector<HTMLElement>(`[data-folder-cell="${CSS.escape(target ?? "")}"] .weir-folder-trigger button`)?.focus({ preventScroll: true });
    }
    if (still) this.complete(generation);
    else void Promise.all(this.animations.map(animation => animation.finished)).then(() => this.complete(generation), () => {});
  }
  private cancel(reason: string) {
    this.sequence++;
    if (this.interaction) this.record("interrupt", reason);
    this.interaction = null;
    for (const animation of this.animations) animation.cancel();
    this.animations = [];
    if (this.root.current) this.root.current.style.height = "";
  }
  private complete(generation: number) {
    if (generation !== this.sequence) return;
    this.record("finish");
    this.interaction = null;
    this.cancel("finished");
    this.setState({ present: new Set(this.state.target ? [this.state.target] : []), phases: this.state.target ? { [this.state.target]: "open" } : {} });
  }
  private settle(reason: string) {
    if (!this.interaction) return;
    this.record("interrupt", reason);
    this.interaction = null;
    this.complete(this.sequence);
  }
  render() {
    return <div ref={this.root} className="weir-folder-grid">
      {this.props.items.map(item => {
        const expanded = item.id === this.state.target;
        const present = this.state.present.has(item.id);
        return <section key={item.id} data-folder-cell={item.id} data-folder-motion={`${item.id}:cell`} data-expanded={expanded || undefined} data-phase={this.state.phases[item.id] ?? "closed"} className="weir-folder-cell">
          {present && <div className="weir-folder-surface" data-folder-motion={`${item.id}:surface`} aria-hidden />}
          <div className="weir-folder-trigger" data-folder-motion={`${item.id}:trigger`}>{item.folder(expanded)}</div>
          {present && <div id={`folder-content-${item.id}`} className="weir-folder-content stack" data-folder-motion={`${item.id}:content`} inert={!expanded} aria-hidden={!expanded || undefined}>
            <div className="weir-folder-close"><Button size="icon-sm" variant="ghost" title="收起文件夹" aria-label={`收起 ${item.label} 文件夹`} onClick={() => this.props.onExpandedChange(null)}><Minimize2 /></Button></div>
            {item.content}
          </div>}
        </section>;
      })}
      {this.props.children && <div data-folder-cell="add" data-folder-motion="add:cell" className="weir-folder-cell"><div className="weir-folder-trigger" data-folder-motion="add:trigger">{this.props.children}</div></div>}
    </div>;
  }
}
