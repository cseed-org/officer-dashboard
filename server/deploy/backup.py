#!/usr/bin/env python3
"""Encrypted D1 snapshot client. Backup uses only the public age recipient."""
import argparse
from contextlib import closing
import datetime as dt
import fcntl
import gzip
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import sys
import tempfile
import time
import urllib.request
import uuid

CHUNK = 1_900_000


def setting(key):
    direct, path = os.environ.get(key), os.environ.get(key + '_FILE')
    if not path:
        return direct or ''
    if direct:
        raise ValueError('set a credential or its _FILE, not both')
    with open(path, encoding='utf8') as source:
        value = source.read(262145)
    if not value.strip() or len(value) > 262144:
        raise ValueError('empty or oversized secret file')
    return value.strip()


def alert(message):
    print(message, file=sys.stderr)
    if os.environ.get('DISCORD_WEBHOOK_URL'):
        try:
            req = urllib.request.Request(os.environ['DISCORD_WEBHOOK_URL'], json.dumps({'content': message}).encode(),
                                         {'Content-Type': 'application/json', 'User-Agent': 'dashboard-backup'})
            urllib.request.urlopen(req, timeout=20).close()
        except Exception:
            print('Discord notification failed', file=sys.stderr)


class Sink:
    def __init__(self):
        self.url = os.environ['BACKUP_SINK_URL'].rstrip('/')
        if not self.url.startswith(('https://', 'http://127.0.0.1:', 'http://localhost:', 'http://host.docker.internal:')):
            raise ValueError('sink requires HTTPS')
        self.headers = {'Authorization': 'Bearer ' + setting('BACKUP_BEARER_SECRET'),
                       'CF-Access-Client-Id': setting('BACKUP_ACCESS_CLIENT_ID'),
                       'CF-Access-Client-Secret': setting('BACKUP_ACCESS_CLIENT_SECRET')}

    def request(self, path, method='GET', data=None, raw=False):
        headers = dict(self.headers)
        if data is not None and not isinstance(data, bytes):
            data = json.dumps(data).encode()
            headers['Content-Type'] = 'application/json'
        class NoRedirect(urllib.request.HTTPRedirectHandler):
            def redirect_request(self, *args):
                return None
        req = urllib.request.Request(self.url + path, data, headers, method=method)
        with urllib.request.build_opener(NoRedirect).open(req, timeout=120) as response:
            body = response.read(CHUNK + 10000 if raw else 100000)
            return body if raw else json.loads(body)

    def list(self):
        return self.request('/snapshots')

    def upload(self, path, kind='hourly'):
        size = path.stat().st_size
        if not 0 < size <= 13 * CHUNK:
            raise ValueError('snapshot exceeds 24.7 MB; skipped')
        chunks = (size + CHUNK - 1) // CHUNK
        allocation = 2 * (((size + 4095) // 4096) * 4096 + chunks * 8192 + 16384)
        self.request('/prune', 'POST', {'hourly': 48, 'reserve': allocation})
        usage = self.request('/usage')
        if usage['bytes'] + allocation > 350_000_000 or allocation * 82 > 450_000_000:
            alert('Backup storage pressure: falling back to 24 hourly snapshots')
            usage = self.request('/prune', 'POST', {'hourly': 24, 'reserve': allocation})
        writes = 100 + 20 * chunks
        if usage['bytes'] + allocation >= 350_000_000 or usage['writes'] + writes + 50_000 >= 70_000:
            alert('Backup usage reached 70% of a free-tier limit')
        # Each bounded client upload costs <100 requests and <3000 scanned rows,
        # including prune, usage, verification and scheduling. Leave shared-account headroom.
        if usage['bytes'] + allocation > usage['byte_limit'] or usage['writes'] + writes > usage['write_limit'] or usage['uploads'] >= 200:
            alert('BACKUP BLOCKED: reservation or daily budget exhausted; operator action required if storage remains full')
            raise ValueError('backup budget exhausted; skipped')
        with path.open('rb') as source:
            digest = hashlib.file_digest(source, 'sha256').hexdigest()
        snapshot = {'id': uuid.uuid4().hex, 'kind': kind, 'size': size, 'chunks': chunks, 'sha256': digest}
        self.request('/snapshots', 'POST', snapshot)
        with path.open('rb') as source:
            for n in range(chunks):
                self.request(f"/snapshots/{snapshot['id']}/chunks/{n}", 'PUT', source.read(CHUNK))
        result = self.request(f"/snapshots/{snapshot['id']}/commit", 'POST')
        if result['sha256'] != digest:
            raise ValueError('remote verification failed')
        with tempfile.TemporaryDirectory(dir=path.parent) as work:
            self.download(snapshot, Path(work) / 'verify.age')
        return snapshot

    def download(self, snapshot, target):
        digest, total = hashlib.sha256(), 0
        with target.open('xb') as output:
            for n in range(snapshot['chunks']):
                data = self.request(f"/snapshots/{snapshot['id']}/chunks/{n}", raw=True)
                digest.update(data)
                total += len(data)
                output.write(data)
        if total != snapshot['size'] or digest.hexdigest() != snapshot['sha256']:
            target.unlink()
            raise ValueError('ciphertext sha256/size mismatch')


def integrity(path):
    with closing(sqlite3.connect(Path(path).resolve().as_uri() + '?mode=ro', uri=True)) as db:
        if db.execute('PRAGMA integrity_check').fetchall() != [('ok',)]:
            raise ValueError('SQLite integrity check failed')


def copy_database(source, target):
    # Same sqlite3_backup API as the sqlite3 CLI's .backup, consistent with live WAL.
    with closing(sqlite3.connect(Path(source).resolve().as_uri() + '?mode=ro', uri=True)) as src, closing(sqlite3.connect(target)) as dst:
        src.backup(dst)
    integrity(target)


def encrypt(source, target):
    compressed = target.with_suffix('.gz')
    with source.open('rb') as src, gzip.open(compressed, 'wb') as dst:
        shutil.copyfileobj(src, dst)
    subprocess.run(['age', '-r', os.environ['BACKUP_AGE_RECIPIENT'], '-o', str(target), str(compressed)], check=True)
    compressed.unlink()


def backup(kind='hourly'):
    db = Path(os.environ.get('DB_PATH', '/data/dashboard.db'))
    with tempfile.TemporaryDirectory(prefix='backup-', dir=db.parent) as work:
        work = Path(work)
        copy_database(db, work / 'snapshot.db')
        encrypt(work / 'snapshot.db', work / 'snapshot.age')
        result = Sink().upload(work / 'snapshot.age', kind)
        print(result['id'])
        return result


R2_RESEED_SCHEMA = 'CREATE TABLE IF NOT EXISTS r2_reseed(id INTEGER PRIMARY KEY CHECK(id=1), requested_at INTEGER NOT NULL, fallback_at INTEGER NOT NULL DEFAULT 0, alerted INTEGER NOT NULL DEFAULT 0)'


def mark_restored_r2(path):
    # Restoring SQLite cannot recover R2 operations spent after the snapshot.
    # The marker makes the app re-seed its usage counters (Cloudflare analytics,
    # else half of each free-tier allowance) before its next billable request.
    with closing(sqlite3.connect(path)) as db, db:
        db.execute(R2_RESEED_SCHEMA)
        db.execute("INSERT INTO r2_reseed(id,requested_at) VALUES(1,CAST(strftime('%s','now') AS INTEGER)) ON CONFLICT(id) DO UPDATE SET requested_at=excluded.requested_at,fallback_at=0,alerted=0")


def restore(snapshot_id, target, identity, encrypted_only=False):
    sink = Sink()
    snapshots = [s for s in sink.list() if s['kind'] != 'd1-weekly' and (snapshot_id == 'latest' or s['id'] == snapshot_id)]
    if not snapshots:
        raise ValueError('no committed SQLite snapshot')
    target = Path(target).resolve()
    if target.exists():
        raise ValueError('destination already exists')
    with tempfile.TemporaryDirectory(prefix='restore-', dir=target.parent) as work:
        work = Path(work)
        sink.download(snapshots[0], work / 'snapshot.age')
        if encrypted_only:
            os.link(work / 'snapshot.age', target)
            return
        if not identity:
            raise ValueError('restore needs the off-box age identity')
        subprocess.run(['age', '-d', '-i', identity, '-o', str(work / 'snapshot.gz'), str(work / 'snapshot.age')], check=True)
        with gzip.open(work / 'snapshot.gz', 'rb') as src, (work / 'snapshot.db').open('wb') as dst:
            shutil.copyfileobj(src, dst)
        integrity(work / 'snapshot.db')
        mark_restored_r2(work / 'snapshot.db')
        os.link(work / 'snapshot.db', target)
    print('sha256 verified; integrity ok')
    print('R2 usage counters will be re-seeded on first start (Cloudflare analytics, else 50% of each free-tier allowance); do not remove r2_reseed.')


def membership():
    account, database = os.environ['BACKUP_MEMBERSHIP_ACCOUNT_ID'], os.environ['BACKUP_MEMBERSHIP_DATABASE_ID']
    url = f'https://api.cloudflare.com/client/v4/accounts/{account}/d1/database/{database}/export'
    bookmark = None
    for _ in range(60):
        payload = {'output_format': 'polling'}
        if bookmark:
            payload['current_bookmark'] = bookmark
        req = urllib.request.Request(url, json.dumps(payload).encode(),
            {'Authorization': 'Bearer ' + os.environ['BACKUP_MEMBERSHIP_API_TOKEN'], 'Content-Type': 'application/json'})
        with urllib.request.urlopen(req, timeout=120) as response:
            result = json.load(response)
        if not result.get('success'):
            raise ValueError('membership export rejected')
        result = result['result']
        if result.get('status') == 'error':
            raise ValueError('membership export failed')
        if result.get('status') == 'complete':
            with tempfile.TemporaryDirectory(dir=Path(os.environ.get('DB_PATH', '/data/dashboard.db')).parent) as work:
                work = Path(work)
                with urllib.request.urlopen(result['result']['signed_url'], timeout=120) as src, (work/'membership.sql').open('wb') as dst:
                    shutil.copyfileobj(src, dst)
                encrypt(work/'membership.sql', work/'membership.age')
                Sink().upload(work/'membership.age', 'd1-weekly')
            return
        bookmark = result.get('at_bookmark')
        time.sleep(5)
    raise ValueError('membership export timed out')


def loop():
    while True:
        try:
            # One backup writer, including host update's docker compose exec invocation.
            lock = Path(os.environ.get('DB_PATH', '/data/dashboard.db')).parent / '.backup.lock'
            with lock.open('a') as handle:
                fcntl.flock(handle, fcntl.LOCK_EX)
                sink, now = Sink(), dt.datetime.now(dt.timezone.utc)
                snapshot = backup()
                snapshots = sink.list()
                if not any(s['kind'] == 'daily' and s['created_at'][:10] == now.date().isoformat() for s in snapshots):
                    sink.request(f"/snapshots/{snapshot['id']}/promote", 'POST')
                week_start = (now.date() - dt.timedelta(days=now.weekday())).isoformat()
                if os.environ.get('BACKUP_MEMBERSHIP_API_TOKEN') and not any(s['kind'] == 'd1-weekly' and s['created_at'][:10] >= week_start for s in snapshots):
                    membership()
        except Exception as error:
            alert('Backup failed or skipped: ' + type(error).__name__)
        time.sleep(3600 - time.time() % 3600)


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser()
    parser.add_argument('command', choices=['backup', 'loop', 'restore', 'download', 'membership'])
    parser.add_argument('--snapshot', default='latest')
    parser.add_argument('--output')
    parser.add_argument('--identity')
    args = parser.parse_args()
    if args.command == 'loop':
        loop()
        return
    lock = Path(os.environ.get('DB_PATH', '/data/dashboard.db')).parent / '.backup.lock'
    with lock.open('a') as handle:
        fcntl.flock(handle, fcntl.LOCK_EX)
        if args.command == 'backup':
            backup()
        elif args.command == 'membership':
            membership()
        else:
            restore(args.snapshot, args.output, args.identity, args.command == 'download')


if __name__ == '__main__':
    try:
        main()
    except Exception as error:
        alert('Backup operation failed: ' + type(error).__name__)
        sys.exit(1)
