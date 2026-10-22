package sub

import (
	"strings"

	js "github.com/imzyb/MiGate/internal/sub/jscompat"
)

// 对照测试共用的代表性输入（覆盖全部协议与常见的非标准写法）。

func vmessLink(json string) string { return "vmess://" + js.Base64EncodeUTF8(json) }

func ssLink(userinfo, rest string) string { return "ss://" + js.Base64EncodeUTF8(userinfo) + rest }

var sampleLinks = []string{
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@hk1.example.com:443?encryption=none&security=reality&sni=www.apple.com&fp=chrome&pbk=PUBKEY123&sid=6ba85179&type=tcp&flow=xtls-rprx-vision#%F0%9F%87%AD%F0%9F%87%B0%20%E9%A6%99%E6%B8%AF%2001",
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@jp.example.com:8443?encryption=none&security=tls&type=ws&path=%2Fws%3Fed%3D2048&host=cdn.example.com&sni=cdn.example.com&alpn=h2,http/1.1#日本 东京 x2",
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@[2001:db8::1]:443?security=tls&type=grpc&serviceName=grpcsvc&mode=multi#IPv6 节点",
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@us.example.com:443?type=xhttp&path=/xh&host=x.example.com&mode=auto&extra=%7B%22xPaddingMethod%22%3A%22tokenish%22%2C%22xPaddingKey%22%3A%22k%22%7D#US XHTTP",
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@sg.example.com:443?type=httpupgrade&path=/up&host=up.example.com&security=reality&pbk=abc&sxm=1#新加坡 SG-01 [IPLC]",
	"vless://auto:b831381d@bad.example.com:443#auto 节点",
	"trojan://p%40ss%2Fword@tw.example.com:443?sni=tw.example.com&allowInsecure=1&type=ws&path=/tr&host=tw.example.com#台湾 TW 01",
	"trojan://00000000-0000-0000-0000-000000000000@127.0.0.1:443#剩余流量：100GB",
	"trojan://00000000-0000-0000-0000-000000000000@127.0.0.1:443#流量剩余 10G",
	"trojan://pass@kr.example.com:443?security=reality&pbk=key&sid=01&peer=peer.example.com&dp=前置#🇰🇷 韩国 Reality",
	"ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ=@ss.example.com:8388#%E7%BE%8E%E5%9B%BD%20SS",
	"ss://YWVzLTEyOC1nY206cGFzcw@ss2.example.com:443?plugin=obfs-local%3Bobfs%3Dtls%3Bobfs-host%3Dbing.com#UK obfs",
	"ss://chacha20-ietf-poly1305:plainpass@ss3.example.com:1234#明文 SS",
	"ss://rc4-md5:legacy@ss4.example.com:1234#legacy",
	"hysteria2://letmein@hy.example.com:443?sni=hy.example.com&insecure=1&obfs=salamander&obfs-password=obfspw&mport=20000-30000#德国 Hy2",
	"hy2://p%26w@hy2.example.com:8443?up=100&down=200&fast_open=1&realm-id=r1&stun-servers=a.com,b.com#hy2 别名",
	"hysteria://auth@hy1.example.com:443?protocol=udp#Hysteria v1",
	"tuic://uuid-1234:pa%3Ass@tuic.example.com:443?sni=tuic.example.com&alpn=h3&congestion_control=bbr&udp_relay_mode=native&zero_rtt_handshake=1#法国 TUIC",
	"tuic://onlyuuid@tuic2.example.com:443#TUIC v4",
	"snell://psk123@snell.example.com:6333?version=4&obfs=http&obfs-host=bing.com&reuse=true#Snell 加拿大",
	"snell://psk@snell2.example.com:6333?ecn=true&tfo=false#snell ecn",
	"anytls://pwd@anytls.example.com:443?sni=a.example.com&insecure=1#AnyTLS 荷兰",
	"wireguard://cHJpdmF0ZWtleQ%3D%3D@wg.example.com:51820?publickey=cHVibGlja2V5&address=10.0.0.2/32,fd00::2/128&reserved=1,2,3&mtu=1280&dns=1.1.1.1#WARP 俄罗斯",
	"wireguard://key@[2606:4700::1]:2408?address=172.16.0.2&allowedips=0.0.0.0/0#WG IPv6",
	"socks5://user:pass@socks.example.com:1080?tls=1&sni=s.example.com#SOCKS 印度",
	"socks5://u@socks2.example.com:1080#socks 只有用户",
	"https://user:pa%20ss@proxy.example.com:443?sni=p.example.com#HTTPS 代理",
	"http://proxy2.example.com:8080#HTTP 代理",
	"mierus://mieru.example.com:2999?username=u&password=p&transport=TCP&udp=true&mtu=1400#Mieru 节点",
	"naive+https://user:pass@naive.example.com:443#Naive",
	"ssr://" + strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(js.Base64EncodeUTF8("ssr.example.com:8388:auth_aes128_md5:aes-256-cfb:tls1.2_ticket_auth:"+
		strings.TrimRight(js.Base64EncodeUTF8("ssrpass"), "=")+"/?obfsparam="+strings.TrimRight(js.Base64EncodeUTF8("obfs.com"), "=")+
		"&remarks="+strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(js.Base64EncodeUTF8("SSR 香港 节点")), "=")+"&group=Zw")), "="),
	vmessLink(`{"v":"2","ps":"🇺🇸 美国 VMess","add":"vm.example.com","port":"443","id":"b831381d-6324-4d53-ad4f-8cda48b30811","aid":"0","scy":"auto","net":"ws","type":"none","host":"vm.example.com","path":"/vm","tls":"tls","sni":"vm.example.com","alpn":"h2, http/1.1","fp":"chrome"}`),
	vmessLink(`{"v":"2","ps":"VMess gRPC","add":"vm2.example.com","port":443,"id":"uuid","aid":2,"net":"grpc","path":"svc","tls":""}`),
	vmessLink(`{"v":"2","ps":"VMess h2","add":"vm3.example.com","port":"8443","id":"uuid","net":"h2","host":"a.com, b.com","path":"/h2","tls":"tls"}`),
	vmessLink(`{"v":"2","ps":"VMess http","add":"vm4.example.com","port":"80","id":"uuid","net":"http","host":"","path":""}`),
	vmessLink(`{"v":"2","ps":"auto vmess","add":"vm5.example.com","port":"80","id":"auto:123"}`),
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@local.example.com:443?security=none#本机 localhost",
	"vless://b831381d-6324-4d53-ad4f-8cda48b30811@127.0.0.1:1080#本地回环",
	"trojan://pw@weird.example.com:443#%E4%B8%AD%E6%96%87%",
	"trojan://pw@plus.example.com:443#a+b%2Bc",
	"vless://uuid@hostonly.example.com?type=ws#无端口",
	"ss://" + js.Base64EncodeUTF8("2022-blake3-aes-256-gcm:"+js.Base64EncodeUTF8("0123456789abcdef")) + "@ss2022.example.com:443#SS2022 错配",
	"ss://" + js.Base64EncodeUTF8("2022-blake3-aes-128-gcm:"+js.Base64EncodeUTF8("0123456789abcdef")) + "@ss2022b.example.com:443#SS2022 正确",
	"ss://" + js.Base64EncodeUTF8("aes-256-gcm:pw@sip.example.com:8388") + "#SIP002 整体编码",
	"ss://" + strings.TrimRight(js.Base64EncodeUTF8("chacha20-ietf-poly1305:p@ss:w"), "=") + "@v2.example.com:443?plugin=v2ray-plugin%3Btls%3Bmode%3Dwebsocket%3Bhost%3Dcdn.example.com%3Bpath%3D%2Fws%3Bmux%3D0#v2ray-plugin 日本",
	"ss://" + js.Base64EncodeUTF8("aes-128-gcm:pw") + "@[2001:db8::2]:8388?plugin=simple-obfs%3Bobfs%3Dhttp%3Bobfs-host%3Da.com%5C%3Bb#IPv6 SS",
	vmessLink(`{"v":2,"ps":"VMess quic","add":"vq.example.com","port":443,"id":"uuid","aid":"x","net":"quic","type":"srtp","host":"aes-128-gcm","path":"key","tls":"reality","sni":"s.example.com"}`),
	"vmess://" + strings.TrimRight(strings.NewReplacer("+", "-", "/", "_").Replace(js.Base64EncodeUTF8(`{"ps":"URL安全 无填充","add":"vs.example.com","port":"80","id":"uuid","net":"ws","host":"h.example.com"}`)), "="),
	"vmess://bm90IGpzb24=",
	"trojan://pw%23x@t2.example.com:443?type=grpc&serviceName=svc&mode=gun&fp=firefox&alpn=h2#trojan gRPC",
	"hy2://a%26b@h3.example.com:443?obfs=salamander&obfs-password=a%26b&sni=s&handshake-timeout=10&ports=1000-2000&hop-interval=30#hy2 混淆",
	"tuic://u%3Aid:p%40ss@[2001:db8::3]:443?alpn=h3%2Ch2&allow_insecure=1&udp_over_stream=true&request_timeout=8000&cwnd=10&disable_sni=0&fast_open=false#TUIC IPv6",
	"vless://uuid@vh.example.com:80?type=tcp&headerType=http&host=a.com&security=none#VLESS http 伪装",
	"socks5://socks3.example.com:1080#无认证 socks",
	"http://u:p@h4.example.com:8080?sni=x#HTTP 认证",
	"wireguard://key@wg2.example.com:51820?reserved=a,b&mtu=x&keepalive=25&presharedkey=psk&public-key=pk#WG 非法 reserved",
	"snell://h5.example.com:1?psk=frompsk&udp-relay=true#Snell 参数 PSK",
	"anytls://pw@any2.example.com?alpn=h2&pinned-peer-cert-sha256=ab#AnyTLS 无端口",
	"ssd://" + js.Base64EncodeUTF8(`{"encryption":"aes-128-gcm","password":"ssdpw","servers":[{"server":"ssd.example.com","port":8388,"remarks":"SSD 节点","plugin":"obfs","plugin_options":"h.com"}]}`),
	"mierus://[2001:db8::4]:2999?username=u&multiplexing=false#Mieru IPv6",
	"naive+quic://u:p@nq.example.com:443#naive quic",
	"https://h6.example.com:443#纯 HTTPS 无认证",
}

