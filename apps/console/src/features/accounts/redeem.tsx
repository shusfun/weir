import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Field, Form, Notice, PasswordInput } from "@weir/ui";
import { api } from "../../api";
import { useQuery } from "../../lib/query";
import { useFormAction } from "../../lib/action";
import { useConsoleContext } from "../../lib/console-context";
import { State } from "../../components/feedback";
import { RedemptionHistory } from "./redemption-history";
import { Section } from "../../components/system-settings";

import type { CodePage } from "./types";

export function RedeemCard() {
  const status = useQuery({ queryKey: ["redemption-status"], queryFn: () => api("/me/redemption") as Promise<{ enabled: boolean }> });
  const [page, setPage] = useState(1);
  const history = useQuery({ queryKey: ["me-redemptions", page], queryFn: () => api(`/me/redemptions?page=${page}`) as Promise<CodePage>, refetchInterval: (q) => q.state.data?.items.some((c) => c.synchronization === "pending") ? 5000 : false });
  const client = useQueryClient();
  const [code, setCode] = useState("");
  const [result, setResult] = useState("");
  const [issued, setIssued] = useState(0);
  const action = useFormAction(() => api("/me/redeem", "POST", { code }), {
    fields: { invalid_access_code: "code", access_code_cooldown: "code" },
    onSuccess: (r) => {
      setPage(1);
      void client.invalidateQueries({ queryKey: ["me-redemptions"] });
      setCode(""); setIssued((v) => v + 1); setResult(r.synchronization === "pending" ? r.message : "兑换成功，额度和授权已入账。");
      void client.invalidateQueries({ queryKey: ["me-subscription"] });
      void client.invalidateQueries({ queryKey: ["me-nodes"] });
      void client.invalidateQueries({ queryKey: ["me-links"] });
    },
  });
  useConsoleContext(status.data?.enabled ? { form: [{ key: "redeem_code", label: "兑换码", secret: true, filled: !!code }] } : null);
  const hasHistory = (history.data?.total ?? 0) > 0 || page > 1;
  // 未开放兑换且没有任何历史记录时整块隐藏，避免用大面积空卡片干扰订阅链接页
  if (!status.isPending && !status.error && !status.data?.enabled && !history.isPending && !history.error && !hasHistory) {
    return null;
  }
  return <div className="stack">
    {status.isPending || status.error ? <State loading={status.isPending} error={status.error} onRetry={() => status.refetch()} /> : status.data.enabled ? <Section title="兑换套餐" description="有效期顺延，已有节点与订阅授权保留；按月重置的流量只叠加到本周期。">
    <Form data-tour="redeem" busy={action.busy} errors={action.errors} onSubmit={() => { setResult(""); void action.submit(); }}>
      <Field key={issued} label="兑换码" name="code" hint="由管理员提供，一码一用；邀请码不能在这里兑换。">
        <PasswordInput required minLength={51} maxLength={51} autoComplete="off" value={code} onChange={(e) => setCode(e.target.value.trim())} />
      </Field>
      <Button type="submit" busy={action.busy} busyText="正在兑换…">兑换</Button>
      {action.error && <Notice tone="bad">{action.error}</Notice>}
      {result && <Notice role="status">{result}</Notice>}
    </Form>
  </Section> : null}
    {(hasHistory || history.error) && <RedemptionHistory page={page} onPage={setPage} />}
  </div>;
}
