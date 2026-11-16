// 原型 · 系统设置：现有每一项都在，一页分组列表，不切标签；左侧是分区索引（锚点）。
// 开关点了即生效；要填的项显示当前值，点「修改」在弹窗里改。管理员自己的账户安全放在第一组。
import { useState } from "react";
import {
  Badge, Button, Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, Field, InsetGroup, InsetRow, InsetRows, Input, Notice, PageHeader,
  Segmented, SwitchField, TextLink, useToast,
} from "@weir/ui";



type Row =
  | { kind: "switch"; label: string; desc?: string; on: boolean }
  | { kind: "value"; label: string; desc?: string; value: string; tone?: "ok" | "warn" | "bad" | "neutral"; action?: string }
  | { kind: "theme" };

const sections: { id: string; title: string; desc: string; rows: Row[] }[] = [
  { id: "me", title: "我的账户", desc: "管理员自己的登录安全", rows: [
    { kind: "value", label: "密码", desc: "改完当前设备也要重新登录", value: "30 天前修改", action: "修改" },
    { kind: "switch", label: "两步验证", desc: "登录时要验证码", on: true },
    { kind: "value", label: "通行密钥", value: "1 个 · 本机 Windows Hello", action: "管理" },
    { kind: "value", label: "登录会话", value: "2 个，含当前", action: "查看" },
  ] },
  { id: "look", title: "外观", desc: "保存在账户里，换浏览器登录也一样", rows: [
    { kind: "theme" },
    { kind: "value", label: "动效", value: "均衡", action: "修改" },
    { kind: "switch", label: "背景地球", on: true },
    { kind: "switch", label: "跟随系统的「减少动态效果」", desc: "系统开启时自动进入省电", on: true },
    { kind: "switch", label: "检测到卡顿时提示切换", desc: "同一浏览器 7 天内最多提示一次", on: true },
  ] },
  { id: "update", title: "版本与更新", desc: "中心、Gate 与内核从哪里下载", rows: [
    { kind: "value", label: "版本与下载", value: "0.1.15 · 有新版本 0.1.16", tone: "warn", action: "更新" },
    { kind: "value", label: "GitHub 加速", desc: "按优先级尝试，失败自动换下一个", value: "镜像前缀 2 个 · VPS 中转 1 个", action: "修改" },
  ] },
  { id: "login", title: "登录与注册", desc: "登录方式、注册与发信", rows: [
    { kind: "value", label: "Logo", desc: "登录页和控制台使用的标识", value: "默认", action: "上传" },
    { kind: "switch", label: "允许二次验证（TOTP）", on: true },
    { kind: "switch", label: "允许通行密钥登录", on: true },
    { kind: "switch", label: "管理员必须开启二次验证", on: true },
    { kind: "switch", label: "允许自助注册", desc: "邀请码可附带初始套餐；未附带时新用户没有流量与节点", on: false },
    { kind: "switch", label: "注册时验证邮箱", desc: "需要邮件服务（下方 SMTP 未配置）", on: false },
    { kind: "value", label: "账户验证与发信策略", value: "版本 v3 · 已确认", action: "查看" },
    { kind: "value", label: "Turnstile 人机验证", desc: "可选", value: "未启用", action: "配置" },
    { kind: "value", label: "邮件（SMTP）", desc: "找回密码与注册验证；465 直接 TLS，其他端口 STARTTLS", value: "未配置", tone: "warn", action: "配置" },
  ] },
  { id: "notify", title: "通知", desc: "节点与证书出问题时找到你", rows: [
    { kind: "switch", label: "开启 Telegram 通知", on: true },
    { kind: "switch", label: "VPS 离线超过 5 分钟时提醒", on: true },
    { kind: "value", label: "Bot Token / Chat ID", value: "已设置 · 测试消息 14:02 送达", tone: "ok", action: "修改" },
  ] },
  { id: "defense", title: "防御", desc: "基础防御不依赖模型", rows: [
    { kind: "switch", label: "允许受限的安全临时收紧", desc: "只对可信安全事件，受护栏限制", on: false },
    { kind: "value", label: "受保护域名 / 网段", value: "3 个域名 · 2 个网段", action: "修改" },
    { kind: "value", label: "护栏", desc: "最长生效时间、滚动一小时最多操作数、禁止自动调整的字段", value: "60 分钟 · 每小时 5 次 · 4 个字段", action: "修改" },
    { kind: "value", label: "自动安全处置策略", value: "版本 v3 · 配置来源：内置", action: "查看" },
  ] },
  { id: "ai", title: "AI 与 dsh", desc: "模型故障不影响基础功能", rows: [
    { kind: "switch", label: "启用 dsh", on: true },
    { kind: "value", label: "dsh", desc: "地址、DeepSeek Key", value: "运行中 0.9.3 · 有新版本 0.9.4", tone: "warn", action: "更新" },
    { kind: "switch", label: "Laya 判为可疑的事件自动交给 dsh 分析", on: true },
    { kind: "switch", label: "启用 Laya 判断", on: false },
    { kind: "value", label: "Laya", desc: "地址、模型、推理位置、执行 VPS、测试时限", value: "未部署", action: "部署" },
    { kind: "value", label: "AI 外置配置", value: "版本 v2 · 已确认", action: "查看" },
  ] },
  { id: "certs", title: "证书", desc: "节点证书与本机面板证书", rows: [
    { kind: "value", label: "节点证书", desc: "域名兜底、ACME 账户邮箱、DNS API 密钥", value: "1 张 2 天后到期 · 上次续签失败：80 端口被占用", tone: "warn", action: "重试续签" },
    { kind: "value", label: "本机面板证书", desc: "1Panel 端口与 HTTPS", value: "未接入", action: "配置" },
  ] },
  { id: "data", title: "数据", desc: "保留、备份与接口令牌", rows: [
    { kind: "value", label: "历史保留策略", value: "审计 180 天 · 观测 30 天", action: "修改" },
    { kind: "switch", label: "自动备份", desc: "每 24 小时一次，保留 7 份，存到 WebDAV", on: true },
    { kind: "value", label: "备份", value: "今天 03:00 成功", tone: "ok", action: "立即备份" },
    { kind: "value", label: "导入备份", desc: "先预览新增与覆盖，确认后才写入", value: "—", action: "导入" },
    { kind: "value", label: "API 令牌", value: "2 个", action: "管理" },
  ] },
];