// clashYAMLFixture 含锚点、合并键、多种协议与非常规字段。
const clashYAMLFixture = `
port: 7890
proxy-defaults: &defaults
  udp: true
  skip-cert-verify: true
proxies:
  - name: "🇭🇰 香港 SS"
    type: ss
    server: hk.example.com
    port: 8388
    cipher: aes-256-gcm
    password: "pass:word"
    plugin: obfs
    plugin-opts:
      mode: tls
      host: bing.com
  - {name: 日本 VMess, type: vmess, server: jp.example.com, port: 443, uuid: b831381d-6324-4d53-ad4f-8cda48b30811, alterId: 0, cipher: auto, tls: true, network: ws, ws-opts: {path: /ws, headers: {Host: jp.example.com}}}
  - name: Trojan Reality
    <<: *defaults
    type: trojan
    server: tr.example.com
    port: 443
    password: trpass
    sni: tr.example.com
    reality-opts:
      public-key: pk
      short-id: "01"
      support-x25519mlkem768: true
  - name: VLESS gRPC
    type: vless
    server: vl.example.com
    port: 443
    uuid: b831381d-6324-4d53-ad4f-8cda48b30811
    network: grpc
    tls: true
    servername: vl.example.com
    grpc-opts:
      grpc-service-name: svc
    client-fingerprint: chrome
    dialer-proxy: 前置组
  - name: Hy2
    type: hysteria2
    server: hy.example.com
    port: 443
    password: hypw
    ports: 20000-30000
    up: 100
    down: "200 Mbps"
    fast-open: false
    realm-opts:
      realm-id: abc
      stun-servers: [stun.a.com, stun.b.com]
  - name: TUIC
    type: tuic
    server: tuic.example.com
    port: 443
    uuid: u-1
    password: p-1
    alpn: [h3]
    congestion-controller: bbr
    udp-relay-mode: native
    reduce-rtt: true
    request-timeout: 8000
  - name: WireGuard
    type: wireguard
    server: 162.159.192.1
    port: 2408
    private-key: priv
    public-key: pub
    ip: 172.16.0.2
    ipv6: fd01::1
    reserved: [1, 2, 3]
    mtu: 1280
    dns: [1.1.1.1, 8.8.8.8]
  - name: Snell
    type: snell
    server: snell.example.com
    port: 6333
    psk: snellpsk
    version: 4
    obfs-opts:
      mode: http
      host: bing.com
  - name: AnyTLS
    type: anytls
    server: any.example.com
    port: 443
    password: anypw
    alpn: [h2, http/1.1]
    padding: true
  - name: Socks
    type: socks5
    server: socks.example.com
    port: 1080
    username: u
    password: p
  - name: HTTPS
    type: http
    server: http.example.com
    port: 443
    tls: true
    username: hu
    password: hp
    skip-cert-verify: false
  - name: SSR
    type: ssr
    server: ssr.example.com
    port: 8388
    cipher: aes-256-cfb
    password: ssrpw
    obfs: tls1.2_ticket_auth
    protocol: auth_aes128_md5
    obfs-param: o.com
  - name: Mieru
    type: mieru
    server: mieru.example.com
    port: 2999
    username: mu
    password: mp
    transport: TCP
    multiplexing: MULTIPLEXING_LOW
  - name: Naive
    type: naive
    server: naive.example.com
    port: 443
    username: nu
    password: np
    quic: true
  - name: Bad
    type: unknown-type
    server: bad.example.com
    port: 1
  - name: NumericServer
    type: ss
    server: 1234
    port: 1
proxy-groups:
  - name: Proxy
    type: select
    proxies: [DIRECT]
`

