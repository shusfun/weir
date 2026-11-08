package center

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/shusfun/weir/internal/store/ent"
	"github.com/shusfun/weir/internal/store/ent/credential"
)

var countryShortNames = map[string]string{
	"CN": "中国", "HK": "香港", "MO": "澳门", "TW": "台湾", "JP": "日本", "KR": "韩国", "SG": "新加坡", "MY": "马来西亚", "TH": "泰国", "VN": "越南", "PH": "菲律宾", "ID": "印度尼西亚", "IN": "印度", "PK": "巴基斯坦", "BD": "孟加拉国", "NP": "尼泊尔", "LK": "斯里兰卡", "KH": "柬埔寨", "LA": "老挝", "MM": "缅甸", "MN": "蒙古", "KZ": "哈萨克斯坦", "UZ": "乌兹别克斯坦",
	"US": "美国", "CA": "加拿大", "MX": "墨西哥", "BR": "巴西", "AR": "阿根廷", "CL": "智利", "CO": "哥伦比亚", "PE": "秘鲁", "PA": "巴拿马", "CR": "哥斯达黎加",
	"GB": "英国", "DE": "德国", "FR": "法国", "NL": "荷兰", "BE": "比利时", "LU": "卢森堡", "CH": "瑞士", "AT": "奥地利", "IT": "意大利", "ES": "西班牙", "PT": "葡萄牙", "IE": "爱尔兰", "SE": "瑞典", "NO": "挪威", "FI": "芬兰", "DK": "丹麦", "IS": "冰岛", "PL": "波兰", "CZ": "捷克", "SK": "斯洛伐克", "HU": "匈牙利", "RO": "罗马尼亚", "BG": "保加利亚", "GR": "希腊", "TR": "土耳其", "UA": "乌克兰", "RU": "俄罗斯", "EE": "爱沙尼亚", "LV": "拉脱维亚", "LT": "立陶宛", "RS": "塞尔维亚", "HR": "克罗地亚", "SI": "斯洛文尼亚", "AL": "阿尔巴尼亚", "MD": "摩尔多瓦", "BY": "白俄罗斯",
	"AU": "澳大利亚", "NZ": "新西兰", "AE": "阿联酋", "SA": "沙特阿拉伯", "IL": "以色列", "IR": "伊朗", "IQ": "伊拉克", "QA": "卡塔尔", "BH": "巴林", "OM": "阿曼", "JO": "约旦", "ZA": "南非", "EG": "埃及", "NG": "尼日利亚", "KE": "肯尼亚", "MA": "摩洛哥", "TN": "突尼斯", "MU": "毛里求斯", "SC": "塞舌尔",
}

func subscriptionPlace(p geoPlace) string {
	if !p.Known || p.Country == "" {
		return "地区未知"
	}
	code := strings.ToUpper(p.Country)
	if code == "US" {
		switch {
		case p.Lon < -104:
			return "美西"
		case p.Lon > -87:
			return "美东"
		default:
			return "美中"
		}
	}
	if name := countryShortNames[code]; name != "" {
		return name
	}
	return code
}

type subscriptionEntry struct {
	Entry
	node       *ent.Node
	gate       *ent.Gate
	credential *ent.Credential
	chain      *ent.Chain
	exit       *ent.Exit
}

