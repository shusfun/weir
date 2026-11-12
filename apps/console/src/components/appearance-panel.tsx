// 系统设置 · 外观。选择先在本页预览，保存后写入这台浏览器并同步到账号；撤销回到上次保存的值。
import { useEffect, useState } from "react";
import { Button, ChoiceBoard, ChoiceCard, SectionHeader, SwitchField, ThemeSwatch, useConfirm } from "@weir/ui";
import { useUnsavedChanges } from "../lib/dirty";
import { markStutterPrompted, pushAccountAppearance, readAppearance, readMotion, saveAppearance, stutterPromptDue, writeAppearance, type Appearance } from "../appearance";

const themes = [
  ["light", "明亮", "灰阶底，彩色的玻璃与点阵"],
  ["dark", "暗色", "深色底，点阵会发光"],
  ["system", "跟随系统", "白天明亮，夜里暗色"],
] as const;

const motions = [
  ["full", "完整", "地球慢转、登录页航线、全部过渡"],
  ["balanced", "均衡（推荐）", "地球静止，保留过渡与按压反馈"],
  ["lite", "省电", "地球换成静态图，无玻璃、无阴影"],
] as const;

export function AppearanceWatch() {
  const confirm = useConfirm();
  useEffect(() => {
    if (!readAppearance().stutter || readMotion() === "lite" || !stutterPromptDue()) return;
    let hits = 0;
    const started = Date.now();
    let done = false;
    const ask = () => {
      if (done || hits < 5 || Date.now() - started > 60_000) return;
      done = true;
      markStutterPrompted();
      void confirm({ title: "检测到卡顿，切换到省电模式？", confirmText: "切换到省电" }).then((ok) => {
        if (!ok) return;
        const next = { ...readAppearance(), motion: "lite" as const };
        saveAppearance(next);
      });
    };
    const obs = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) if (entry.duration > 100) hits += 1;
      ask();
    });
    try {
      obs.observe({ type: "long-animation-frame", buffered: true } as PerformanceObserverInit);
    } catch {
      try { obs.observe({ type: "longtask", buffered: true }); } catch { return; }
    }
    return () => obs.disconnect();
  }, [confirm]);
  return null;
}

function same(a: Appearance, b: Appearance) {
  return a.theme === b.theme && a.motion === b.motion && a.followReduce === b.followReduce && a.stutter === b.stutter && a.globe === b.globe;
}

export function AppearancePanel() {
  const [saved, setSaved] = useState(readAppearance);
  const [draft, setDraft] = useState(saved);
  const dirty = !same(draft, saved);
  const clear = useUnsavedChanges(dirty);
  const [busy, setBusy] = useState(false);
  const [syncError, setSyncError] = useState("");
  async function commit(next: Appearance) {
    setBusy(true);
    writeAppearance(next);
    const why = await pushAccountAppearance(next);
    setBusy(false);
    setSyncError(why);
    setSaved(next);
    setDraft(next);
    clear();
  }
  const note = dirty ? "有未保存的修改" : syncError ? `只保存在这台浏览器，账号同步失败：${syncError}` : "已保存到账号，换浏览器登录也会用这套外观";
  return (
    <div className="stack">
      <div className="flex flex-wrap items-center justify-end gap-2">
        <span className="text-meta text-[var(--weir-ink-3)]">{note}</span>
        <Button type="button" variant="ghost" disabled={!dirty || busy} onClick={() => setDraft(saved)}>撤销</Button>
        <Button type="button" disabled={!dirty} busy={busy} busyText="正在保存" onClick={() => void commit(draft)}>保存</Button>
      </div>
      <SectionHeader title="主题" description="以后会加入更多主题" />
      <ChoiceBoard label="主题">
        {themes.map(([key, label, caption]) => (
          <ChoiceCard key={key} selected={draft.theme === key} render={<button type="button" aria-pressed={draft.theme === key} />} onClick={() => setDraft({ ...draft, theme: key })}>
            <ThemeSwatch preview={key} />
            <strong>{label}</strong>
            <small>{caption}</small>
          </ChoiceCard>
        ))}
      </ChoiceBoard>
      <SectionHeader title="动效" description="电脑卡的时候选「省电」，显卡占用会明显下降" />
      <ChoiceBoard label="动效">
        {motions.map(([key, label, caption]) => (
          <ChoiceCard key={key} selected={draft.motion === key} render={<button type="button" aria-pressed={draft.motion === key} />} onClick={() => setDraft({ ...draft, motion: key })}>
            <strong>{label}</strong>
            <small>{caption}</small>
          </ChoiceCard>
        ))}
      </ChoiceBoard>
      <div className="stack rounded-[var(--weir-radius)] border border-[var(--weir-line)] p-3">
        <SwitchField label="跟随系统的「减少动态效果」" description="系统开启时自动进入省电" checked={draft.followReduce} onCheckedChange={(followReduce) => setDraft({ ...draft, followReduce })} />
        <SwitchField label="检测到卡顿时提示切换" description="同一浏览器 7 天内最多提示一次" checked={draft.stutter} onCheckedChange={(stutter) => setDraft({ ...draft, stutter })} />
        <SwitchField label="背景地球" description="右下角显示节点状态；不可点击；手机上不显示" checked={draft.globe} onCheckedChange={(globe) => setDraft({ ...draft, globe })} />
      </div>
    </div>
  );
}
