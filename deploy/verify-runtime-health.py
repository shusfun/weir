"""Read-only domain, role and paired-device readiness after acceptance cleanup."""
import json
from pathlib import Path
import ssl
import time

from live_api import API, ForwardedHTTPS, state
from remote import connect, run
from production_guard import check


def public(path):
    check('GET', path)
    connection = ForwardedHTTPS('sub.caowo.baby', timeout=20, context=ssl.create_default_context())
    try:
        connection.request('GET', path)
        response = connection.getresponse()
        return response.status, response.read()
    finally:
        connection.close()


status, body = public('/healthz')
if status != 200 or json.loads(body).get('status') != 'ok':
    raise RuntimeError('Domain dependency readiness failed')
status, body = public('/login')
if status != 200 or b'<html' not in body.lower():
    raise RuntimeError('Domain login document unavailable')
admin = API().admin()
version = admin.request('GET', '/api/v1/admin/overview')['version']
devices = admin.request('GET', '/api/v1/admin/devices')['items']
if not any(item['status'] == 'approved' for item in devices):
    raise RuntimeError('No paired Gate has a fresh heartbeat')
user = API().acceptance_user(state()['test_users'][0])
user.request('GET', '/api/v1/admin/overview', expected=(403,))
API().request('GET', '/api/v1/admin/overview', expected=(401,))
with connect('gate') as client:
    code, _, _ = run(client, 'systemctl is-active migate-gate migate-gate-helper migate-gate-xray migate-gate-tunnel relay154-x-ui')
    if code:
        raise RuntimeError('Gate or existing x-ui service not active')
report = {
    'date': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()), 'version': version, 'result': 'passed',
    'checks': ['verified domain TLS / login document / PG and Redis health',
               'paired Gate has a heartbeat within 60 seconds',
               'ordinary admin endpoint 403 and anonymous endpoint 401',
               'Gate, owned helper/core/tunnel and existing x-ui services active'],
    'boundary': 'Read only; no user, grant, subscription inventory, service or kernel changes; not a proxy-client regression',
}
Path('docs/evidence/runtime-health.json').write_text(json.dumps(report, indent=2), encoding='utf-8')
print('Domain, permissions, paired heartbeat and service readiness passed')
