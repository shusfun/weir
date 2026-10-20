"""Verify deployed conversion APIs with synthetic credentials, never user links."""
import json,base64
from urllib.parse import quote
from pathlib import Path
import time
from live_api import API

api = API().admin()
version = api.request('GET', '/api/v1/admin/overview')['version']
checks = []
samples = [
    ('http', 'http://us%40er:pa%3Ass@example.com:8080', 'http'),
    ('https', 'https://us%40er:pa%3Ass@example.com:443?sni=proxy.example', 'http'),
    ('socks5', 'socks5://us%40er:pa%3Ass@example.com:1080', 'socks'),
    ('anytls', 'anytls://pa%3Ass@example.com:443?sni=proxy.example', 'anytls'),
]
for name, link, kind in samples:
    result = api.request('POST', '/api/v1/admin/subscriptions/preview',
                         {'links': [link], 'operators': [], 'target': 'singbox'})
    cfg = json.loads(result['output'])
    proxy = cfg['outbounds'][1]
    assert proxy['type'] == kind and proxy['password'] == 'pa:ss'
    assert ('tls' in proxy) == (name in ['https', 'anytls'])
    checks.append({'protocol': name, 'credentials_preserved': True, 'tls_preserved': True})
for link in ['socks5-tls://u:p@example.com:1080',
             'vless://id@example.com:443?security=tls&type=grpc&flow=xtls-rprx-vision',
             'http://example.com:80?unknown=discarded']:
    api.request('POST', '/api/v1/admin/subscriptions/preview',
                {'links': [link], 'operators': [], 'target': 'singbox'}, expected=(422,))
checks.append({'unsupported_or_incompatible_fields_rejected': True})
for query in ['sni=one&sni=two', 'sni=one&peer=two', 'allowInsecure=maybe', 'zero_rtt_handshake=2']:
    api.request('POST', '/api/v1/admin/subscriptions/preview',
                {'links': ['trojan://synthetic@example.com:443?' + query], 'operators': [], 'target': 'singbox'}, expected=(422,))
result = api.request('POST', '/api/v1/admin/subscriptions/preview', {
    'links': ['hysteria2://synthetic@example.com:443?obfs=salamander&obfs-password=%20a%2Bb%252Fc%20'],
    'operators': [], 'target': 'singbox'})
assert json.loads(result['output'])['outbounds'][1]['obfs']['password'] == ' a+b%2Fc '
result = api.request('POST', '/api/v1/admin/subscriptions/preview', {
    'links': ['vless://synthetic@example.com:443?type=ws&path=%2Fedge%3Ftoken%3Da%2Bb%252Fc%20'],
    'operators': [], 'target': 'singbox'})
assert json.loads(result['output'])['outbounds'][1]['transport']['path'] == '/edge?token=a+b%2Fc '
checks.append({'ambiguous_queries_rejected': True, 'query_values_decoded_once': True,
               'credential_and_transport_whitespace_preserved': True})
for method,size in [('2022-blake3-aes-128-gcm',16),('2022-blake3-aes-256-gcm',32),('2022-blake3-chacha20-poly1305',32)]:
    key=base64.b64encode(b'x'*size).decode()
    secrets=[key] if 'chacha' in method else [key,key+':'+base64.b64encode(b'y'*size).decode()]
    for secret in secrets:
        link='ss://'+base64.urlsafe_b64encode((method+':'+secret).encode()).decode().rstrip('=')+'@example.com:8388'
        result=api.request('POST','/api/v1/admin/subscriptions/preview',{'links':[link],'operators':[],'target':'singbox'})
        proxy=json.loads(result['output'])['outbounds'][1]
        assert proxy['method']==method and proxy['password']==secret
    for secret in ['not-base64',key+':']:
        link='ss://'+method+':'+quote(secret,safe='')+'@example.com:8388'
        api.request('POST','/api/v1/admin/subscriptions/preview',{'links':[link],'operators':[],'target':'singbox'},expected=(422,))
checks.append({'ss2022_three_methods_and_aes_identity_chain_preserved':True,'invalid_ss2022_keys_rejected':True,
               'real_core_transport':'local evidence ss2022-core-tests.log'})
for target in ['surge', 'loon', 'quanx']:
    result=api.request('POST','/api/v1/admin/subscriptions/preview',
                       {'links':['trojan://synthetic@example.com:443?sni=example.org#HK'], 'operators':[], 'target':target})
    assert 'example.com' in result['output'] and 'example.org' in result['output']
    assert '[server_local]' in result['output'] if target=='quanx' else '[Proxy]' in result['output']
    checks.append({'ini_target':target,'deployed_preview':True,'real_commercial_client':'not validated'})
Path('docs/evidence/live-conversions.json').write_text(json.dumps({
    'date': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'version': version,
    'scope': 'Deployed HTTPS API using synthetic nodes; real local core transport tests recorded separately',
    'checks': checks,
}, indent=2), encoding='utf-8')
print('Deployed conversion API acceptance passed; ' + str(version))
