package center

func exitBody(args map[string]any) map[string]any {
	return pick(args, "gate_id", "remark", "kind", "address", "port", "username", "password", "link", "source_id", "profile_id", "mode", "match", "node_id", "enabled")
}

func exitKindOptions() [][2]string {
	out := [][2]string{}
	for _, k := range exitKinds {
		if k.Kind != "freedom" {
			out = append(out, [2]string{k.Kind, k.Label})
		}
	}
	return out
}
