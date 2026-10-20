"""Local regression for lease loss, scoped cleanup and release evidence freshness."""
import importlib.util
import json
import hashlib
import io
import os
from pathlib import Path
import tempfile
import tarfile
import unittest
from unittest.mock import patch, Mock
from contextlib import nullcontext
import threading
import urllib.error
import urllib.request

spec = importlib.util.spec_from_file_location('production_runner', Path(__file__).with_name('production-run.py'))
runner = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runner)
import production_guard
import release_gate
import status


class Lease:
    active = True
    def alive(self):
        return self.active


class ProductionTools(unittest.TestCase):
    def test_lease_renews_without_foreground_checks_and_stops_on_loss(self):
        channel = Mock(closed=False)
        channel.recv.return_value = b'MIGATE_LOCKED'
        channel.exit_status_ready.return_value = False
        renewed = threading.Event()
        channel.sendall.side_effect = lambda *_: renewed.set()
        client = Mock()
        client.get_transport.return_value.open_session.return_value = channel
        client.get_transport.return_value.is_active.return_value = True
        with patch.object(runner, '_connect', return_value=client), patch.object(runner.RemoteLease, 'HEARTBEAT_INTERVAL', 0.02):
            with runner.RemoteLease() as lease:
                renewed.clear()
                # No alive(), broker request or child monitor: synchronous phases.
                self.assertTrue(renewed.wait(1), 'Background renewal did not run')
                channel.sendall.side_effect = OSError('disconnected')
                self.assertTrue(lease.heartbeat_failed.wait(1))
                with self.assertRaises(RuntimeError):
                    lease.clear_marker()
            self.assertFalse(lease.heartbeat_thread.is_alive())

    def test_cleanup_failure_keeps_journals_blocks_next_run_and_recovers(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / 'deploy').mkdir()
            (root / 'docs/evidence').mkdir(parents=True)
            (root / 'deploy/production-commands.json').write_text(json.dumps({
                'verify-production-lock.py': {'scope': 'none', 'impacts': ['observe']}}))
            (root / 'docs/evidence/deployment.json').write_text(json.dumps({'date': '2026-10-20T00:00:00Z', 'servers': {}}))
            pending = root / 'operations/pending.json'
            lease = Mock()
            lease.__enter__ = Mock(return_value=lease)
            lease.__exit__ = Mock(return_value=False)
            remote_marker = []
            lease.marker.side_effect = lambda: remote_marker[0] if remote_marker else None
            lease.write_marker.side_effect = lambda value: remote_marker.append(value)
            lease.clear_marker.side_effect = lambda: remote_marker.clear()
            child = Mock(returncode=0)
            child.poll.return_value = 0
            args = ['production-run.py', '--label', 'test-cleanup-failure', '--', 'python', 'deploy/verify-production-lock.py']
            with patch.object(runner, 'ROOT', root), patch.object(runner, 'PENDING', pending), \
                 patch.object(runner, 'RemoteLease', return_value=lease), patch.object(runner, 'Broker', return_value=nullcontext()), \
                 patch.object(runner, 'snapshot', return_value={}), patch.object(runner, 'cleanup', side_effect=RuntimeError('cleanup failure')) as cleanup, \
                 patch.object(runner.subprocess, 'Popen', return_value=child), \
                 patch.object(runner.subprocess, 'run', return_value=Mock(returncode=0)), patch.object(status, 'generate'), \
                 patch('sys.argv', args):
                with self.assertRaisesRegex(RuntimeError, 'cleanup failure'):
                    runner.main()
                self.assertTrue(pending.exists())
                self.assertTrue(remote_marker)
                report = json.loads((root / 'docs/evidence/production-operation.json').read_text())
                self.assertFalse(report['cleanup_verified'])
                self.assertEqual(report['result'], 'failed')
                with self.assertRaisesRegex(RuntimeError, 'Previous production cleanup incomplete'):
                    runner.main()
                self.assertEqual(cleanup.call_count, 1)
                cleanup.side_effect = None
                with patch('sys.argv', ['production-run.py', '--label', 'recover-test', '--recover']):
                    self.assertEqual(runner.main(), 0)
                self.assertFalse(pending.exists())
                self.assertFalse(remote_marker)
                recovered = json.loads((root / 'docs/evidence/production-operation.json').read_text())
                self.assertEqual(recovered['recovery_of'], report['run_id'])
                self.assertEqual(recovered['result'], 'passed')
                self.assertTrue(recovered['cleanup_verified'])
                original = json.loads((root / ('docs/evidence/production-operations/' + report['run_id'] + '.json')).read_text())
                self.assertEqual(original['result'], 'failed')
                self.assertIn('observed_at', recovered)

    def test_unknown_version_cannot_be_unified_and_missing_evidence_stays_unknown(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            evidence = root / 'docs/evidence'
            evidence.mkdir(parents=True)
            observed = {'date': '2026-10-20T00:00:00Z', 'servers': {
                'control': {'current': {'exit_code': 0, 'output': '/releases/rc112'}},
                'gate': {'current': {'exit_code': 1, 'output': ''}}}}
            deployment = evidence / 'deployment.json'
            deployment.write_text(json.dumps(observed))
            (evidence / 'no-version.json').write_text(json.dumps({'checks': ['not a result']}))
            with patch.object(status, 'ROOT', root), patch.object(status, 'EVIDENCE', evidence):
                result = status.generate()
                self.assertIsNone(result['unified_version'])
                item = next(x for x in result['records'] if x['file'] == 'no-version.json')
                self.assertIsNone(item['version'])
                self.assertIsNone(item['accepted_at'])
                self.assertEqual(item['result'], 'recorded')
                observed['servers']['gate']['current'] = {'exit_code': 0, 'output': '/releases/rc112'}
                deployment.write_text(json.dumps(observed))
                self.assertEqual(status.generate()['unified_version'], 'rc112')

    def test_release_refuses_missing_stale_or_unreviewed_local_workflow(self):
        with tempfile.TemporaryDirectory() as folder:
            root = Path(folder)
            (root / 'docs/evidence').mkdir(parents=True)
            (root / 'deploy').mkdir()
            package = root / 'release.tar.gz'
            with tarfile.open(package, 'w:gz') as archive:
                body = json.dumps({'source_fingerprint': 'current', 'batch': 'B0'}).encode()
                info = tarfile.TarInfo('./BUILD.json')
                info.size = len(body)
                archive.addfile(info, io.BytesIO(body))
            evidence = root / 'docs/evidence/local-gate.json'
            visual = root / 'docs/evidence/local-visual-review.json'
            (root / 'deploy/local-workflows.json').write_text(json.dumps({'B0': {'workflows': ['complete-flow']}}))
            evidence.write_text(json.dumps({'result': 'passed', 'source_fingerprint': 'stale'}))
            visual.write_text('{}')
            with patch.object(release_gate, 'ROOT', root):
                with self.assertRaises(RuntimeError):
                    release_gate.verify(package)
                evidence.write_text(json.dumps({'result': 'passed', 'source_fingerprint': 'current', 'batch': 'B0',
                                                'workflows': ['complete-flow'], 'screenshots': [{'path':'docs/evidence/screen.png','sha256':hashlib.sha256(b'test screenshot').hexdigest()}]}))
                (root / 'docs/evidence/screen.png').write_bytes(b'test screenshot')
                with self.assertRaises(RuntimeError):
                    release_gate.verify(package)
                visual.write_text(json.dumps({'result': 'passed', 'local_gate_sha256': hashlib.sha256(evidence.read_bytes()).hexdigest()}))
                self.assertEqual(release_gate.verify(package)['batch'], 'B0')
                (root / 'docs/evidence/screen.png').write_bytes(b'changed after review')
                with self.assertRaisesRegex(RuntimeError, 'screenshot changed'):
                    release_gate.verify(package)

    def test_broker_refuses_missing_wrong_and_expired_lease(self):
        lease = Lease()
        with runner.Broker(lease):
            production_guard.check()
            with patch.dict(os.environ, {'MIGATE_LEASE_TOKEN': 'invalid'}):
                with self.assertRaises(urllib.error.HTTPError):
                    production_guard.check()
            lease.active = False
            with self.assertRaises(urllib.error.HTTPError):
                production_guard.check()
        with self.assertRaises(Exception):
            production_guard.check()

    def test_cleanup_only_owns_prefixed_new_resources_and_verifies_baseline(self):
        baseline = {'id': 1, 'data': {'name': 'existing'}}
        owned = {'id': 2, 'data': {'name': 'run-fixture'}}
        items = [baseline, owned]
        deleted = []
        class API:
            def admin(self):
                return self
            def request(self, method, path, **kwargs):
                deleted.append(path)
                items[:] = [x for x in items if x['id'] != 2]
        record = {'fixture_prefix': 'run-', 'cleanup': {'plan': {'resource_kinds': ['public-page']},
                                                       'baseline': {'public-page': [baseline]}}}
        with patch('live_api.API', API), patch.object(runner, 'inventory', side_effect=lambda *_: list(items)):
            runner.cleanup(record)
        self.assertEqual(deleted, ['/api/v1/admin/resources/public-page/2'])
        items[:] = [baseline, {'id': 3, 'data': {'name': 'somebody-else'}}]
        with patch('live_api.API', API), patch.object(runner, 'inventory', side_effect=lambda *_: list(items)):
            with self.assertRaises(RuntimeError):
                runner.cleanup(record)
        self.assertEqual(len(deleted), 1)

    def test_scope_refuses_preexisting_inventory_and_undeclared_operations(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / 'journal.json'
            path.write_text(json.dumps({'fixture_prefix': 'run-', 'cleanup': {'plan': {'resource_kinds': ['public-page']},
                              'baseline': {'public-page': [{'id': 1}]}}}), encoding='utf-8')
            with patch.object(runner, 'PENDING', path):
                runner.authorize('POST', '/api/v1/admin/resources/public-page', {'data': {'name': 'run-fixture'}})
                with self.assertRaises(RuntimeError):
                    runner.authorize('DELETE', '/api/v1/admin/resources/public-page/1', None)
                with self.assertRaises(RuntimeError):
                    runner.authorize('POST', '/api/v1/admin/resources/public-page', {'data': {'name': 'wrong-prefix'}})
                with self.assertRaises(RuntimeError):
                    runner.authorize('POST', '/api/v1/admin/users', {})


if __name__ == '__main__':
    unittest.main()
