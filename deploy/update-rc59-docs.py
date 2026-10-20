"""Append rc59 scope while retaining acceptance IDs and historical evidence."""
from pathlib import Path
import json

p=Path('docs/implementation.html')
s=p.read_text(encoding='utf-8')
s=s.replace('中心 / Gate · 0.2.0-rc57','中心 / Gate · 0.2.0-rc58')
s=s.replace('rc58 · DNS-01 持久清理（本地验证中）','rc58 · DNS-01 持久清理（两台发布 / 相关验收通过）')
s=s.replace('两台发布、域名空状态 / 错误重试、四宽度逐图待验收','两台 SHA-256 / 备份 / 安装通过；域名真实空状态 / 503 重试 / 不存在目标拒绝及四宽度逐图通过，见 dns-cleanup-ui.json。填充台账仍为隔离供应商证据')
section='''<h2 id="rc59">rc59 · 公开目录与主题（本地验证通过 / 待发布）</h2><div class="table"><table><tr><th>功能</th><th>交互 / 安全</th><th>验收状态</th></tr><tr><td>独立公开页面模块，标题 / 介绍 / 公告，海蓝 / 森林 / 紫罗兰主题</td><td>UIAble 表单、可搜索目录选择器、顺序调整；默认不发布、不显示登录、不分享</td><td>本地 TypeScript / Linux Go / PG18 / Redis8 通过；域名 UI 与四宽度逐图待验收</td></tr><tr><td>公开页目录卡片、复制地址、页面二维码</td><td>只解析显式选择且仍发布的文本页面；卡片仅标题 / 简介 / 路径，API no-store。无用户 / 库存 / 令牌；撤下立即停止匿名 API 发布</td><td>隔离 PG 验证兼容默认值、字段白名单、撤下、禁止自身 / 无效引用、普通用户隔离；不等于匿名订阅导出已完成</td></tr><tr><td>范围保持</td><td>公开订阅导出需单独显式发布契约；留言板与完整伪装主题继续迁移。任意远程 JS 不恢复</td><td>SMTP / Laya / DNS 真服务与商业客户端的外部阻塞不变；R1–R5 未完成</td></tr></table></div><p>rc58 首次客户端在 Gate 更新结束前出现 TLS EOF，原因未确认；失败保留 <a href="evidence/rc58-initial-client-failure.json">原始观察</a>。两台就绪后双用户 Xray 真实出口与模板 / 隔离重测通过，见 live-client.json / live-templates.json。</p>'''
if 'id="rc59"' not in s: s=s.replace('<h2 id="stages">',section+'<h2 id="stages">')
p.write_text(s,encoding='utf-8')
p=Path('docs/migration.html');s=p.read_text(encoding='utf-8')
s=s.replace('逐操作迁移台账 · rc31–rc58','逐操作迁移台账 · rc31–rc59')
s=s.replace('rc56 登录 / MFA / Passkey / 恢复码 / 改密 / 会话撤销真实通过；关闭注册页视觉修复与初始化重建 rc57 待发版；真实邮件恢复外部阻塞','rc56 / rc57 登录 / MFA / Passkey / 恢复码 / 改密 / 会话撤销及关闭注册视觉修复通过；rc57 隔离初始化全流程通过；真实邮件恢复外部阻塞')
s=s.replace('rc58 PG / 受控 DNS 测试；发布与视觉待验收；真实 DNS 外部阻塞','rc58 PG / 受控 DNS 测试与两台发布通过；域名空 / 错误 / 重试及四宽度通过，见 dns-cleanup-ui.json；真实 DNS 外部阻塞')
s=s.replace('目录 / 完整主题待补；不发布私有库存','rc59 文本页面目录 / 三主题 / 顺序 / 页面分享本地通过，待域名及视觉；公开订阅导出 / 完整伪装主题 / 留言板仍待补，不发布私有库存')
rows='''<tr><td>MiSub PublicProfiles / ProfileCard</td><td>Hero、公告、目录卡片与跳转</td><td>独立 /p/slug 模块</td><td>已发布文本页有序目录，API 七字段 / 卡片三字段白名单</td><td>rc59 本地通过，域名待验收；匿名订阅目录另需安全契约</td><td>SUB-04 / UX-01 / MIG-01</td></tr><tr><td>MiSub PublicProfiles / theme / QR</td><td>主题、复制与二维码</td><td>公开页编辑 / 页面分享</td><td>三种固定主题；只分享公开页面路径</td><td>rc59 待真实 UI / 四宽度；无自定义脚本执行能力</td><td>SUB-04 / UX-02</td></tr>'''
if '七字段 / 卡片三字段' not in s:s=s.replace('</tbody></table></div><h2>v0.3',rows+'</tbody></table></div><h2>v0.3',1)
p.write_text(s,encoding='utf-8')
p=Path('docs/design.html');s=p.read_text(encoding='utf-8')
strip='''<div class="strip"><strong>公开目录安全适配</strong><p>目录只引用显式发布的文本页面；卡片仅标题、简介与本地公开路径。默认不分享，主题采用固定预设。私有节点、用户订阅令牌与未发布内容不可匿名导出；完整订阅目录与其余上游交互保持待迁移。</p></div>'''
if '公开目录安全适配' not in s:s=s.replace('<section id="acceptance">',strip+'<section id="acceptance">')
p.write_text(s,encoding='utf-8')
p=Path('deploy/sources.json');d=json.loads(p.read_text(encoding='utf-8'))
d['public_page_reference']='MiSub 4952c16 src/views/PublicProfilesView.vue and components/public/ProfileCard.vue: hero, announcement, card grid and share interaction reference. Reimplemented in React + UIAble without Vue runtime. Security adaptation: directory only selects explicitly published text pages; no private inventory/token/anonymous subscription export, guestbook or executable custom renderer. Full migration still pending.'
p.write_text(json.dumps(d,ensure_ascii=False,indent=2)+'\n',encoding='utf-8')
p=Path('AGENTS.md');s=p.read_text(encoding='utf-8')
rule='- 公开目录只解析显式选择且仍发布的文本页面；目录卡片严格白名单，不能复用私有订阅组接口或泄漏用户令牌。匿名订阅导出需要独立发布契约与验收，不能由目录 UI 隐式开放。\n'
if rule.strip() not in s:s+='\n'+rule
p.write_text(s,encoding='utf-8')
