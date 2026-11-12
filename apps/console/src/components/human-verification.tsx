import { forwardRef, useEffect, useImperativeHandle, useRef, useState } from "react";
import "altcha";
import "altcha/i18n/zh-cn";
import type { Challenge } from "altcha/types";
import { Button, Notice } from "@weir/ui";
import { api, formError } from "../api";
import { turnstileProof } from "../lib/turnstile";

export type HumanResult = { altcha: string; turnstile?: string; traceId: string };
export type HumanVerificationHandle = { solve: (purpose: string) => Promise<HumanResult>; reset: () => void };

export const HumanVerification = forwardRef<HumanVerificationHandle, { purpose: string }>(function HumanVerification({ purpose }, ref) {
  const host = useRef<HTMLDivElement>(null);
  const solve = useRef<(p: string) => Promise<HumanResult>>(async () => { throw new Error("人机验证尚未就绪"); });
  const reset = useRef<() => void>(() => {});
  const [error, setError] = useState("");
  const [generation, setGeneration] = useState(0);
  useImperativeHandle(ref, () => ({ solve: (p) => solve.current(p), reset: () => reset.current() }), []);
  useEffect(() => {
    const parent = host.current;
    if (!parent) return;
    const controller = new AbortController();
    const widget = document.createElement("altcha-widget");
    const external = document.createElement("div");
    widget.setAttribute("language", "zh-cn");
    widget.setAttribute("workers", "2");
    parent.append(widget);
    let pending: { purpose: string; promise: Promise<HumanResult> } | null = null;
    let cached: { purpose: string; result: HumanResult; expires: number } | null = null;
    const run = async (p: string): Promise<HumanResult> => {
      if (controller.signal.aborted) throw new Error("人机验证已取消");
      if (cached?.purpose === p && cached.expires > Date.now()) return cached.result;
      if (pending) { if (pending.purpose === p) return pending.promise; await pending.promise.catch(() => {}); }
      const promise = (async () => {
        setError("");
        const traceId = crypto.randomUUID().replaceAll("-", "");
        const challenge: Challenge & { provider: string; sitekey?: string } = await api(`/auth/human/challenge?purpose=${encodeURIComponent(p)}`, "GET", undefined, { traceId });
        if (controller.signal.aborted) throw new Error("人机验证已取消");
        let value: HumanResult;
        if (challenge.provider === "turnstile") {
          widget.remove();
          external.replaceChildren();
          parent.append(external);
          const started = performance.now();
          const token = await turnstileProof(external, challenge.sitekey ?? "", p, controller.signal, () => { cached = null; setError("Turnstile 验证已失效，请重新验证"); });
          await new Promise<void>((resolve) => { const timer = setTimeout(resolve, Math.max(0, Number(challenge.parameters.data?.minimumSeconds ?? 2) * 1000 - (performance.now() - started))); controller.signal.addEventListener("abort", () => { clearTimeout(timer); resolve(); }, { once: true }); });
          if (controller.signal.aborted) throw new Error("人机验证已取消");
          value = { altcha: btoa(JSON.stringify({ challenge: { signature: challenge.signature } })), turnstile: token, traceId };
        } else {
        external.remove();
        if (!widget.isConnected) parent.append(widget);
        widget.reset();
        await widget.configure({ challenge, auto: "off", workers: 2, language: "zh-cn", minDuration: Number(challenge.parameters.data?.minimumSeconds ?? 2) * 1000, humanInteractionSignature: false });
        const result = await widget.verify({ controller });
        if (!result?.payload || controller.signal.aborted) throw new Error("人机验证未完成，请重试");
        value = { altcha: result.payload, traceId };
        }
        cached = { purpose: p, result: value, expires: Number(challenge.parameters.expiresAt) * 1000 };
        return value;
      })();
      pending = { purpose: p, promise };
      try { return await promise; }
      catch (e) { if (!controller.signal.aborted) setError(formError(e, {}).message); throw e; }
      finally { if (pending?.promise === promise) pending = null; }
    };
    solve.current = run;
    reset.current = () => { cached = null; setGeneration((v) => v + 1); };
    const stateChange = () => { if (widget.getState() === "expired" || widget.getState() === "error") cached = null; };
    widget.addEventListener("statechange", stateChange);
    void run(purpose).catch(() => {});
    return () => { controller.abort(); widget.removeEventListener("statechange", stateChange); widget.remove(); external.remove(); };
  }, [purpose, generation]);
  return <div className="stack" data-tour="human-verification">
    <div ref={host} className="human-verification" aria-label="人机验证" />
    {error && <Notice tone="bad">{error}<Button type="button" variant="link" onClick={() => setGeneration((v) => v + 1)}>重新验证</Button></Notice>}
  </div>;
});
