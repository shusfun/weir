"""Assign a template to one existing acceptance account and run its real client.

Only fresh template/profile records are created. All generated credentials stay
in the restricted operator directory and are removed after each probe.
"""
import copy
import http.client
import json
from pathlib import Path
import socket
import subprocess
import time
import urllib.parse
import urllib.request
from live_api import API, OPS, state

sessions={}
def config_for(user):
    api=sessions.get(user['email'])
    if api is None:
        api=API().acceptance_user(user);sessions[user['email']]=api
    info=api.request('GET','/api/v1/subscription')
    return json.loads(api.request('GET',urllib.parse.urlsplit(info['url']).path+'?target=singbox',raw=True))

def probe(config,expected):
    config=copy.deepcopy(config)
    config['inbounds']=[{'type':'http','listen':'127.0.0.1','listen_port':20995}]
    config['log']={'level':'info'}
    path=OPS/'template-client.json';path.write_text(json.dumps(config))
    binary='dist/cores/sing-box-1.14.3-stats/sing-box.exe'
    diagnostics=(OPS/'template-client-diagnostics.log').open('wb')
    process=subprocess.Popen([binary,'run','-c',str(path)],stdout=subprocess.DEVNULL,stderr=diagnostics)
    try:
        for _ in range(30):
            try:
                with socket.create_connection(('127.0.0.1',20995),timeout=.2):break
            except OSError:time.sleep(.1)
        if process.poll() is not None:raise RuntimeError('Template client exited before listening; restricted diagnostics recorded')
        opener=urllib.request.build_opener(urllib.request.ProxyHandler({'https':'http://127.0.0.1:20995'}))
        ok=False
        try:
            body=opener.open('https://www.cloudflare.com/cdn-cgi/trace',timeout=12).read().decode()
            ok='ip=154.9.26.146' in body
        except Exception as error:
            if expected:raise RuntimeError('real template client failed ('+type(error).__name__+', '+str(getattr(error,'reason','unknown'))+'); restricted diagnostics recorded') from None
        if ok!=expected:raise RuntimeError('template route policy was not enforced')
    finally:
        process.terminate();process.wait(timeout=10);diagnostics.close();path.unlink(missing_ok=True)

def main():
    api=API().admin();users=state()['test_users'];created=[]
    version=api.request('GET','/api/v1/admin/overview')['version']
    records=api.request('GET','/api/v1/admin/users')['items']
    user_id=next(u['id'] for u in records if u['email']==users[0]['email'])
    before=config_for(users[0]);peer_before=config_for(users[1])
    template={'name':'acceptance-template-'+str(int(time.time())),'target':'singbox',
              'groups':[{'name':'AcceptanceChoice','type':'select','members':['$nodes']}],
              'rules':[{'type':'DOMAIN','value':'example.invalid','policy':'REJECT'},{'type':'FINAL','policy':'AcceptanceChoice'}],
              'dns':['1.1.1.1']}
    try:
        saved=api.request('POST','/api/v1/admin/resources/templates',{'data':template});created.append(('templates',saved['id']))
        profile=api.request('POST','/api/v1/admin/resources/profiles',{'data':{'name':'acceptance-template-profile','template_ids':[saved['id']],'user_ids':[user_id]}})
        created.append(('profiles',profile['id']))
        cfg=config_for(users[0])
        assert cfg['route']['final']=='AcceptanceChoice' and cfg['dns']['servers'][0]['server']=='1.1.1.1'
        actual=[p for p in cfg['outbounds'] if 'server' in p]
        original=[p for p in before['outbounds'] if 'server' in p]
        assert len(actual)==len(original) and {(p.get('uuid'),p.get('password')) for p in actual}=={(p.get('uuid'),p.get('password')) for p in original}
        assert config_for(users[1])==peer_before
        probe(cfg,True)
        template['rules'][0]['value']='www.cloudflare.com'
        api.request('POST','/api/v1/admin/resources/templates',{'id':saved['id'],'data':template})
        probe(config_for(users[0]),False)
        checks=['assigned output template active','authorized credentials preserved','peer subscription unchanged','real sing-box client egress through Gate','real domain reject rule enforced']
    finally:
        for kind,record_id in reversed(created):api.request('DELETE',f'/api/v1/admin/resources/{kind}/{record_id}')
        assert config_for(users[0])==before
    Path('docs/evidence/live-templates.json').write_text(json.dumps({'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'version':version,'checks':checks,'cleanup':'fresh profile/template deleted; original subscription restored'},indent=2),encoding='utf-8')
    print('Real assigned template client, route rejection and user isolation passed; '+str(version))

if __name__=='__main__':main()
