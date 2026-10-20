"""Operator transport. Credentials stay in the operator's restricted directory."""
import argparse
import json
import os
from pathlib import Path
import sys
import paramiko

def credentials():
    default = Path(os.environ.get('LOCALAPPDATA', str(Path.home()))) / 'MiGate/operations/credentials.json'
    return json.loads(Path(os.environ.get('MIGATE_OPERATIONS_FILE', default)).read_text(encoding='utf-8'))

def connect(target):
    from production_guard import check
    check()
    return _connect(target)

def _connect(target):
    config = credentials()[target]
    client = paramiko.SSHClient()
    client.load_host_keys(str(Path.home() / '.ssh/known_hosts'))
    client.set_missing_host_key_policy(paramiko.RejectPolicy())
    client.connect(config['host'], port=22, username='root', key_filename=config.get('key'),
                   password=config.get('password'), look_for_keys=False, allow_agent=False,
                   timeout=15, banner_timeout=15, auth_timeout=15)
    client.get_transport().set_keepalive(15)
    return client

def run(client, command, stdin=None, timeout=120):
    inp, out, err = client.exec_command(command, timeout=timeout)
    if stdin is not None:
        inp.write(stdin)
        inp.flush()
        inp.channel.shutdown_write()
    output = out.read().decode('utf-8', errors='replace')
    error = err.read().decode('utf-8', errors='replace')
    status = out.channel.recv_exit_status()
    return status, output, error

if __name__ == '__main__':
    parser = argparse.ArgumentParser()
    parser.add_argument('target', choices=['control', 'gate'])
    parser.add_argument('--command')
    parser.add_argument('--upload', nargs=2, metavar=('LOCAL', 'REMOTE'))
    parser.add_argument('--timeout', type=int, default=120)
    args = parser.parse_args()
    with connect(args.target) as client:
        if args.upload:
            with client.open_sftp() as sftp:
                sftp.put(*args.upload)
            print('Upload complete')
        if args.command:
            status, output, error = run(client, args.command, timeout=args.timeout)
            print(output, end='')
            print(error, end='', file=sys.stderr)
            sys.exit(status)
