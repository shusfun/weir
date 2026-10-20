"""Inject a failed acceptance after creating a scoped fixture; runner must clean it."""
import os
from live_api import API

api = API().admin()
prefix = os.environ['MIGATE_FIXTURE_PREFIX']
api.request('POST', '/api/v1/admin/resources/public-page', {'data': {
    'name': prefix + 'interrupted', 'title': 'Disposable cleanup probe',
    'slug': prefix + 'probe', 'enabled': False}})
print('Owned fixture created; injecting acceptance failure to exercise runner cleanup')
raise SystemExit(23)