export function ProtoSystem() {
  const toast = useToast();
  const [on, setOn] = useState<Record<string, boolean>>(() => Object.fromEntries(sections.flatMap((s) => s.rows.flatMap((r) => (r.kind === "switch" ? [[r.label, r.on]] : [])))));
  const [theme, setTheme] = useState("system");
  const [edit, setEdit] = useState<string | null>(null);
  return (
    <div className="stack">
      <PageHeader eyebrow="系统" title="设置" description="所有设置在一页；开关点了即生效，要填的项点「修改」在弹窗里改。" />
      <div className="grid items-start gap-[var(--weir-space-5)] lg:grid-cols-[181px_minmax(0,1fr)]">
        <nav aria-label="设置分区" className="stack-tight lg:sticky lg:top-[var(--weir-space-5)]">
          {sections.map((s) => <TextLink key={s.id} render={<a href={`#set-${s.id}`} />}>{s.title}</TextLink>)}
        </nav>
        <div className="stack">
          {sections.map((s) => (
            <InsetGroup key={s.id} id={`set-${s.id}`} title={s.title} description={s.desc}>
              <InsetRows>
                {s.rows.map((r, i) => r.kind === "switch" ? (
                  <InsetRow key={i}><SwitchField placement="end" label={r.label} description={r.desc} checked={on[r.label]} onCheckedChange={(c) => { setOn({ ...on, [r.label]: c }); toast(`${r.label}：已${c ? "打开" : "关闭"}（原型）`); }} /></InsetRow>
                ) : r.kind === "theme" ? (
                  <InsetRow key={i} className="actions justify-between"><strong>主题</strong><Segmented aria-label="主题" value={theme} onValueChange={setTheme} options={[["system", "跟随系统"], ["dark", "深色"], ["light", "浅色"]]} /></InsetRow>
                ) : (
                  <InsetRow key={i} className="actions justify-between">
                    <span className="stack-tight"><strong>{r.label}</strong>{r.desc && <small className="text-[var(--weir-ink-3)]">{r.desc}</small>}</span>
                    <span className="actions">
                      {r.tone ? <Badge variant="soft" tone={r.tone}>{r.value}</Badge> : <small className="text-[var(--weir-ink-2)]">{r.value}</small>}
                      {r.action && <Button size="sm" variant="outline" onClick={() => setEdit(r.label)}>{r.action}</Button>}
                    </span>
                  </InsetRow>
                ))}
              </InsetRows>
            </InsetGroup>
          ))}
        </div>
      </div>
      <Dialog open={!!edit} onOpenChange={(o) => !o && setEdit(null)}>
        <DialogContent>
          <DialogHeader><DialogTitle>{edit}</DialogTitle><DialogDescription>原型里用一个示例表单代表各项的详情弹窗；正式实现沿用现有表单内容。</DialogDescription></DialogHeader>
          <div className="stack"><Field label="示例字段" name="demo"><Input placeholder="现有表单的字段放这里" /></Field></div>
          <DialogFooter><Button variant="ghost" onClick={() => setEdit(null)}>取消</Button><Button onClick={() => { toast(`${edit}：已保存（原型）`, { tone: "ok" }); setEdit(null); }}>保存</Button></DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}


export function ProtoMe() {
  return (
    <div className="stack">
      <PageHeader title="账户安全" description="普通用户的密码、两步验证与登录会话，沿用现有页面。" />
      <Notice tone="info">原型不重画这一页。</Notice>
    </div>
  );
}
