// 客户端目录：下载入口、平台、格式与显示顺序。编辑只改本地草稿，点保存一次发布。
import { useRef, useState, type ReactNode } from "react";
import { useDraft } from "../lib/dirty";
import { useQuery } from "../lib/query";
import {
  SortableList,
  CheckboxField,
  Field,
  Form,
  FormGrid,
  Notice,
  SearchInput,
  SectionHeader,
  Button,
  Input,
  Textarea,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
  SwitchField,
  Dialog,
  DialogContent,
  DialogTitle,
  DialogDescription,
  DialogFooter,
  useConfirm,
  FilePicker,
} from "@weir/ui";
import { Plus } from "lucide-react";
import { ClientIcon } from "./client-icon";
import { ConflictNotice, State } from "./feedback";
import { useFormAction } from "../lib/action";
import { client } from "../lib/client";
import { api, isConflict } from "../api";
import type { CatalogClient } from "../lib/models";
import { field, useConsoleContext } from "../lib/console-context";
import { Section } from "./system-settings";

const platforms = ["windows", "macos", "linux", "android", "ios", "harmonyos"];
const platformNames: Record<string, string> = {
  windows: "Windows",
  macos: "macOS",
  linux: "Linux",
  android: "Android",
  ios: "iOS",
  harmonyos: "HarmonyOS",
};
const icons = [
  ["monitor", "桌面"],
  ["phone", "手机"],
  ["globe", "通用"],
  ["shield", "安全"],
];
const formats = [
  ["base64", "通用订阅"],
  ["clash", "Clash / Mihomo"],
  ["singbox", "sing-box"],
  ["surge", "Surge"],
  ["loon", "Loon"],
  ["quanx", "Quantumult X"],
  ["egern", "Egern"],
];
const matches = (v: CatalogClient, search: string) =>
  [v.name, v.description, ...v.platforms.map((p) => platformNames[p] ?? p)].join(" ").toLocaleLowerCase().includes(search.toLocaleLowerCase());
const blank = (): CatalogClient => ({
  id: "client-" + crypto.randomUUID().slice(0, 8),
  name: "",
  description: "",
  platforms: ["windows"],
  url: "",
  repo: "",
  icon: "monitor",
  format: "clash",
  import: "copy",
  enabled: true,
});

