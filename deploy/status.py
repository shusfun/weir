"""Generate the sole current-runtime/evidence index; never infer acceptance from mtime."""
import html
import json
from pathlib import Path
import re
import time

ROOT = Path(__file__).resolve().parent.parent
EVIDENCE = ROOT / 'docs/evidence'


def generate():
    deployment = json.loads((EVIDENCE / 'deployment.json').read_text(encoding='utf-8'))
    servers = {}
    for role, item in deployment.get('servers', {}).items():
        current = item.get('current', {})
        version = Path(current.get('output', '')).name if current.get('exit_code') == 0 else None
        manifest = item.get('manifest', {})
        try:
            installed = json.loads(manifest.get('output', '{}'))
        except ValueError:
            installed = {}
        servers[role] = {'version': version, 'service': item.get('service', {}).get('output', 'unknown'),
                         'manifest_matches': bool(version and installed.get('current') == version),
                         'sha256': item.get('binary_sha256', {}).get('output', '').split(' ')[0] or None,
                         'backups': item.get('latest_backup_snapshots', {}).get('output', '').splitlines()}
    versions = {x['version'] for x in servers.values() if x['version']}
    unified = next(iter(versions)) if (set(servers) == {'control', 'gate'} and
                                      all(x['version'] for x in servers.values()) and len(versions) == 1) else None
    records = []
    for path in sorted([*EVIDENCE.glob('*.json'), *(EVIDENCE / 'production-operations').glob('*.json')]):
        if path.name in ['deployment.json', 'current-state.json'] or path.stat().st_size > 2_000_000:
            continue
        try:
            data = json.loads(path.read_text(encoding='utf-8-sig'))
        except (ValueError, OSError):
            continue
        if not isinstance(data, dict):
            continue
        version = data.get('version') or data.get('application_version')
        if not isinstance(version, str) or not re.fullmatch(r'[A-Za-z0-9._-]{1,80}', version):
            version = None
        date = data.get('date') or data.get('finished_at') or data.get('timestamp')
        if not isinstance(date, str) or not re.match(r'^\d{4}-\d{2}-\d{2}', date):
            date = None
        # Human judgment stays in the original evidence; no "checks exist => passed".
        result = data.get('result') if data.get('result') in ['passed', 'failed', 'blocked'] else 'recorded'
        records.append({'file': path.relative_to(EVIDENCE).as_posix(), 'version': version, 'accepted_at': date, 'result': result,
                        'relation': 'current-service-version' if version in versions else 'historical' if version else 'unversioned'})
    report = {'generated_at': time.strftime('%Y-%m-%dT%H:%M:%SZ', time.gmtime()),
              'observed_at': deployment.get('date'), 'servers': servers, 'unified_version': unified,
              'records': records, 'note': 'Runtime observation does not imply feature acceptance. Missing version/time stays unknown.'}
    (EVIDENCE / 'current-state.json').write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
    esc = lambda v: html.escape(str(v if v is not None else '未知'))
    server_rows = ''.join('<tr>' + ''.join('<td>' + esc(v) + '</td>' for v in [role, entry['version'], entry['service'], '一致' if entry['manifest_matches'] else '未知 / 不一致', ' · '.join(entry['backups']) or None]) + '</tr>' for role, entry in servers.items())
    record_rows = ''.join('<tr><td><a href="evidence/' + esc(r['file']) + '">' + esc(r['file']) + '</a></td>' + ''.join('<td>' + esc(v) + '</td>' for v in [r['version'], r['accepted_at'], r['relation'], r['result']]) + '</tr>' for r in records)
    doc = '''<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>MiGate 当前取证</title><style>body{font:14px/1.7 system-ui;background:#f5f6f8;color:#182334;margin:0}main{max-width:1200px;margin:auto;padding:24px}a{color:#315cdd}.table{overflow:auto}table{border-collapse:collapse;background:white;width:100%}td:first-child,th:first-child{min-width:90px}td:nth-child(2),th:nth-child(2){min-width:145px}td:nth-child(3),th:nth-child(3){min-width:105px}td:nth-child(4),th:nth-child(4){min-width:110px}td,th{padding:10px;text-align:left;border:1px solid #dce3ed;vertical-align:top;overflow-wrap:anywhere}input{padding:10px;width:min(100%,420px)}.note{padding:12px;background:#fff8e9}@media(max-width:600px){main{padding:16px}table{min-width:740px}}</style><main><a href="implementation.html">实施证据</a> · <a href="batches.html">有限批次 / 剩余范围</a><h1>当前运行与证据索引</h1>'''
    doc += '<p>服务器取证：' + esc(report['observed_at']) + '；本页生成：' + esc(report['generated_at']) + '</p><p class="note">此页自动生成。在线版本与历史验收分别呈现；缺少版本 / 验收时间保持未知，recorded 不代表通过。观察超过 24 小时需重新取证。</p>'
    doc += '<div class="table"><table><tr><th>服务</th><th>实际版本</th><th>状态</th><th>清单</th><th>最近备份</th></tr>' + server_rows + '</table></div><h2>证据版本与时间</h2><label>筛选文件 / 版本 <input id="filter" type="search"></label><div class="table"><table id="evidence"><thead><tr><th>证据</th><th>版本</th><th>记录内时间</th><th>当前 / 历史</th><th>显式结果</th></tr></thead><tbody>' + record_rows + '</tbody></table></div><script>document.getElementById("filter").addEventListener("input",e=>{for(const row of document.querySelectorAll("#evidence tbody tr"))row.hidden=!row.textContent.toLowerCase().includes(e.target.value.toLowerCase())})</script></main></html>'
    (ROOT / 'docs/status.html').write_text(doc, encoding='utf-8')
    return report


if __name__ == '__main__':
    generate()
    print('Current runtime/evidence index generated; missing metadata remains unknown')