const singboxJSONFixture = `{
  "outbounds": [
    {"type": "selector", "tag": "proxy", "outbounds": ["a"]},
    {"type": "vless", "tag": "SB VLESS", "server": "sb.example.com", "server_port": 443, "uuid": "uuid-1", "flow": "xtls-rprx-vision",
     "tls": {"enabled": true, "server_name": "sb.example.com", "reality": {"enabled": true, "public_key": "pk", "short_id": "sid"}}},
    {"type": "vmess", "tag": "SB VMess", "server": "sb2.example.com", "server_port": 8443, "uuid": "uuid-2", "alter_id": 0,
     "transport": {"type": "ws", "path": "/p", "host": "h.example.com"}, "tls": {"enabled": true, "server_name": "h.example.com"}},
    {"type": "trojan", "tag": "SB Trojan", "server": "sb3.example.com", "server_port": 443, "password": "p w",
     "tls": {"enabled": true, "insecure": true, "alpn": ["h2", "http/1.1"]}, "transport": {"type": "grpc", "service_name": "svc"}},
    {"type": "shadowsocks", "tag": "SB SS", "server": "sb4.example.com", "server_port": 8388, "method": "2022-blake3-aes-128-gcm", "password": "key=="},
    {"type": "hysteria2", "tag": "SB Hy2", "server": "sb5.example.com", "server_port": 443, "password": "hy", "obfs": {"type": "salamander", "password": "ob"}, "tls": {"enabled": true}},
    {"type": "tuic", "tag": "SB TUIC", "server": "sb6.example.com", "server_port": 443, "uuid": "u", "password": "p", "congestion_control": "bbr", "tls": {"enabled": true, "server_name": "t"}},
    {"type": "anytls", "tag": "SB AnyTLS", "server": "sb7.example.com", "server_port": 443, "password": "a"},
    {"type": "direct", "tag": "direct"},
    {"type": "wireguard", "tag": "wg", "server": "w", "server_port": 1}
  ]
}`

