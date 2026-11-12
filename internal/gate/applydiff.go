package gate

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// userChange 描述一份内核配置里，相对上一份发生的用户增减或同 email 凭据变化。
type userChange struct {
	Tag      string
	Protocol string
	Email    string
	Add      json.RawMessage
}

// userDelta 判断两份已生成的内核配置是否只有入站用户不同。
// 其余字段逐字相同才返回 true。同 email 的 UUID、密码、flow 等变化记成先删再加。
// sing-box 的用户不走这条路径。
func userDelta(oldBody, newBody []byte) (added, removed []userChange, only bool) {
	var oldDoc, newDoc map[string]any
	if json.Unmarshal(oldBody, &oldDoc) != nil || json.Unmarshal(newBody, &newDoc) != nil {
		return nil, nil, false
	}
	oldUsers := takeClients(oldDoc)
	newUsers := takeClients(newDoc)
	oldRest, err1 := json.Marshal(oldDoc)
	newRest, err2 := json.Marshal(newDoc)
	if err1 != nil || err2 != nil || !bytes.Equal(oldRest, newRest) {
		return nil, nil, false
	}
	for tag, users := range newUsers {
		for email, raw := range users {
			old, exists := oldUsers[tag][email]
			if !exists {
				added = append(added, userChange{Tag: tag, Protocol: protocolOf(newDoc, tag), Email: email, Add: raw})
				continue
			}
			if !sameJSON(old, raw) {
				removed = append(removed, userChange{Tag: tag, Email: email})
				added = append(added, userChange{Tag: tag, Protocol: protocolOf(newDoc, tag), Email: email, Add: raw})
			}
		}
	}
	for tag, users := range oldUsers {
		for email := range users {
			if _, ok := newUsers[tag][email]; !ok {
				removed = append(removed, userChange{Tag: tag, Email: email})
			}
		}
	}
	return added, removed, true
}

func protocolOf(doc map[string]any, tag string) string {
	inbounds, _ := doc["inbounds"].([]any)
	for _, item := range inbounds {
		in, _ := item.(map[string]any)
		if got, _ := in["tag"].(string); got == tag {
			protocol, _ := in["protocol"].(string)
			return protocol
		}
	}
	return ""
}

func sameJSON(a, b json.RawMessage) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return bytes.Equal(a, b)
	}
	ab, err1 := json.Marshal(left)
	bb, err2 := json.Marshal(right)
	return err1 == nil && err2 == nil && bytes.Equal(ab, bb)
}

func takeClients(doc map[string]any) map[string]map[string]json.RawMessage {
	out := map[string]map[string]json.RawMessage{}
	inbounds, _ := doc["inbounds"].([]any)
	for _, item := range inbounds {
		in, _ := item.(map[string]any)
		tag, _ := in["tag"].(string)
		settings, _ := in["settings"].(map[string]any)
		if tag == "" || settings == nil {
			continue
		}
		clients, _ := settings["clients"].([]any)
		if clients == nil {
			continue
		}
		out[tag] = map[string]json.RawMessage{}
		for _, client := range clients {
			raw, err := json.Marshal(client)
			if err != nil {
				continue
			}
			var fields map[string]any
			_ = json.Unmarshal(raw, &fields)
			email, _ := fields["email"].(string)
			if email == "" {
				continue
			}
			out[tag][email] = raw
		}
		delete(settings, "clients")
	}
	return out
}

func handlerUser(protocol string, client json.RawMessage) ([]byte, error) {
	var fields map[string]any
	if err := json.Unmarshal(client, &fields); err != nil {
		return nil, err
	}
	email, _ := fields["email"].(string)
	if email == "" {
		return nil, fmt.Errorf("user has no email")
	}
	account := map[string]any{}
	switch protocol {
	case "vless", "vmess":
		account["id"] = fields["id"]
		if flow, ok := fields["flow"]; ok {
			account["flow"] = flow
		}
	case "trojan", "shadowsocks":
		account["password"] = fields["password"]
	default:
		if id, ok := fields["id"]; ok {
			account["id"] = id
		}
		if password, ok := fields["password"]; ok {
			account["password"] = password
		}
		if flow, ok := fields["flow"]; ok {
			account["flow"] = flow
		}
	}
	return json.Marshal(map[string]any{
		"email":   email,
		"level":   0,
		"account": account,
	})
}