export function ClientCatalogEditor() {
  const confirmRemove = useConfirm();
  const q = useQuery({ queryKey: ["client-catalog-admin"], queryFn: () => client.catalog(true), refetchOnWindowFocus: false });
  const [search, setSearch] = useState("");
  const draft = useDraft({ clients: q.data?.clients ?? [] as CatalogClient[], revision: q.data?.revision ?? "" });
  const items = draft.value.clients;
  const setItems = (clients: CatalogClient[]) => draft.set(previous => ({ ...previous, clients }));
  const [editor, setEditor] = useState<CatalogClient | null>(null);
  // 这次保存是否恢复默认目录：用 ref 传给请求，点按钮后立即提交时读不到还没生效的 state
  const reset = useRef(false);
  const [notice, setNotice] = useState("");
  const save = useFormAction(() => api("/admin/clients", "PUT", { clients: items, revision: draft.value.revision, reset: reset.current }), {
    invalidate: [["client-catalog-admin"], ["client-catalog"]],
    onSuccess: () => { draft.commit(); setNotice(reset.current ? "已恢复默认客户端目录。" : "客户端目录已保存；用户仅显示启用项。"); },
  });
  const submit = async (resetting: boolean) => {
    if (resetting && !(await confirmRemove({ title: "恢复默认客户端目录？", description: "覆盖当前目录及顺序，用户授权保持。", confirmText: "恢复默认" }))) return;
    reset.current = resetting;
    void save.submit();
  };
  const dirty = draft.changed;
  const visible = items.filter((v) => matches(v, search));
  // 一行目录项；reorder 是排序按钮（搜索时不排序，传 null）
  const row = (v: (typeof items)[number], reorder: ReactNode) => (
    <article className="list-row catalog-row">
      <div className="catalog-identity">
        <ClientIcon icon={v.icon} data={v.icon_data} />
        <div>
          <strong>{v.name}</strong>
          <small>
            {v.platforms.map((p) => platformNames[p] ?? p).join(" / ")} · {formats.find(([f]) => f === v.format)?.[1] ?? v.format} · {v.enabled ? "已启用" : "已停用"}
          </small>
          <small>{v.description}</small>
        </div>
      </div>
      <div className="actions">
        {reorder}
        <Button variant="outline" size="sm" onClick={() => setEditor({ ...v, platforms: [...v.platforms] })}>
          编辑
        </Button>
        <Button
          variant="destructive"
          size="sm"
          onClick={async () => {
            if (!(await confirmRemove({ title: `移除「${v.name}」？`, description: "仅从目录移除，不删除用户授权或订阅。保存目录后生效。", confirmText: "移除", tone: "danger" }))) return;
            setItems(items.filter((x) => x.id !== v.id));
            setNotice("");
          }}
        >
          删除
        </Button>
      </div>
    </article>
  );
  useConsoleContext({ dialog: editor ? { title: "编辑客户端" } : undefined, list: { total: items.length }, form: [field("search", "搜索", search)] });
  return (
    <Section title="客户端目录" description="下载入口、平台、格式与显示顺序。改完点保存目录后生效。">
      <div className="stack client-catalog-editor">
        <SectionHeader
          level="h3"
          title="客户端目录"
          description="下载入口、平台、格式与显示顺序。改完点保存目录后生效。"
          actions={
            <Button onClick={() => setEditor(blank())}>
              <Plus size={15} />
              新增客户端
            </Button>
          }
        />
        {notice && <Notice role="status">{notice}</Notice>}
        {q.isPending || q.isError ? (
          <State loading={q.isPending} error={q.error} onRetry={() => void q.refetch()} />
        ) : (
          <>
            <SearchInput aria-label="搜索客户端目录" value={search} onChange={(e) => setSearch(e.target.value)} placeholder="搜索名称、平台或说明" />
            {!items.length && <p>目录为空；用户仍可复制已有格式订阅。</p>}
            {/* 排序只在完整列表上进行：搜索时只显示匹配项，不能拖动 */}
            {search ? (
              visible.map((v) => <div key={v.id}>{row(v, null)}</div>)
            ) : (
              <SortableList
                label="客户端目录（按显示顺序）"
                items={items}
                getId={(v) => v.id}
                onReorder={(next) => {
                  setItems(next);
                  setNotice("");
                }}
                itemLabel={(v) => v.name}
                className="gap-0"
                itemClassName="items-center border-b border-[var(--weir-line-soft)] last:border-b-0"
                renderItem={(v, { reorder }) => row(v, reorder)}
              />
            )}
            {!!search && visible.length === 0 && <p role="status">没有匹配的客户端。</p>}
            <div className="actions">
              <Button variant="outline" disabled={save.busy} busy={save.busy && reset.current} busyText="正在恢复…" onClick={() => void submit(true)}>
                恢复默认目录
              </Button>
              <Button disabled={!dirty || save.busy} busy={save.busy && !reset.current} busyText="正在保存…" onClick={() => void submit(false)}>
                保存目录
              </Button>
              {dirty && <small>有未保存更改，顺序尚未应用</small>}
            </div>
            <ConflictNotice error={save.raw} onReload={() => void q.refetch()} />
            {save.error && !isConflict(save.raw) && <Notice tone="bad">{save.error}</Notice>}
          </>
        )}
        <Dialog open={!!editor} onOpenChange={(v) => !v && setEditor(null)}>
          <DialogContent size="lg">
            <DialogTitle>{items.some((v) => v.id === editor?.id) ? "编辑客户端" : "新增客户端"}</DialogTitle>
            <DialogDescription>仅 HTTPS 下载地址；不接受令牌模板或自定义执行协议。图标使用统一组件。</DialogDescription>
            {editor && (
              <ClientForm
                key={editor.id}
                value={editor}
                cancel={() => setEditor(null)}
                done={(next) => {
                  setItems(items.some((v) => v.id === next.id) ? items.map((v) => (v.id === next.id ? next : v)) : [...items, next]);
                  setEditor(null);
                  setNotice("");
                }}
              />
            )}
          </DialogContent>
        </Dialog>
      </div>
    </Section>
  );
}

