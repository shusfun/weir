"""Create the explicitly labeled disposable systemd Agent, without host mounts."""
import json,subprocess,time
name='migate-quota-acceptance'
def run(*args,check=True):
 r=subprocess.run(args,capture_output=True,text=True,encoding='utf-8',errors='replace')
 if check and r.returncode:raise RuntimeError('Isolated fixture preparation failed: '+r.stderr[-700:])
 return r
if run('docker','container','inspect',name,check=False).returncode==0:raise RuntimeError('Named fixture exists; inspect before any replacement')
# Reuse only the documented dedicated stack/network; never reset active checks.
if run('docker','ps','-q','--filter','label=com.docker.compose.project=migate-local-test').stdout.strip():raise RuntimeError('Dedicated stack already in use')
run('docker','compose','-f','deploy/test.compose.yml','up','-d','--wait')
run('docker','run','-d','--name',name,'--label','migate.acceptance=quota','--network','migate-local-test_default','--privileged','--cgroupns=private','--tmpfs','/run','--tmpfs','/run/lock','--tmpfs','/tmp','-p','127.0.0.1:24446:24446','migate-systemd-test:bookworm')
for _ in range(30):
 if run('docker','exec',name,'systemctl','is-system-running',check=False).returncode==0:break
 time.sleep(1)
else:raise RuntimeError('Disposable systemd did not become ready')
run('docker','exec',name,'sh','-eu','-c','command -v ssh-keygen >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq --no-install-recommends openssh-client); command -v python3 >/dev/null; command -v ssh >/dev/null')
labels=json.loads(run('docker','inspect',name,'--format','{{json .Config.Labels}}').stdout)
assert labels['migate.acceptance']=='quota'
print('Isolated labeled systemd Agent ready on loopback 24446; no host mounts or production service changes')
