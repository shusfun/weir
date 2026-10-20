"""Cooperative operator lease. All real SSH/API/browser work shares one runner."""
import json
import os
import sys
import urllib.request


def check(method=None, path=None, data=None):
    address = os.environ.get('MIGATE_LEASE_ADDRESS', '')
    token = os.environ.get('MIGATE_LEASE_TOKEN', '')
    if not address.startswith('http://127.0.0.1:') or not token:
        raise RuntimeError('Production operation requires deploy/production-run.py (shared lock + cleanup scope)')
    body = None if method is None else json.dumps({'method': method, 'path': path, 'data': data}).encode()
    req = urllib.request.Request(address + ('/check' if body is None else '/authorize'), data=body,
                                 headers={'Authorization': 'Bearer ' + token, 'Content-Type': 'application/json'})
    # Never send a lease through an operator HTTP proxy.
    with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req, timeout=5) as response:
        if json.load(response).get('active') is not True:
            raise RuntimeError('Production lease lost')


if __name__ == '__main__':
    try:
        envelope = json.load(sys.stdin) if '--request' in sys.argv else None
        check(**envelope) if envelope else check()
    except Exception:
        print('Production lease unavailable; use production-run.py', file=sys.stderr)
        sys.exit(1)
