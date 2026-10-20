"""Fail before SSH/upload when the matching local interaction gate is missing."""
import hashlib
import json
from pathlib import Path
import tarfile

ROOT = Path(__file__).resolve().parent.parent


def verify(archive):
    with tarfile.open(archive, 'r:gz') as package:
        members = [m for m in package.getmembers() if m.name.lstrip('./') == 'BUILD.json']
        if len(members) != 1 or members[0].size > 4096:
            raise RuntimeError('Release lacks a unique BUILD.json; rebuild with the local gate')
        build = json.load(package.extractfile(members[0]))
    evidence = json.loads((ROOT / 'docs/evidence/local-gate.json').read_text(encoding='utf-8'))
    review = json.loads((ROOT / 'docs/evidence/local-visual-review.json').read_text(encoding='utf-8'))
    if evidence.get('result') != 'passed' or evidence.get('source_fingerprint') != build.get('source_fingerprint'):
        raise RuntimeError('Local full-workflow evidence does not match release source')
    definitions = json.loads((ROOT / 'deploy/local-workflows.json').read_text(encoding='utf-8'))
    definition = definitions.get(build.get('batch'))
    if not definition or evidence.get('batch') != build.get('batch') or evidence.get('workflows') != definition['workflows']:
        raise RuntimeError('Complete local workflows for this batch are required')
    if not evidence.get('screenshots'):
        raise RuntimeError('Local interaction gate requires captured screenshots')
    evidence_hash = hashlib.sha256((ROOT / 'docs/evidence/local-gate.json').read_bytes()).hexdigest()
    if review.get('result') != 'passed' or review.get('local_gate_sha256') != evidence_hash:
        raise RuntimeError('Matching screenshot review is required before production deployment')
    for item in evidence['screenshots']:
        path = (ROOT / item['path']).resolve()
        if not path.is_relative_to(ROOT / 'docs/evidence') or hashlib.sha256(path.read_bytes()).hexdigest() != item['sha256']:
            raise RuntimeError('Reviewed local screenshot changed')
    return build
