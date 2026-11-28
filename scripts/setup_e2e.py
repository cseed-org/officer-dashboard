#!/usr/bin/env python3
"""End to end, as root on a rootful Docker host: scripts/setup host writes real files
with real owners through the image, a second run changes nothing, and the container
users can read what was written for them.

  sudo python3 scripts/setup_e2e.py --image <registry>/<name>@sha256:<digest> [--work DIR]

The security workflow's Docker job runs it against the image it just built. It needs
root (real chown) and touches only a temporary directory.
"""
import argparse
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile

ROOT = Path(__file__).resolve().parents[1]
SECRETS = {
    'discord-client-secret': 'e2e-discord-client-secret',
    'tunnel-token': 'e2e-tunnel-token',
    'backup-access-client-id': 'e2e-access-id.access',
    'backup-access-client-secret': 'e2e-access-secret',
}
EXPECTED = {  # path: (mode, uid, gid)
    'secrets': (0o711, 0, 0),
    'secrets/app': (0o700, 10001, 10001),
    'secrets/backup': (0o700, 10001, 10001),
    'secrets/tunnel-token': (0o400, 65532, 65532),
    'secrets/app/discord-client-secret': (0o400, 10001, 10001),
    'secrets/backup/bearer': (0o400, 10001, 10001),
    'secrets/backup/access-client-id': (0o400, 10001, 10001),
    'secrets/backup/access-client-secret': (0o400, 10001, 10001),
    '.env': (0o600, 0, 0),
    'dashboard.env': (0o600, 0, 0),
    'backup.env': (0o600, 0, 0),
    'prod-image.env': (0o600, 0, 0),
}


def fail(message):
    print('E2E FAILED: ' + message, file=sys.stderr)
    sys.exit(1)


def snapshot(deploy):
    out = {}
    for path in sorted(deploy.rglob('*')):
        st = path.lstat()
        data = path.read_bytes() if path.is_file() else b''
        out[str(path.relative_to(deploy))] = (data, st.st_mode, st.st_uid, st.st_gid, st.st_mtime_ns)
    return out


def setup(deploy, *args):
    command = [sys.executable, str(ROOT / 'scripts/setup'), '--deploy-dir', str(deploy), *args]
    result = subprocess.run(command, capture_output=True, text=True, cwd='/')
    print('$ scripts/setup ' + ' '.join(args) + '\n' + result.stdout + result.stderr)
    for value in SECRETS.values():
        if value in result.stdout + result.stderr:
            fail('a secret value was printed')
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--image', required=True)
    parser.add_argument('--work', help='parent directory for the test install (default: a new temporary directory)')
    args = parser.parse_args()
    if os.geteuid() != 0:
        fail('run as root')
    work = Path(tempfile.mkdtemp(prefix='setup e2e ', dir=args.work))
    try:
        deploy = work / 'club install, one' / 'server' / 'deploy'
        deploy.mkdir(parents=True)
        for name in ['compose.cloudflare.yml', '.env.example', 'dashboard.env.example', 'backup.env.example']:
            shutil.copy(ROOT / 'server/deploy' / name, deploy / name)
        inputs = work / 'inputs'
        inputs.mkdir(mode=0o700)
        secret_args = []
        for name, value in SECRETS.items():
            (inputs / name).write_text(value + '\n')
            secret_args += ['--secret', f'{name}={inputs / name}']
        flags = ['--app-url', 'https://dash.club.example', '--discord-client-id', '123456789012345678',
                 '--owner-discord-id', '123456789012345679', '--owner-name', 'Ada Lovelace',
                 '--age-recipient', 'age1' + 'q' * 58]

        first = setup(deploy, 'host', '--yes', '--image', args.image, *secret_args, *flags)
        if first.returncode != 0:
            fail('first run exited %d' % first.returncode)
        for rel, (mode, uid, gid) in EXPECTED.items():
            st = (deploy / rel).lstat()
            if (st.st_mode & 0o7777, st.st_uid, st.st_gid) != (mode, uid, gid):
                fail('%s is %04o %d:%d, want %04o %d:%d' % (rel, st.st_mode & 0o7777, st.st_uid, st.st_gid, mode, uid, gid))
        for name, value in SECRETS.items():
            rel = {'discord-client-secret': 'secrets/app/discord-client-secret', 'tunnel-token': 'secrets/tunnel-token',
                   'backup-access-client-id': 'secrets/backup/access-client-id',
                   'backup-access-client-secret': 'secrets/backup/access-client-secret'}[name]
            if (deploy / rel).read_text() != value:
                fail(rel + ' does not hold the value given')
        if len((deploy / 'secrets/backup/bearer').read_text()) != 64:
            fail('the generated bearer is not 32 bytes of hex')

        before = snapshot(deploy)
        again = setup(deploy, 'host', '--yes', *flags)
        if again.returncode != 0 or 'nothing to do' not in again.stdout:
            fail('second run was not a no-op')
        preview = setup(deploy, 'host', '--dry-run')
        if preview.returncode != 0 or 'nothing to do' not in preview.stdout:
            fail('dry run on a complete install was not a no-op')
        if snapshot(deploy) != before:
            fail('a no-op run changed a file, mode, owner or mtime')

        # Real Compose reads the env files the way setup wrote them.
        config = subprocess.run(['docker', 'compose', '--env-file', '.env', '--env-file', 'prod-image.env',
                                 '-f', 'compose.cloudflare.yml', 'config', '--format', 'json'],
                                cwd=deploy, capture_output=True, text=True)
        if config.returncode != 0:
            fail('docker compose config: ' + config.stderr)
        services = json.loads(config.stdout)['services']
        if services['dashboard']['environment'].get('BOOTSTRAP_OWNER_NAME') != 'Ada Lovelace':
            fail('Compose read BOOTSTRAP_OWNER_NAME as %r' % services['dashboard']['environment'].get('BOOTSTRAP_OWNER_NAME'))
        if services['dashboard']['image'] != args.image:
            fail('Compose uses image %r' % services['dashboard']['image'])

        # The container users can read their files, through the same mounts Compose uses.
        for user, source, target, path, value in [
            ('10001:10001', 'secrets/app', '/run/secrets/app', '/run/secrets/app/discord-client-secret', SECRETS['discord-client-secret']),
            ('10001:10001', 'secrets/backup', '/run/secrets/backup', '/run/secrets/backup/access-client-id', SECRETS['backup-access-client-id']),
            ('65532:65532', 'secrets/tunnel-token', '/run/secrets/tunnel-token', '/run/secrets/tunnel-token', SECRETS['tunnel-token']),
        ]:
            read = subprocess.run(['docker', 'run', '--rm', '--user', user, '--network', 'none',
                                   '--mount', f'type=bind,"source={deploy / source}",target={target},readonly',
                                   '--entrypoint', '/bin/cat', args.image, path], capture_output=True, text=True)
            if read.returncode != 0 or read.stdout != value:
                fail(f'UID {user} cannot read {path}: {read.stderr.strip()}')
            other = subprocess.run(['docker', 'run', '--rm', '--user', '1000:1000', '--network', 'none',
                                    '--mount', f'type=bind,"source={deploy / source}",target={target},readonly',
                                    '--entrypoint', '/bin/cat', args.image, path], capture_output=True, text=True)
            if other.returncode == 0:
                fail(f'another UID can read {path}')
        print('E2E OK: owners, modes, no-op rerun, Compose parsing and container reads all verified.')
    finally:
        shutil.rmtree(work, ignore_errors=True)


if __name__ == '__main__':
    main()