// 单个客户端的编辑表单：只暂存到目录草稿，不直接请求服务端
function ClientForm({ value, cancel, done }: { value: CatalogClient; cancel: () => void; done: (v: CatalogClient) => void }) {
  const [c, setC] = useState(value);
  const [iconError, setIconError] = useState("");
  // 选图后异步读取；期间又选了别的图或移除了图标时，丢弃旧结果
  const generation = useRef(0);
  const set = <K extends keyof CatalogClient>(key: K, v: CatalogClient[K]) => setC((cur) => ({ ...cur, [key]: v }));
  async function uploadIcon(file: File | undefined) {
    if (!file) return;
    const g = ++generation.current;
    setIconError("");
    if (!["image/png", "image/jpeg"].includes(file.type) || file.size > 200 * 1024) {
      setIconError("请选择不超过 200 KiB 的 PNG / JPEG 图片，边长最多 512 像素。");
      return;
    }
    try {
      const data = await new Promise<string>((resolve, reject) => {
        const reader = new FileReader();
        reader.onerror = () => reject(Error("图片读取失败"));
        reader.onload = () => resolve(String(reader.result));
        reader.readAsDataURL(file);
      });
      if (g === generation.current) set("icon_data", data);
    } catch {
      if (g === generation.current) setIconError("图片读取失败，请重新选择。");
    }
  }
  return (
    <Form onSubmit={() => done(c)}>
      <Field label="客户端名称">
        <Input value={c.name} required maxLength={120} onChange={(e) => set("name", e.target.value)} />
      </Field>
      <Field label="客户端说明">
        <Textarea value={c.description} maxLength={600} onChange={(e) => set("description", e.target.value)} />
      </Field>
      <FormGrid>
        <Field label="客户端下载地址" customError={c.url && !c.url.startsWith("https://") ? "下载地址须为 HTTPS" : ""}>
          <Input value={c.url} type="url" required onChange={(e) => set("url", e.target.value.trim())} />
        </Field>
        <Field label="GitHub 仓库（可选）" hint="格式：owner/repo" customError={c.repo && !/^[\w.-]+\/[\w.-]+$/.test(c.repo) ? "仓库请填写 owner/repo" : ""}>
          <Input value={c.repo} onChange={(e) => set("repo", e.target.value.trim())} />
        </Field>
      </FormGrid>
      <fieldset className="resource-selector">
        <legend>适用平台</legend>
        <Field customError={c.platforms.length ? "" : "请至少选择一个平台"}>
          <div className="flex flex-wrap gap-x-4 gap-y-2">
            {platforms.map((p) => (
              <CheckboxField key={p} label={platformNames[p]} checked={c.platforms.includes(p)} onCheckedChange={(v) => set("platforms", v ? [...c.platforms, p] : c.platforms.filter((x) => x !== p))} />
            ))}
          </div>
        </Field>
      </fieldset>
      <FormGrid>
        <Field label="订阅格式">
          <Select value={c.format} onValueChange={(v) => v && setC({ ...c, format: v, import: v === "clash" ? c.import : "copy" })}>
            <SelectTrigger>
              <SelectValue>{formats.find(([v]) => v === c.format)?.[1] ?? c.format}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {formats.map(([v, n]) => (
                <SelectItem key={v} value={v}>
                  {n}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
        <Field label="目录图标">
          <Select value={c.icon} onValueChange={(v) => v && set("icon", v)}>
            <SelectTrigger>
              <SelectValue>{icons.find(([v]) => v === c.icon)?.[1] ?? c.icon}</SelectValue>
            </SelectTrigger>
            <SelectContent>
              {icons.map(([v, n]) => (
                <SelectItem key={v} value={v}>
                  {n}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </Field>
      </FormGrid>
      <Field label="上传客户端图标（可选）" error={iconError} hint="PNG / JPEG ≤ 200 KiB、边长 ≤ 512 像素；发布时由服务器重新编码。留空使用统一图标，整个目录上限 768 KiB。">
        <FilePicker accept="image/png,image/jpeg" maxBytes={200 * 1024} onError={setIconError} onFile={(f) => void uploadIcon(f)}>
          选择图标（PNG / JPEG），或拖到这里
        </FilePicker>
      </Field>
      {c.icon_data && (
        <div className="actions">
          <ClientIcon icon={c.icon} data={c.icon_data} />
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              generation.current++;
              set("icon_data", "");
            }}
          >
            移除上传图标
          </Button>
        </div>
      )}
      {c.format === "clash" && <SwitchField label="允许 Clash 协议导入" checked={c.import === "clash"} onCheckedChange={(v) => set("import", v ? "clash" : "copy")} />}
      <SwitchField label="向用户显示此客户端" checked={c.enabled} onCheckedChange={(v) => set("enabled", v)} />
      <DialogFooter>
        <Button type="button" variant="outline" onClick={cancel}>
          取消
        </Button>
        <Button type="submit">暂存客户端</Button>
      </DialogFooter>
    </Form>
  );
}
