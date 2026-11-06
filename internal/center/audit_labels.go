package center

import (
	"context"
	"sort"
	"strings"
)

// auditLabels 是审计操作的中文名；没有登记的操作显示原始编码，不猜。
var auditLabels = map[string]string{
	"ai.apply": "确认 dsh 草稿", "ai.discard": "放弃 dsh 草稿", "ai.draft": "dsh 提交草稿", "ai.withdraw": "dsh 撤回草稿", "ai.provider.delete": "删除旧模型配置",
	"api_token.create": "新建 API 令牌", "api_token.delete": "删除 API 令牌",
	"auth.email": "修改邮箱", "auth.login": "登录", "auth.passkey.delete": "删除通行密钥", "auth.passkey.enroll": "添加通行密钥", "auth.password.change": "修改密码",
	"auth.register": "注册", "auth.reset": "重置密码", "auth.session.revoke": "退出登录会话", "auth.totp.disable": "关闭二次验证", "auth.totp.enroll": "开启二次验证",
	"backup.import": "导入备份", "backup.webdav": "修改 WebDAV 备份设置", "backup.webdav.restore": "从 WebDAV 恢复", "backup.webdav.run": "执行 WebDAV 备份",
	"cert.create": "添加证书", "cert.delete": "删除证书", "cert.renew": "续签证书", "certs.settings": "修改节点证书设置",
	"chain.create": "新建链式代理", "chain.delete": "删除链式代理", "chain.update": "修改链式代理",
	"defense.analyze": "分析安全事件", "defense.block": "封禁来源", "defense.dismiss": "忽略安全事件", "defense.laya": "修改 Laya 设置", "defense.laya.delete": "删除 Laya 设置", "defense.laya.test": "测试 Laya", "defense.unblock": "解除封禁",
	"driver.restore": "恢复驱动", "driver.rollback": "回退驱动", "driver.save": "保存驱动",
	"dsh.fetch": "下载 dsh", "dsh.open": "打开 dsh", "dsh.probe": "dsh 实测", "dsh.remove": "删除 dsh 版本", "dsh.restart": "重启 dsh", "dsh.settings": "修改 dsh 设置", "dsh.version": "切换 dsh 版本",
	"gate.invite": "生成 VPS 安装命令", "gate.kernel.restart": "重启内核", "gate.pair": "VPS 接入", "gate.panel.discover": "检测面板", "gate.panel.remove": "移除面板授权", "gate.panel.set": "设置面板授权",
	"gate.revoke": "撤销 VPS", "gate.ssh_install": "SSH 安装 VPS", "gate.update": "修改 VPS", "guestbook.config": "修改留言板设置",
	"job.cancel": "取消任务", "kernel.defaults": "修改默认内核版本", "kernel.fetch": "下载内核", "kernel.remove": "删除内核版本",
	"link.create": "新建订阅链接", "link.delete": "删除订阅链接", "link.rotate": "更换订阅地址", "link.update": "修改订阅链接",
	"node.create": "新建节点", "node.delete": "删除节点", "node.update": "修改节点",
	"exit.delete": "删除出口", "exit.save": "保存出口", "exit.probe": "实测出口", "page.delete": "删除公开页面", "page.save": "保存公开页面",
	"proxy.type_exit": "给类型加出口", "proxy.type_exit_delete": "去掉类型出口", "proxy.update": "修改代理设置", "proxy.custom_rules": "修改自定义清单",
	"route.delete": "删除分流规则", "route.save": "保存分流规则",
	"settings.announcement": "修改全局公告", "settings.notify": "修改通知设置", "settings.security": "修改安全设置", "settings.smtp": "修改邮件设置",
	"subscription.nodes.import": "导入手动节点", "subscription.profile.delete": "删除订阅组", "subscription.profile.grants": "分配订阅组", "subscription.profile.save": "保存订阅组",
	"subscription.profile.share": "分享订阅组", "subscription.profile.unshare": "取消分享订阅组", "subscription.settings": "修改订阅默认设置", "subscription.source.delete": "删除订阅源", "subscription.source.save": "保存订阅源",
	"system.install": "安装系统", "system.rollback": "回退版本", "system.update": "更新版本",
	"user.create": "新建用户", "user.delete": "删除用户", "user.reset_usage": "清零用量", "user.update": "修改用户",
}

func auditActionLabel(code string) string {
	if v, ok := auditLabels[code]; ok {
		return v
	}
	return code
}

func auditActionsMatching(q string) []string {
	var out []string
	for code, label := range auditLabels {
		if strings.Contains(label, q) {
			out = append(out, code)
		}
	}
	sort.Strings(out)
	return out
}

// auditActorName 是操作者的邮箱；账户已删除时写明。actors 是这一页的缓存。
func (s *Server) auditActorName(ctx context.Context, id int, actors map[int]string) string {
	if id == 0 {
		return ""
	}
	if v, ok := actors[id]; ok {
		return v
	}
	name := "账户 #" + itoa(id) + "（已删除）"
	if u, err := s.Store.Ent.User.Get(ctx, id); err == nil {
		name = firstNonEmpty(u.Email, u.Name)
	}
	actors[id] = name
	return name
}

var auditObjectNames = map[string]string{"gate": "VPS", "node": "节点", "user": "用户", "link": "链接", "chain": "链式代理", "driver": "驱动", "kernel": "内核版本",
	"source": "订阅源", "profile": "订阅组", "page": "公开页面", "cert": "证书", "audit": "审计记录", "exit": "出口", "route": "分流规则"}

// auditTargetName 把「类型:编号」换成对象名称；对象已删除或不是这种格式时说明 / 原样返回。
func (s *Server) auditTargetName(ctx context.Context, target string) string {
	typ, key, ok := strings.Cut(target, ":")
	label, known := auditObjectNames[typ]
	if !ok || !known {
		return target
	}
	if ref, found := s.resolveObject(ctx, typ, key); found && ref.Name != "" {
		return label + " " + ref.Name
	}
	return label + " #" + key + "（已删除或不可查）"
}
