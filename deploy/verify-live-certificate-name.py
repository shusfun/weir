"""Generate private import fixture only in restricted operator storage."""
import datetime,json,subprocess
from cryptography import x509
from cryptography.x509.oid import NameOID
from cryptography.hazmat.primitives import hashes,serialization
from cryptography.hazmat.primitives.asymmetric import ec
from live_api import OPS

key=ec.generate_private_key(ec.SECP256R1());now=datetime.datetime.now(datetime.timezone.utc)
name=x509.Name([x509.NameAttribute(NameOID.COMMON_NAME,'certificate-name.invalid')])
cert=x509.CertificateBuilder().subject_name(name).issuer_name(name).public_key(key.public_key()).serial_number(x509.random_serial_number()).not_valid_before(now-datetime.timedelta(minutes=5)).not_valid_after(now+datetime.timedelta(days=2)).sign(key,hashes.SHA256())
fixture=OPS/'certificate-name-fixture.json'
try:
 fixture.write_text(json.dumps({'certificate':cert.public_bytes(serialization.Encoding.PEM).decode(),'key':key.private_bytes(serialization.Encoding.PEM,serialization.PrivateFormat.PKCS8,serialization.NoEncryption()).decode()}),encoding='utf-8')
 result=subprocess.run(['node','deploy/browser-certificate-name.cjs'],capture_output=True,text=True)
 if result.returncode:
  for line in result.stderr.splitlines():
   if line.startswith('Certificate name workflow failed at stage: '):print(line)
  raise RuntimeError('Actual certificate name browser workflow failed; private inputs omitted from diagnostics')
 print('Actual certificate name UI import/update/history/selector passed; restricted private fixture removed')
finally:fixture.unlink(missing_ok=True)