func (s *Server) selectSubscriptionEntries(ctx context.Context, u *ent.User, l *ent.Link) ([]subscriptionEntry, error) {
	creds, err := s.Store.Ent.Credential.Query().Where(credential.LinkIDEQ(l.ID), credential.InternalEQ(false), credential.ProbeParentIDIsNil()).Order(ent.Asc(credential.FieldNodeID), ent.Asc(credential.FieldID)).All(ctx)
	if err != nil {
		return nil, err
	}
	disabled := map[string]bool{}
	for _, t := range loadPolicy(u).Disabled {
		disabled[t] = true
	}
	ti := s.typeIndex(ctx)
	authorized, err := s.userNodeIDs(ctx, u)
	if err != nil {
		return nil, err
	}
	viaExit := map[int]bool{}
	if u.AllowChain {
		for _, c := range creds {
			if c.ChainID != nil {
				ch, err := s.Store.Ent.Chain.Get(ctx, *c.ChainID)
				if err == nil && ch.Enabled && s.exitPolicy(ctx, ch.ExitID) != "" {
					viaExit[c.NodeID] = true
				}
			}
		}
	}
	out := []subscriptionEntry{}
	for _, c := range creds {
		if authorized[c.NodeID] == nil {
			continue
		}
		n, err := s.Store.Ent.Node.Get(ctx, c.NodeID)
		if err != nil || !n.Enabled {
			continue
		}
		typ := ti.of(n).Key
		if disabled[typ] || c.ChainID == nil && viaExit[c.NodeID] {
			continue
		}
		g, err := s.Store.Ent.Gate.Get(ctx, n.GateID)
		if err != nil || g.Status != "active" {
			continue
		}
		e := subscriptionEntry{Entry: Entry{NodeID: n.ID, Protocol: n.Protocol, Type: typ, Host: g.PublicHost, Port: n.Port, UUID: c.UUID, Password: c.Password}, node: n, gate: g, credential: c}
		eid := n.ExitID
		if c.ChainID != nil {
			if !u.AllowChain {
				continue
			}
			ch, err := s.Store.Ent.Chain.Get(ctx, *c.ChainID)
			if err != nil || !ch.Enabled || ch.UserID != u.ID || ch.EntryNodeID != n.ID {
				continue
			}
			e.chain, e.ChainID = ch, ch.ID
			eid = ch.ExitID
		}
		ex, err := s.Store.Ent.Exit.Get(ctx, eid)
		if err != nil {
			return nil, err
		}
		if !ex.Enabled {
			continue
		}
		e.Ref = "node:" + itoa(n.ID)
		if e.chain != nil && ex.PolicyType == "" {
			e.Ref = "chain:" + itoa(e.ChainID)
		}
		e.exit = ex
		e.Chained = ex.Kind != "freedom"
		out = append(out, e)
	}
	return out, nil
}

func (s *Server) nameSubscriptionEntries(ctx context.Context, es []subscriptionEntry) {
	counts := map[string]int{}
	for i := range es {
		e := &es[i]
		prefix := "[直连] "
		if e.Chained {
			prefix = "[链式] "
		}
		label := s.nodeDisplayName(ctx, e.node, e.gate, e.exit, e.chain)
		e.Name = prefix + label
		counts[e.Name]++
	}
	seq := map[string]int{}
	for i := range es {
		base := es[i].Name
		if counts[base] > 1 {
			seq[base]++
			es[i].Name = fmt.Sprintf("%s %02d", base, seq[base])
		}
	}
}

func (s *Server) renderSubscriptionEntries(ctx context.Context, es []subscriptionEntry) []Entry {
	out := make([]Entry, 0, len(es))
	for _, e := range es {
		var cert *ent.Certificate
		var err error
		if e.node.CertID != nil {
			cert, err = s.Store.Ent.Certificate.Get(ctx, *e.node.CertID)
			if err != nil {
				e.Error = "证书状态未知"
			} else if ok, why := certUsable(cert, time.Now()); !ok {
				e.Error = why
			}
		}
		if e.Error == "" {
			e.URI, err = shareLink(e.node, e.gate.PublicHost, e.credential, e.Name, cert)
			if err != nil {
				e.Error = err.Error()
			}
		}
		out = append(out, e.Entry)
	}
	return out
}

func subscriptionInfoLinks(u *ent.User, l *ent.Link) []string {
	remaining := "剩余 不限"
	if u.Quota > 0 {
		remaining = fmt.Sprintf("剩余 %.1f GB", float64(max(int64(0), u.Quota-u.Used))/(1<<30))
	}
	expiry := "到期 长期有效"
	if _, end := linkWindow(u, l); end != nil {
		expiry = "到期 " + end.Format("2006-01-02")
	}
	out := []string{}
	for _, text := range []string{remaining, expiry} {
		out = append(out, (&url.URL{Scheme: "trojan", User: url.User("weir-info"), Host: "127.0.0.1:1", RawQuery: "security=none&type=tcp", Fragment: text}).String())
	}
	return out
}
