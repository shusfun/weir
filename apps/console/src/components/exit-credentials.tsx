import { useState } from "react";
import { Button, Field, Input, Notice, Textarea, useConfirm } from "@weir/ui";
import { api } from "../api";
import { useFormAction } from "../lib/action";
import type { ExitItem } from "../lib/core";

export function ExitCredentials({ exit }: { exit: ExitItem }) {
  const [plain, setPlain] = useState<{ password: string; link: string } | null>(null);
  const action = useFormAction(async () => { setPlain(await api(`/admin/exits/${exit.id}/credentials`, "POST", { confirm: true })); });
  const confirm = useConfirm();
  if (!exit.has_password && !exit.has_link) return null;
  return <div className="stack">
    <Button type="button" variant="outline" busy={action.busy} onClick={async () => {
      if (plain) { setPlain(null); return; }
      if (!await confirm({ title: "查看代理凭据？", description: "此次查看会记录到操作审计。关闭弹窗后清除页面中的明文。", confirmText: "查看并记录审计" })) return;
      void action.submit();
    }}>{plain ? "隐藏凭据" : "查看凭据"}</Button>
    {action.error && <Notice tone="bad">{action.error}</Notice>}
    {plain?.password && <Field label="已保存密码"><Input readOnly autoComplete="off" value={plain.password} /></Field>}
    {plain?.link && <Field label="已保存节点链接"><Textarea readOnly value={plain.link} /></Field>}
  </div>;
}
