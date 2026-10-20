"""Bounded read-only task projection for a failed pool acceptance."""
import json
from pathlib import Path
import time
from live_api import API,state
api=API().admin()
items=api.request('GET',f'/api/v1/admin/tasks?device_id={state()["device_id"]}&page_size=30')['items']
safe=[{k:t[k] for k in ['id','operation','status','created_at'] if k in t} for t in items]
report={'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':api.request('GET','/api/v1/admin/overview')['version'],'result':'diagnostic','tasks':safe,'boundary':'Read only; no payloads, credentials or raw results'}
Path('docs/evidence/b1-task-diagnostic.json').write_text(json.dumps(report,indent=2),encoding='utf-8')
print(json.dumps(safe,indent=2))
