// 添加 VPS：生成一次性配对命令（1 小时有效），在 VPS 上粘贴执行后自动连接中心。
import { useState } from "react";
import { Button, CodeBlock, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Form, Input, Notice } from "@weir/ui";
import { api } from "../api";
import { useFormAction } from "../lib/action";
import { field, useConsoleContext } from "../lib/console-context";

export function InviteGate({ open, close, done }: { open: boolean; close: () => void; done: () => void }) {
  const [name, setName] = useState("");
  const [command, setCommand] = useState("");
  const [copyError, setCopyError] = useState("");
  const invite = useFormAction(() => api("/admin/gates/invite", "POST", { name: name.trim() }) as Promise<{ command: string }>, {
    onSuccess: (res) => {
      setCommand(res.command);
      done();
    },
  });
  const reset = () => {
    setName("");
    setCommand("");
    setCopyError("");
    invite.reset();
    close();
  };
  useConsoleContext(open ? { dialog: { title: "添加 VPS", step: command ? "已生成安装命令" : "填写名称" }, form: [field("name", "名称", name)] } : null);
  return (
    <Dialog open={open} onOpenChange={(o) => !o && !invite.busy && reset()}>
      <DialogContent size="md">
        <DialogTitle>添加 VPS</DialogTitle>
        <DialogDescription>在 VPS 上以 root 执行生成的命令，安装完成后自动连接，之后只需在这里操作。</DialogDescription>
        {command ? (
          <div className="stack">
            <CodeBlock className="block whitespace-pre-wrap break-all">{command}</CodeBlock>
            <Notice role="status">命令 1 小时内有效且只能使用一次；连接成功后此 VPS 会出现在列表中。</Notice>
            <DialogFooter error={copyError}>
              <CopyButton value={command} onError={() => setCopyError("复制失败，请手动选择命令复制。")}>复制命令</CopyButton>
              <Button variant="outline" onClick={reset}>完成</Button>
            </DialogFooter>
          </div>
        ) : (
          <Form busy={invite.busy} errors={invite.errors} onSubmit={() => void invite.submit()}>
            <Field label="名称" hint="例如：香港-1。可随时修改。" customError={name && !name.trim() ? "请输入名称" : ""}>
              <Input required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <DialogFooter error={invite.error}>
              <Button type="button" variant="outline" disabled={invite.busy} onClick={reset}>取消</Button>
              <Button type="submit" disabled={invite.busy} busy={invite.busy} busyText="正在生成…">生成安装命令</Button>
            </DialogFooter>
          </Form>
        )}
      </DialogContent>
    </Dialog>
  );
}
