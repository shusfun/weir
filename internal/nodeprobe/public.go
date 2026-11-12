package nodeprobe

import (
	"context"
	"errors"
	"net"
	"strconv"

	"github.com/shusfun/weir/internal/publicnet"
)

func PublicHosts(ctx context.Context, proxies []map[string]any, target string) (map[string]any, error) {
	u, err := publicnet.URL(target)
	if err != nil { return nil, err }
	hosts := map[string]any{}
	pin := func(host string) error {
		ips, err := publicnet.Resolve(ctx, host, nil)
		if err != nil { return err }
		addresses := make([]string,len(ips))
		for i, ip := range ips { addresses[i] = ip.String() }
		if net.ParseIP(host) == nil { hosts[host] = addresses }
		return nil
	}
	if err = pin(u.Hostname()); err != nil { return nil, err }
	for _, proxy := range proxies {
		host, ok := proxy["server"].(string)
		if !ok || host == "" || proxy["dialer-proxy"] != nil || proxy["interface-name"] != nil { return nil, errors.New("probe_endpoint_invalid") }
		port := 0
		switch v := proxy["port"].(type) { case int: port=v; case float64: if v==float64(int(v)) { port=int(v) }; case string: port,_=strconv.Atoi(v) }
		if port < 1 || port > 65535 { return nil, errors.New("probe_endpoint_invalid") }
		if err = pin(host); err != nil { return nil, err }
	}
	return hosts,nil
}
