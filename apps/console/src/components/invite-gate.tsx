// 添加 VPS：生成一次性配对命令（1 小时有效），在 VPS 上粘贴执行后自动连接中心。
import { useState } from "react";
import { Button, CodeBlock, CopyButton, Dialog, DialogContent, DialogDescription, DialogFooter, DialogTitle, Field, Input, Notice } from "@migate/ui";
import { api } from "../api";

export function InviteGate({ open, close, done }: { open: boolean; close: () => void; done: () => void }) {
  const [name, setName] = useState("");
  const [command, setCommand] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const reset = () => {
    setName("");
    setCommand("");
    setError("");
    close();
  };
  async function submit() {
    setBusy(true);
    setError("");
    try {
      const res = await api("/admin/gates/invite", "POST", { name: name.trim() });
      setCommand(res.command);
      done();
    } catch (e) {
      setError(String(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open={open} onOpenChange={(o) => !o && !busy && reset()}>
      <DialogContent size="md">
        <DialogTitle>添加 VPS</DialogTitle>
        <DialogDescription>在 VPS 上以 root 执行生成的命令，安装完成后自动连接，之后只需在这里操作。</DialogDescription>
        {command ? (
          <div className="stack">
            <CodeBlock className="block whitespace-pre-wrap break-all">{command}</CodeBlock>
            <Notice role="status">命令 1 小时内有效且只能使用一次；连接成功后此 VPS 会出现在列表中。</Notice>
            <DialogFooter>
              <CopyButton value={command} onError={() => setError("复制失败，请手动选择命令复制。")}>复制命令</CopyButton>
              <Button variant="outline" onClick={reset}>完成</Button>
            </DialogFooter>
          </div>
        ) : (
          <form className="stack" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
            <Field label="名称" htmlFor="gate-name" hint="例如：香港-1。可随时修改。">
              <Input id="gate-name" required maxLength={100} value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <DialogFooter>
              <Button type="button" variant="outline" disabled={busy} onClick={reset}>取消</Button>
              <Button type="submit" disabled={busy || !name.trim()}>{busy ? "正在生成…" : "生成安装命令"}</Button>
            </DialogFooter>
          </form>
        )}
        {error && <Notice tone="bad">{error}</Notice>}
      </DialogContent>
    </Dialog>
  );
}
