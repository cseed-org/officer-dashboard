"""Real updater/restore functions with external failures injected; Linux host tests."""
import contextlib
import gzip
import importlib.machinery
import importlib.util
import os
from pathlib import Path
import sqlite3
import subprocess
import tempfile
import unittest
from unittest.mock import patch

ROOT = Path(__file__).resolve().parents[1]


def load(name, path):
    loader = importlib.machinery.SourceFileLoader(name, str(path))
    module = importlib.util.module_from_spec(importlib.util.spec_from_loader(name, loader))
    loader.exec_module(module)
    return module


update = load('updater', ROOT / 'scripts/update')
backup = load('backup', ROOT / 'server/deploy/backup.py')
OLD = 'example/dashboard@sha256:' + 'a' * 64
NEW = 'example/dashboard@sha256:' + 'b' * 64


class UpdateTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir=os.environ.get('PANE_SCRATCH_DIR'))
        self.addCleanup(self.temp.cleanup)
        self.deploy = Path(self.temp.name)
        self.state = self.deploy / 'prod-image.env'
        self.state.write_text('DASHBOARD_IMAGE="' + OLD + '" # valid Compose pin\n')
        self.db = 'before'
        self.checkpoint = None
        self.calls = []
        self.failure = None
        self.rollback_failure = False

    def run_command(self, args, env=None):
        args = args[8:]  # docker compose and the three configuration options
        self.calls.append((args, env['DASHBOARD_IMAGE']))
        command = args[-1]
        if args[0] == 'stop' and self.failure == 'partial-stop':
            self.failure = None
            raise RuntimeError('dashboard stopped; backup stop failed')
        if args[0] == 'stop' and self.rollback_failure and self.db == 'migrated':
            raise RuntimeError('rollback stop failed')
        if command == 'backup' and args[0] == 'run':
            if self.failure == 'backup':
                self.failure = None
                raise RuntimeError('sink offline')
            return 'snapshot-id'
        if command == 'checkpoint':
            self.assertIsNone(self.checkpoint, 'original checkpoint overwritten')
            self.checkpoint = self.db
        if command == 'migrate' and self.failure == 'migrate':
            raise RuntimeError('candidate migration failed')
        if args == ['up', '-d', '--no-deps', 'backup'] and self.failure == 'sidecar':
            self.failure = None
            raise RuntimeError('backup service infrastructure failed')
        if args[0] == 'up' and env['DASHBOARD_IMAGE'] == NEW:
            self.db = 'migrated'
            if self.failure == 'startup':
                raise RuntimeError('candidate startup failed after migration')
        if command == 'rollback':
            self.db = self.checkpoint
        if command == 'cleanup':
            self.checkpoint = None
        return ''

    def poll(self):
        cwd = Path.cwd()
        try:
            with patch.object(update, 'run', self.run_command), patch.object(update, 'notify'), patch('sys.argv', ['update', '--deploy-dir', str(self.deploy), '--image', NEW]):
                update.main()
        finally:
            os.chdir(cwd)

    def test_poll_uses_pinned_repository_and_project_env_file(self):
        self.state.write_text('DASHBOARD_IMAGE=registry.example.org/club/dashboard@sha256:' + 'a' * 64 + '\n')
        self.deploy.joinpath('.env').write_text('COMPOSE_PROJECT_NAME=another-club\nTUNNEL_NET=10.250.90\n')
        def command(args, env=None):
            if args[:3] == ['docker', 'buildx', 'imagetools']:
                self.assertEqual(args[4], 'registry.example.org/club/dashboard:prod')
                return '{"digest":"sha256:' + 'b' * 64 + '"}'
            self.assertEqual(args[2:8], ['--env-file', str(self.deploy / '.env'), '--env-file', str(self.state), '-f', str(self.deploy / 'compose.cloudflare.yml')])
            return ''
        cwd = Path.cwd()
        try:
            with patch.object(update, 'run', command), patch.object(update, 'notify'), patch('sys.argv', ['update', '--deploy-dir', str(self.deploy)]):
                update.main()
        finally:
            os.chdir(cwd)
        self.assertEqual(update.dotenv(self.state)['DASHBOARD_IMAGE'], 'registry.example.org/club/dashboard@sha256:' + 'b' * 64)

    def test_two_polls_after_failed_rollback_preserve_checkpoint(self):
        self.failure = 'startup'
        self.rollback_failure = True
        with self.assertRaises(RuntimeError):
            self.poll()
        calls = len(self.calls)
        with self.assertRaisesRegex(RuntimeError, 'operator recovery'):
            self.poll()
        self.assertEqual(len(self.calls), calls)
        self.assertEqual(self.checkpoint, 'before')
        self.assertEqual(self.db, 'migrated')
        self.assertTrue((self.deploy / 'update-recovery-required.json').exists())
        self.assertEqual(update.dotenv(self.state)['DASHBOARD_IMAGE'], OLD)

    def test_sink_outage_retries_same_image_automatically(self):
        self.failure = 'backup'
        with self.assertRaises(RuntimeError):
            self.poll()
        self.assertFalse((self.deploy / 'rejected-prod-image').exists())
        self.poll()
        self.assertEqual(update.dotenv(self.state)['DASHBOARD_IMAGE'], NEW)

    def test_sidecar_failure_does_not_reject_candidate(self):
        self.failure = 'sidecar'
        with self.assertRaises(RuntimeError):
            self.poll()
        self.assertEqual(self.db, 'before')
        self.assertFalse((self.deploy / 'rejected-prod-image').exists())
        self.poll()
        self.assertEqual(update.dotenv(self.state)['DASHBOARD_IMAGE'], NEW)

    def test_partial_stop_reconciles_previous_services(self):
        self.failure = 'partial-stop'
        with self.assertRaises(RuntimeError):
            self.poll()
        self.assertIn((['up', '-d', '--no-deps', 'dashboard', 'backup'], OLD), self.calls)
        self.assertFalse((self.deploy / 'update-recovery-required.json').exists())
        self.poll()
        self.assertEqual(update.dotenv(self.state)['DASHBOARD_IMAGE'], NEW)

    def test_candidate_failure_rejected_only_after_successful_recovery(self):
        self.failure = 'startup'
        with self.assertRaises(RuntimeError):
            self.poll()
        self.assertEqual(self.db, 'before')
        calls = len(self.calls)
        self.poll()
        self.assertEqual(len(self.calls), calls)
        self.assertEqual((self.deploy / 'rejected-prod-image').read_text().strip(), NEW)

    def test_dotenv_quotes_comments_and_literal_hash(self):
        self.state.write_text('export DASHBOARD_IMAGE = "' + OLD + '" # pin\nA=foo#bar\nB=hello # note\nC=\'quoted value\'\n')
        self.assertEqual(update.dotenv(self.state), {'DASHBOARD_IMAGE': OLD, 'A': 'foo#bar', 'B': 'hello', 'C': 'quoted value'})


class RestoreTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir=os.environ.get('PANE_SCRATCH_DIR'))
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)

    def test_literal_sqlite_uri_characters(self):
        for name in ['percent%20', 'question?mode=memory#']:
            directory = self.root / name
            directory.mkdir()
            source, target = directory / 'source.db', directory / 'copy.db'
            with contextlib.closing(sqlite3.connect(source)) as db, db:
                db.execute('CREATE TABLE proof(value)')
                db.execute("INSERT INTO proof VALUES('present')")
            backup.copy_database(source, target)
            with contextlib.closing(sqlite3.connect(target)) as db:
                self.assertEqual(db.execute('SELECT value FROM proof').fetchone(), ('present',))
            target.write_bytes(b'not sqlite')
            with self.assertRaises(sqlite3.DatabaseError):
                backup.integrity(target)

    def test_restore_input_decrypts_gunzips_and_places(self):
        source_db = self.root / 'source.db'
        with contextlib.closing(sqlite3.connect(source_db)) as db, db:
            db.execute('CREATE TABLE proof(value)')
            db.execute("INSERT INTO proof VALUES('present')")
        ciphertext = self.root / 'dashboard-2026-11-20.db.gz.age'
        ciphertext.write_bytes(b'drive ciphertext')
        target = self.root / 'restored.db'
        def decrypt(args, **kwargs):
            with gzip.open(args[args.index('-o') + 1], 'wb') as output:
                output.write(source_db.read_bytes())
        with patch.object(backup.subprocess, 'run', decrypt):
            backup.restore_local(ciphertext, target, 'offline-identity')
        with contextlib.closing(sqlite3.connect(target)) as db:
            self.assertEqual(db.execute('SELECT value FROM proof').fetchone(), ('present',))
        with self.assertRaises(ValueError):
            backup.restore_local(ciphertext, target, 'offline-identity')
        with self.assertRaises(ValueError):
            backup.restore_local(ciphertext, self.root / 'other.db', '')
        self.assertFalse((self.root / 'other.db').exists())

    def test_restore_input_falls_back_to_copy_across_filesystems(self):
        source_db = self.root / 'source.db'
        with contextlib.closing(sqlite3.connect(source_db)) as db, db:
            db.execute('CREATE TABLE proof(value)')
            db.execute("INSERT INTO proof VALUES('present')")
        ciphertext = self.root / 'dashboard-2026-11-20.db.gz.age'
        ciphertext.write_bytes(b'drive ciphertext')
        target = self.root / 'restored.db'
        def decrypt(args, **kwargs):
            with gzip.open(args[args.index('-o') + 1], 'wb') as output:
                output.write(source_db.read_bytes())
        real_link = os.link
        def exdev(src, dst):
            if str(dst).endswith('snapshot.age'):
                raise OSError(18, 'Invalid cross-device link')
            return real_link(src, dst)
        with patch.object(backup.os, 'link', exdev), patch.object(backup.subprocess, 'run', decrypt):
            backup.restore_local(ciphertext, target, 'offline-identity')
        with contextlib.closing(sqlite3.connect(target)) as db:
            self.assertEqual(db.execute('SELECT value FROM proof').fetchone(), ('present',))

    def test_restore_never_replaces_competing_destination(self):
        for encrypted_only in [True, False]:
            target = self.root / str(encrypted_only)
            class Sink:
                def list(inner):
                    return [{'id': 'test', 'kind': 'hourly'}]
                def download(inner, snapshot, path):
                    path.write_bytes(b'ciphertext')
                    target.write_bytes(b'competing output')
            def decrypt(args, **kwargs):
                source = self.root / 'db'
                with contextlib.closing(sqlite3.connect(source)) as db, db:
                    db.execute('CREATE TABLE proof(value)')
                with gzip.open(args[args.index('-o') + 1], 'wb') as output:
                    output.write(source.read_bytes())
            with patch.object(backup, 'Sink', Sink), patch.object(backup.subprocess, 'run', decrypt):
                with self.assertRaises(FileExistsError):
                    backup.restore('latest', target, 'offline-identity', encrypted_only)
            self.assertEqual(target.read_bytes(), b'competing output')


if __name__ == '__main__':
    unittest.main()
