import {PrefixFields} from "./subscription-naming";
import { useState, useId, type ReactNode } from "react";
import { SourceFilters } from "./source-filters";
import { Field,
  Button,
  Label,
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  Textarea,
  Switch,
  Select,
  SelectTrigger,
  SelectValue,
  SelectContent,
  SelectItem,
} from "@migate/ui";
import { api } from "../api";
import { ResourceSelector } from "./resource-selector";
import { OperatorEditor } from "./operator-editor";
import { TextField } from "./form-field";
import { OutputTemplateFields } from "./template-fields";
import { PublicPageFields } from "./public-page-fields";
type Row = Record<string, any>;
// 仅在编辑弹窗内使用：去掉外框，只保留分区标题与内容
function Panel({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Card variant="plain">
      <CardHeader>
        <CardTitle>{title}</CardTitle>
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  );
}
export function ResourceEditor({
  kind,
  save,
  existing,
}: {
  kind: string;
  save: (d: Row) => Promise<unknown>;
  existing?: Row | null;
}) {
  const examples: Row = {
    sources: {
      name: "",
      url: "",
      enabled: true,
      refresh_seconds: 3600,
      allow_stale: false,
      max_stale_seconds: 86400,
    },
    nodes: { name: "", url: "" },
    profiles: {
      name: "",
      source_ids: [],
      node_ids: [],
      operator_ids: [],
      user_ids: [],
      template_ids: [],
      operators: [],
      enabled: true,
    },
    operators: {
      name: "",
      operators: [
        {
          type: "rename",
          params: {
            template: {
              enabled: true,
              template: "{region:zh}-{index:02}",
              indexScope: "region",
            },
          },
        },
      ],
    },
    templates: {
      name: "",
      target: "clash",
      groups: [{ name: "PROXY", type: "select", members: ["$nodes"] }],
      rules: [{ type: "FINAL", policy: "PROXY" }],
      dns: [],
    },
    "public-page": {
      name: "",
      slug: "welcome",
      title: "欢迎",
      description: "",
      notice: "",
      show_login: false,
      enabled: false,
    },
  };
  const [text, T] = useState(
    JSON.stringify(existing?.data ?? examples[kind] ?? { name: "" }, null, 2),
  );
  const [message, M] = useState("");
  const [fields, F] = useState<Row>(
    existing?.data ?? examples[kind] ?? { name: "" },
  );
  const [advanced, A] = useState(
    ![
      "sources",
      "nodes",
      "profiles",
      "operators",
      "templates",
      "public-page",
    ].includes(kind),
  );
  const [busy, B] = useState(false);
  const fieldId = useId();
  const setField = (key: string, value: any) => F({ ...fields, [key]: value });
  const textField = (key: string, label: string, type = "text") => (
    <TextField
      key={key}
      name={fieldId + key}
      label={label}
      type={type}
      value={fields[key]}
      required={key === "name" || key === "url"}
      onChange={(value) =>
        setField(key, type === "number" ? Number(value) : value)
      }
    />
  );
  const toggleField = (key: string, label: string) => (
    <div className="actions" key={key}>
      <Switch
        id={fieldId + key}
        checked={fields[key] === true}
        onCheckedChange={(value) => setField(key, value)}
      />
      <Label htmlFor={fieldId + key}>{label}</Label>
    </div>
  );
  const listField = (key: string, label: string) => (
    <TextField
      key={key}
      name={fieldId + key}
      label={label + "（按顺序填写 ID，逗号分隔）"}
      required={false}
      value={Array.isArray(fields[key]) ? fields[key].join(", ") : fields[key]}
      onChange={(value) => setField(key, value)}
    />
  );
  const normalize = (input: Row) => {
    const data = { ...input };
    if (kind === "profiles")
      for (const key of [
        "source_ids",
        "node_ids",
        "operator_ids",
        "user_ids",
        "template_ids",
      ]) {
        if (typeof data[key] === "string") {
          const value = data[key].trim();
          if (value && !/^\d+(\s*,\s*\d+)*$/.test(value))
            throw Error("引用 ID 必须为正整数，用逗号分隔");
          data[key] = value ? value.split(",").map(Number) : [];
        }
        if (
          !Array.isArray(data[key] ?? []) ||
          (data[key] ?? []).some(
            (id: number) => !Number.isSafeInteger(id) || id <= 0,
          )
        )
          throw Error("引用 ID 必须为正整数数组");
      }
    return data;
  };
  return (
    <Panel title={existing ? "编辑内容" : "填写内容"}>
      <p>
        {kind === "templates"
          ? "组合策略组、匹配规则和 DNS，输出时仅使用当前用户已授权节点。"
          : kind === "sources"
            ? "导入上游订阅并设置刷新策略；库存不会自动授权给普通用户。"
            : kind === "nodes"
              ? "保存单个节点连接信息，订阅组可引用并预览。"
              : kind === "profiles"
                ? "组合源、节点、处理链和模板；分配给用户时只处理其已有 Gate 授权。"
                : kind === "operators"
                  ? "按顺序筛选、重命名、排序或去重，并通过预览核对处理结果。"
                  : "编辑公开内容，保存后需显式发布。"}
      </p>
      <form
        className="stack"
        onSubmit={async (e) => {
          e.preventDefault();
          B(true);
          M("");
          try {
            const data = normalize(advanced ? JSON.parse(text) : fields);
            await save({ id: existing?.id, data });
            M("已保存");
          } catch (e) {
            M(String(e));
          } finally {
            B(false);
          }
        }}
      >
        {[
          "sources",
          "nodes",
          "profiles",
          "operators",
          "templates",
          "public-page",
        ].includes(kind) && (
          <Button
            type="button"
            variant="outline"
            onClick={() => {
              try {
                if (advanced) F(normalize(JSON.parse(text)));
                else T(JSON.stringify(normalize(fields), null, 2));
                A(!advanced);
                M("");
              } catch (e) {
                M(String(e));
              }
            }}
          >
            {advanced ? "返回表单" : "高级 JSON"}
          </Button>
        )}
        {!advanced && (
          <>
            {textField("name", "名称")}
            {kind === "operators" && (
              <OperatorEditor
                value={fields.operators ?? []}
                change={(v) => setField("operators", v)}
              />
            )}
            {kind === "templates" && (
              <OutputTemplateFields fields={fields} change={F} />
            )}
            {kind === "public-page" && (
              <>
                {textField("slug", "公开路径（2–64 位小写字母、数字、连字符）")}
                {textField("title", "公开页标题")}
                <Field label="公开介绍（纯文本）" htmlFor={fieldId + "description"}><Textarea
                  id={fieldId + "description"}
                  value={fields.description ?? ""}
                  onChange={(e) => setField("description", e.target.value)}
                /></Field>
                <Field label="公告（纯文本）" htmlFor={fieldId + "notice"}><Textarea
                  id={fieldId + "notice"}
                  value={fields.notice ?? ""}
                  onChange={(e) => setField("notice", e.target.value)}
                /></Field>
                {toggleField("show_login", "显示登录入口")}
                <PublicPageFields
                  fields={fields}
                  change={F}
                  ownId={existing?.id}
                />
                {toggleField("enabled", "发布公开页")}
                <p>
                  发布地址：/p/{fields.slug}
                  。内容按纯文本显示，不发布节点、用户或订阅凭据。
                </p>
              </>
            )}
            {(kind === "sources" || kind === "nodes") &&
              textField("url", kind === "sources" ? "订阅源地址" : "节点链接")}
            {kind === "nodes" && (
              <>
                {textField("group", "节点分组")}
                {textField("notes", "节点备注", "textarea")}
              </>
            )}
            {kind === "sources" && (
              <>
                {textField("group", "订阅源分组")}
                {textField("user_agent", "请求 User-Agent（留空使用默认值）")}
                {textField("website", "服务商网站（可选）")}
                {textField("notes", "订阅源备注", "textarea")}
                {toggleField("plus_as_space", "URI 节点名称中的 + 视为空格")}
                <p className="muted">
                  仅转换 URI 名称里的原始
                  +；%2B、密码、连接参数及结构化节点名称保持原值。更改后需要重新刷新源。
                </p>
                {toggleField("enabled", "启用自动刷新")}
                {textField(
                  "refresh_seconds",
                  "刷新间隔（60–86400 秒）",
                  "number",
                )}
                {toggleField("exclude_traffic", "合并时不计算此订阅源流量")}
                <p className="muted">仅从管理员上游用量合计中排除，仍计入最早到期；不改变节点、用户授权或强制额度。</p>
                {toggleField("node_cache", "保护性节点缓存")}
                <p className="muted">开启后刷新绕过上游强缓存，拒绝异常缩水或仅本机公告节点的结果；失败可在下方时限内使用上次成功缓存，预览明确标记过期。至少 10 个节点时，新结果不足原来的 30% 不覆盖。</p>
                {toggleField("allow_stale", "源失败时允许有限使用上次缓存")}
                {(fields.allow_stale || fields.node_cache) &&
                  textField(
                    "max_stale_seconds",
                    "缓存最长保留（60–604800 秒）",
                    "number",
                  )}
                <SourceFilters fields={fields} change={setField} />
              </>
            )}
            {kind === "profiles" && (
              <>
                {toggleField("enabled", "启用订阅组")}
                <ResourceSelector
                  kind="sources"
                  label="订阅源"
                  value={fields.source_ids ?? []}
                  onChange={(v) => setField("source_ids", v)}
                />
                <ResourceSelector
                  kind="nodes"
                  label="独立节点"
                  value={fields.node_ids ?? []}
                  onChange={(v) => setField("node_ids", v)}
                />
                <ResourceSelector
                  kind="operators"
                  label="处理链"
                  value={fields.operator_ids ?? []}
                  onChange={(v) => setField("operator_ids", v)}
                />
                <h4>节点前缀覆盖</h4><PrefixFields inherit value={fields.prefix_settings??{}} change={v=>setField("prefix_settings",v)}/><p className="muted">留空继承全局；只改变库存节点的名称，不增加授权。</p>
<div className="actions"><Switch id={fieldId+"skip-defaults"} checked={fields.skip_default_operators===true} onCheckedChange={v=>setField("skip_default_operators",v)}/><Label htmlFor={fieldId+"skip-defaults"}>无独立步骤时跳过全局默认处理</Label></div><p className="muted">默认使用设置中的全局处理链；有独立步骤时完整覆盖默认。此开关只影响空链。</p>
                <ResourceSelector
                  kind="users"
                  label="应用处理链的用户"
                  value={fields.user_ids ?? []}
                  onChange={(v) => setField("user_ids", v)}
                />
                <ResourceSelector
                  kind="templates"
                  label="输出模板"
                  value={fields.template_ids ?? []}
                  onChange={(v) => setField("template_ids", v)}
                />
                <p>源与节点供管理员预览；用户仍只获得其已授权 Gate 节点。</p>
              </>
            )}
          </>
        )}
        {advanced && (
          <>
            <Field label={<>{kind} JSON</>} htmlFor={fieldId + "json"}><Textarea
              id={fieldId + "json"}
              value={text}
              onChange={(e) => T(e.target.value)}
              rows={7}
            /></Field>
          </>
        )}
        <Button type="submit" disabled={busy}>
          {busy ? "保存中…" : "保存"}
        </Button>
        <div role="status">{message}</div>
      </form>
    </Panel>
  );
}
