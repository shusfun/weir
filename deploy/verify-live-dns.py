"""Actual assigned subscription -> pinned sing-box -> encrypted DNS resolution.

Only creates scoped template/profile resources. Credentials remain in restricted
operator storage; query responses and config contents are never logged.
"""
import copy,json,secrets,socket,struct,subprocess,time,urllib.parse
from pathlib import Path
from live_api import API,OPS,state

def config_for(user):
 a=API();a.login(user['email'],user['password'])
 info=a.request('GET','/api/v1/subscription')
 return json.loads(a.request('GET',urllib.parse.urlsplit(info['url']).path+'?target=singbox',raw=True))

def query_dns(config):
 cfg=copy.deepcopy(config)
 cfg['inbounds']=[{'type':'direct','tag':'dns-acceptance','listen':'127.0.0.1','listen_port':20996,'network':'udp'}]
 cfg['route']['rules'].insert(0,{'inbound':['dns-acceptance'],'action':'hijack-dns'})
 cfg['log']={'level':'fatal'}
 file=OPS/'encrypted-dns-client.json';file.write_text(json.dumps(cfg))
 process=subprocess.Popen(['dist/cores/sing-box-1.14.3-stats/sing-box.exe','run','-c',str(file)],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)
 try:
  transaction=secrets.token_bytes(2)
  name=b''.join(bytes([len(label)])+label.encode() for label in 'www.cloudflare.com'.split('.'))+b'\0'
  packet=transaction+struct.pack('!HHHHH',0x100,1,0,0,0)+name+struct.pack('!HH',1,1)
  time.sleep(.5)
  with socket.socket(socket.AF_INET,socket.SOCK_DGRAM) as sock:
   sock.settimeout(12);sock.sendto(packet,('127.0.0.1',20996));response,_=sock.recvfrom(4096)
  if response[:2]!=transaction or len(response)<12:raise RuntimeError('Invalid real DNS response')
  flags,_,answers,_,_=struct.unpack('!HHHHH',response[2:12])
  if flags & 0xF or answers==0:raise RuntimeError('Encrypted DNS did not resolve a real domain')
  return {'rcode':flags & 0xF,'answer_count':answers}
 finally:
  process.terminate();process.wait(timeout=10);file.unlink(missing_ok=True)

if __name__=='__main__':
 api=API().admin();users=state()['test_users'];created=[];results=[]
 version=api.request('GET','/api/v1/admin/overview')['version']
 uid=next(u['id'] for u in api.request('GET','/api/v1/admin/users')['items'] if u['email']==users[0]['email'])
 before=config_for(users[0]);peer=config_for(users[1])
 template={'name':'encrypted-dns-acceptance-'+str(int(time.time())),'target':'singbox','groups':[{'name':'DNSAcceptance','type':'select','members':['$nodes']}],'rules':[{'type':'FINAL','policy':'DNSAcceptance'}],'dns':['https://1.1.1.1/dns-query']}
 try:
  saved=api.request('POST','/api/v1/admin/resources/templates',{'data':template});created.append(('templates',saved['id']))
  profile=api.request('POST','/api/v1/admin/resources/profiles',{'data':{'name':'encrypted-dns-acceptance','template_ids':[saved['id']],'user_ids':[uid]}});created.append(('profiles',profile['id']))
  for transport,address,bootstrap in [('DoH-IP','https://1.1.1.1/dns-query',[]),('DoH-host','https://cloudflare-dns.com/dns-query',['1.1.1.1']),('DoT-IP','tls://1.1.1.1',[])]:
   template['dns']=[address];template['dns_bootstrap']=bootstrap
   api.request('POST','/api/v1/admin/resources/templates',{'id':saved['id'],'data':template})
   cfg=config_for(users[0])
   if cfg['dns']['servers'][0]['type']!=('tls' if transport=='DoT-IP' else 'https'):raise RuntimeError('DNS transport lost in subscription')
   answer=query_dns(cfg)
   if config_for(users[1])!=peer:raise RuntimeError('Other user subscription changed')
   results.append({'transport':transport,'passed':True,**answer})
   print(transport+' actual DNS resolution passed',flush=True)
 finally:
  for kind,id in reversed(created):api.request('DELETE',f'/api/v1/admin/resources/{kind}/{id}')
  if config_for(users[0])!=before:raise RuntimeError('Original subscription not restored')
  Path('docs/evidence/live-encrypted-dns.json').write_text(json.dumps({'version':version,'date':time.strftime('%Y-%m-%dT%H:%M:%SZ',time.gmtime()),'results':results,'cleanup':'fresh template/profile removed; original subscription restored'},indent=2))
