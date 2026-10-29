// 客户端目录：下载入口、平台、格式与显示顺序。编辑只改本地草稿，核对后验证管理员身份一次发布。
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ReorderButtons,
  CheckboxField,
  Field,
  Form,
  FormGrid,
  Notice,
  SearchInput,
  SectionHeader,
  Button,
  Card,
  CardContent,
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
  PasswordInput,
  useConfirm,
} from "@weir/ui";
import { Plus } from "lucide-react";
import { ClientIcon } from "./client-icon";
import { ConflictNotice, State } from "./feedback";
import { emptyReauth, useReauthAction } from "./system-settings";
import { client } from "../lib/client";
import { api, isConflict } from "../api";
import type { CatalogClient } from "../lib/models";
import { field, useConsoleContext } from "../lib/console-context";

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
  const [items, setItems] = useState<CatalogClient[]>([]);
  const [editor, setEditor] = useState<CatalogClient | null>(null);
  const [confirm, setConfirm] = useState(false);
  const [reset, setReset] = useState(false);
  const [re, setRe] = useState(emptyReauth);
  const [notice, setNotice] = useState("");
  useEffect(() => {
    if (q.data) setItems(q.data.clients);
  }, [q.data?.revision]);
  const save = useReauthAction(() => api("/admin/clients", "PUT", { clients: items, revision: q.data!.revision, ...re, reset }), {
    invalidate: [["client-catalog-admin"], ["client-catalog"]],
    onSuccess: () => {
      setConfirm(false);
      setReset(false);
      setRe(emptyReauth);
      setNotice("客户端目录已保存；用户仅显示启用项。");
    },
    onError: () => setRe((v) => ({ ...v, code: "" })),
  });
  const openConfirm = (resetting: boolean) => {
    setReset(resetting);
    save.reset();
    setConfirm(true);
  };
  const closeConfirm = () => {
    if (save.busy) return;
    setConfirm(false);
    setRe(emptyReauth);
  };
  const dirty = !!q.data && JSON.stringify(items) !== JSON.stringify(q.data.clients);
  function move(index: number, delta: number) {
    const next = [...items];
    [next[index], next[index + delta]] = [next[index + delta], next[index]];
    setItems(next);
    setNotice("");
  }
  const visible = items.map((v, index) => ({ v, index })).filter(({ v }) => matches(v, search));
  useConsoleContext({ dialog: editor ? { title: "编辑客户端" } : confirm ? { title: reset ? "恢复默认客户端目录" : "确认保存客户端目录" } : undefined, list: { selected: items.length }, form: [field("search", "搜索", search), field("password", "当前密码", re.password, true)] });
  return (
    <Card>
      <CardContent className="stack client-catalog-editor">
        <SectionHeader
          level="h3"
          title="客户端目录"
          description="下载入口、平台、格式与显示顺序。变更核对后验证管理员身份保存。"
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
            {visible.map(({ v, index }) => (
              <article key={v.id} className="list-row catalog-row">
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
                  <ReorderButtons label={v.name} onUp={() => move(index, -1)} onDown={() => move(index, 1)} upDisabled={index === 0} downDisabled={index === items.length - 1} />
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
            ))}
            {!!search && visible.length === 0 && <p role="status">没有匹配的客户端。</p>}
            <div className="actions">
              <Button variant="outline" onClick={() => openConfirm(true)}>
                恢复默认目录
              </Button>
              <Button disabled={!dirty} onClick={() => openConfirm(false)}>
                核对并保存目录
              </Button>
              {dirty && <small>有未保存更改，顺序尚未应用</small>}
            </div>
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
        <Dialog open={confirm} onOpenChange={(v) => !v && closeConfirm()}>
          <DialogContent>
            <DialogTitle>{reset ? "恢复默认客户端目录" : "确认保存客户端目录"}</DialogTitle>
            <DialogDescription>
              {reset ? "覆盖当前目录及顺序，用户授权保持。" : "将发布 " + items.length + " 项客户端及当前顺序。"}
              下载入口不接收订阅令牌。
            </DialogDescription>
            <Form busy={save.busy} errors={save.errors} onSubmit={() => void save.submit()}>
              <Field label="管理员密码" name="password">
                <PasswordInput value={re.password} onChange={(e) => setRe({ ...re, password: e.target.value })} required autoComplete="current-password" />
              </Field>
              <Field label="二次验证码（已绑定时必填）" name="code">
                <Input value={re.code} inputMode="numeric" onChange={(e) => setRe({ ...re, code: e.target.value })} autoComplete="one-time-code" />
              </Field>
              <ConflictNotice
                error={save.raw}
                onReload={() => {
                  setConfirm(false);
                  void q.refetch();
                }}
              />
              {save.error && !isConflict(save.raw) && <Notice tone="bad">{save.error}</Notice>}
              <DialogFooter>
                <Button type="button" variant="outline" disabled={save.busy} onClick={closeConfirm}>
                  取消
                </Button>
                <Button type="submit" disabled={save.busy}>
                  {save.busy ? "保存中…" : "确认发布目录"}
                </Button>
              </DialogFooter>
            </Form>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
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
        <Input
          type="file"
          accept="image/png,image/jpeg"
          onChange={(e) => {
            void uploadIcon(e.target.files?.[0]);
            e.target.value = "";
          }}
        />
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