const surgeFixture = `[Proxy]
Surge SS = ss, surge.example.com, 8388, encrypt-method=aes-128-gcm, password=pw, obfs=http, obfs-host=bing.com, udp-relay=true
Surge Trojan = trojan, tr.example.com, 443, password=trpw, sni=tr.example.com, skip-cert-verify=true
Surge VMess = vmess, vm.example.com, 443, username=b831381d-6324-4d53-ad4f-8cda48b30811, tls=true
Surge Snell = snell, sn.example.com, 6333, psk=snpsk, version=4, reuse=true
Surge TUIC = tuic, tu.example.com, 443, token=tok, sni=tu.example.com
Surge WG = wireguard, wg.example.com, 51820, private-key=priv, peer-public-key=pub, self-ip=10.0.0.2, client-id=1/2/3
Surge Positional = ss, pos.example.com, 1234, aes-256-gcm, positionalpw
# comment line
; another comment
shadowsocks=qx.example.com:8388, method=chacha20-ietf-poly1305, password=qxpw, obfs=over-tls, tag=QX SS
vless=qxv.example.com:443, method=none, password=b831381d-6324-4d53-ad4f-8cda48b30811, obfs=over-tls, vless-flow=xtls-rprx-vision, reality-base64-pubkey=pk, reality-hex-shortid=sid, tag=QX VLESS
trojan=qxt.example.com:443, password="quoted pw", tls-verification=false, tag="QX Trojan"
`
