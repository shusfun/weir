import { useRef, useState } from "react";
import { Field, Notice,
  Button,
  Textarea,
  Label,
  Switch,
  Table,
  TableHeader,
  TableHead,
  TableRow,
  TableBody,
  TableCell,
  Card,
  CardContent,
} from "@migate/ui";
import { api } from "../api";
interface Preview {
  nodes: {
    index: number;
    name: string;
    protocol: string;
    server: string;
    port: number;
    duplicate: boolean;
  }[];
  node_count: number;
  duplicate_count: number;
}
export function InventoryImport({ saved }: { saved: () => void }) {
  const [content, C] = useState("");
  const [dedup, D] = useState(true);
  const [preview, P] = useState<Preview | null>(null);
  const [busy, B] = useState(false);
  const [error, E] = useState("");
  const [done, N] = useState("");
  const [reading, R] = useState(false);
  const [fileName, F] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);
  const selection = useRef(0);
  async function readFiles(files: File[]) {
    if (busy) return;
    const ticket = ++selection.current;
    P(null); E(""); N(""); F(""); C("");
    if (files.length !== 1 || files[0].size > 2 * 1024 * 1024) {
      R(false); E("请选择一个不超过 2 MiB 的文本订阅文件。"); return;
    }
    R(true);
    try {
      const buffer = await files[0].arrayBuffer();
      const text = new TextDecoder("utf-8", { fatal: true }).decode(buffer);
      if (text.includes("\0")) throw Error("binary file");
      if (ticket !== selection.current) return;
      C(text.replace(/^\uFEFF/, "")); F(files[0].name); N("文件已读取，请解析预览；尚未写入库存。");
    } catch {
      if (ticket === selection.current) E("无法读取文件。请使用 UTF-8 文本格式，勿上传压缩包或二进制文件。");
    } finally { if (ticket === selection.current) R(false); }
  }
  async function run(commit: boolean) {
    B(true);
    E("");
    N("");
    try {
      const r = await api("/admin/subscriptions/import", "POST", {
        content,
        preview: !commit,
        deduplicate: dedup,
      });
      if (commit) {
        N("已导入 " + r.node_count + " 个节点");
        C("");
        P(null);
        saved();
      } else P(r);
    } catch (e) {
      E(String(e));
    } finally {
      B(false);
    }
  }
  return (
    <div className="stack">
      <p>
        URI / Base64、Clash YAML、sing-box JSON 或 Surge / Loon / QuanX 本地节点。最多 500
        个节点；先解析预览，再确认写入库存。导入不授予用户访问权限。
        INI 只导入本地节点，不应用规则和策略；不支持的字段明确拒绝。
      </p>
      <div className="import-file-panel" role="group" aria-label="从文件导入"
        onDragOver={(e) => { e.preventDefault(); e.dataTransfer.dropEffect = "copy"; }}
        onDrop={(e) => { e.preventDefault(); void readFiles(Array.from(e.dataTransfer.files)); }}>
        <input ref={fileInput} type="file" hidden aria-label="选择订阅文件"
          accept=".txt,.conf,.ini,.yaml,.yml,.json,text/plain,application/json"
          onChange={(e) => { void readFiles(Array.from(e.target.files ?? [])); e.target.value = ""; }} />
        <Button type="button" variant="outline" disabled={busy || reading} onClick={() => fileInput.current?.click()}>
          {reading ? "正在读取文件…" : "选择订阅文件"}
        </Button>
        <p>{fileName || "也可将一个文件拖到此处。UTF-8 文本，最大 2 MiB；文件只在浏览器读取。"}</p>
      </div>
      <Field label="节点清单内容" htmlFor="inventory-content"><Textarea
        id="inventory-content"
        mono
        value={content}
        disabled={reading || busy}
        onChange={(e) => {
          selection.current++; F(""); N("");
          C(e.target.value);
          P(null);
        }}
        rows={7}
      /></Field>
      <Label className="selector-option">
        <Switch
          checked={dedup}
          onCheckedChange={(v) => {
            D(v);
            P(null);
          }}
        />
        按协议和连接凭据去重
      </Label>
      <div className="actions">
        <Button
          type="button"
          variant="outline"
          disabled={busy || reading || !content.trim()}
          onClick={() => run(false)}
        >
          {busy ? "正在处理…" : "解析预览"}
        </Button>
        {preview && (
          <Button disabled={busy || reading} onClick={() => run(true)}>
            确认导入 {preview.node_count} 个节点
          </Button>
        )}
      </div>
      {error && (
        <Notice tone="bad">
          {error}
        </Notice>
      )}
      {done && (
        <Notice role="status">
          {done}
        </Notice>
      )}
      {preview && (
        <>
          <p>
            准备导入 {preview.node_count} 个 · 检测到 {preview.duplicate_count}{" "}
            个重复项
          </p>
          <div className="import-node-cards" aria-label="节点预览摘要">
            {preview.nodes.map((n) => (
              <Card key={n.index} variant="plain"><CardContent>
                <strong>{n.name || "未命名节点"}</strong>
                <dl><div><dt>协议</dt><dd>{n.protocol}</dd></div><div><dt>服务器</dt><dd>{n.server}</dd></div><div><dt>端口</dt><dd>{n.port}</dd></div></dl>
              </CardContent></Card>
            ))}
          </div>
          <div className="scroll import-preview import-node-table">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>名称</TableHead>
                  <TableHead>协议</TableHead>
                  <TableHead>服务器</TableHead>
                  <TableHead>端口</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {preview.nodes.map((n) => (
                  <TableRow key={n.index}>
                    <TableCell>{n.name || "未命名节点"}</TableCell>
                    <TableCell>{n.protocol}</TableCell>
                    <TableCell>{n.server}</TableCell>
                    <TableCell>{n.port}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        </>
      )}
    </div>
  );
}
