"""User-requested default uninstall: retain data and managed cores, manifest only."""
import argparse, json, shlex, time
from pathlib import Path
from remote import connect, run
from production_guard import check

p=argparse.ArgumentParser()
p.add_argument('--execute', action='store_true')
a=p.parse_args()
report={'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'scope':'default uninstall; retain configuration, data and managed cores','servers':{}}
def command(c, cmd, timeout=120):
    check()
    code,out,err=run(c,cmd,timeout=timeout)
    if code: raise RuntimeError('Uninstall operation failed: '+out+err)
    return out

try:
    for role in ['gate','control']:
        with connect(role) as c:
            manifest=f'/etc/migate-{role}/install-manifest.json'
            manager=f'/opt/migate-{role}/migate-manage'
            m=json.loads(command(c,'cat '+manifest))
            assert m['role']==role
            plan=json.loads(command(c,f'{manager} uninstall --manifest {manifest} --dry-run'))
            owned={r['path']:r['kind'] for r in m['resources']}
            for item in plan:
                assert item['operation'] in ['revoke-device','stop-service','remove']
                if item['operation']=='remove':
                    assert owned.get(item['target']) in ['program','service','transport-key']
            entry={'version':m['current'],'plan':plan}
            report['servers'][role]=entry
            print(role+' preview: '+json.dumps(plan),flush=True)
            if not a.execute: continue
            # Only services unrelated to this application are compared, without config/secret output.
            baseline=command(c,"systemctl list-units --type=service --state=running --no-legend --no-pager | awk '{print $1}'")
            protected=[u for u in baseline.splitlines() if not u.startswith('migate-')]
            core_units=[Path(r['path']).name for r in m['resources'] if r['kind']=='core-service']
            core_states={u:run(c,'systemctl is-active '+shlex.quote(u))[1].strip() for u in core_units}
            backup=f'{manager} backup --manifest {manifest} --backup-dir /var/backups/migate --confirm {role}:backup'
            if role=='control': backup+=' --pg-dump /etc/migate-control/pg-dump'
            command(c,backup,300)
            entry['backup_completed']=True
            entry['latest_backup']=command(c,f"find /var/backups/migate -maxdepth 1 -type f -name 'migate-{role}-*.snapshot.json' | sort | tail -n 1").strip()
            result=command(c,f'{manager} uninstall --manifest {manifest} --confirm {role}:uninstall',180)
            entry['revocation_warning']='revocation incomplete' in result or 'revocation not verified' in result
            for r in m['resources']:
                if r['kind'] in ['service','program','transport-key']:
                    command(c,'test ! -e '+shlex.quote(r['path'])+' && test ! -L '+shlex.quote(r['path']))
                elif r['kind'] in ['config','data']:
                    command(c,'test -d '+shlex.quote(r['path']))
                if r['kind']=='service':
                    state=run(c,'systemctl is-active '+shlex.quote(Path(r['path']).name))[1].strip()
                    assert state not in ['active','activating','reloading']
            for u in protected: command(c,'systemctl is-active --quiet '+shlex.quote(u))
            for u,state in core_states.items():
                assert run(c,'systemctl is-active '+shlex.quote(u))[1].strip()==state, 'Managed core state changed'
            entry.update(result='uninstalled',protected_services_verified=protected,retained_core_states=core_states)
            print(role+': application removed; data, cores and unrelated services verified',flush=True)
    report['result']='passed' if a.execute else 'previewed'
finally:
    Path('docs/evidence/user-uninstall'+('' if a.execute else '-preview')+'.json').write_text(json.dumps(report,indent=2),encoding='utf-8')
