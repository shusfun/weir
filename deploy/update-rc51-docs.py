"""Update acceptance status without replacing historical evidence."""
from pathlib import Path

p = Path('docs/implementation.html')
s = p.read_text(encoding='utf-8')
s = s.replace('rc50 验收中 · 自动续期与选择器修复', 'rc50 已部署 · 自动续期与选择器修复')
s = s.replace('两台部署与真实 UI / 视觉仍待验收。', '两台部署、域名真实 UI 与四宽度截图人工复核通过；凭据显示名称、新建目标和中文环境。两用户独立凭据、直达 Gate 与模板隔离回归通过。真实 CA 签发 / 自动续期未验收。')
s = s.replace('中心 / Gate · 0.2.0-rc48', '中心 / Gate · 0.2.0-rc50')
s = s.replace('版本安装 / 升级 / 回滚已实测，DNS-01 与证书历史待补', '版本安装 / 升级 / 回滚已实测；DNS-01 与自动续期已实现，真实 CA 待配置，证书历史清理待补')
s = s.replace('完整故障矩阵与 DNS-01 仍待完成', '完整故障矩阵、DNS-01 真实签发及自动续期仍待验收')
s = s.replace('证书自动续签与历史版本清理仍待完成', '自动续签实现已有协议 / 数据库测试；真实 CA 续签与历史版本清理仍待完成')
anchor = '<h2 id="stages">'
section = '''<h2 id="rc51">rc51 · 续期并发与失败状态修复</h2><div class="table"><table><tr><th>维度</th><th>结果</th><th>证据 / 限制</th></tr><tr><td>功能</td><td>续期入队事务锁定并重读策略；管理员关闭后的旧扫描不能入队。策略列表在数据库按设备过滤。</td><td>Linux 全量 Go、PG / Redis 集成及上游对照通过；新增 stale scheduler 回归</td></tr><tr><td>交互</td><td>DNS 凭据读取失败显示错误 / 重试，禁止提交；不再显示为空库存。</td><td>两台发布与域名交互验收中</td></tr><tr><td>视觉</td><td>rc50 四宽度表单已逐图检查；rc51 错误状态待截图</td><td><a href="evidence/dns01-ui.json">当前 DNS 流程证据</a>；错误注入不冒充真实 DNS 服务故障</td></tr><tr><td>历史异常</td><td>rc48 Gate 更新命令曾退出 1，后续检查清单 / 链接一致且服务稳定；rc49 / rc50 更新正常。</td><td>该次非零退出根因未定，保留异常，不标为已修复</td></tr></table></div>'''
if 'id="rc51"' not in s:
    s = s.replace(anchor, section + anchor)
p.write_text(s, encoding='utf-8')

p = Path('docs/migration.html')
s = p.read_text(encoding='utf-8').replace('rc31–rc50', 'rc31–rc51')
s = s.replace('自动证书签发 / 续签与证书历史清理待补', 'DNS-01 签发 / 自动续期已实现并有协议、PG 与域名 UI 证据；公共 CA 真实签发 / 续期缺 DNS 凭据，证书历史清理待补')
p.write_text(s, encoding='utf-8')
