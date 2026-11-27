"""Host helpers shared by scripts/update and scripts/setup. Linux, Python 3.11+, stdlib only.

scripts/update runs unattended on production: keep these functions' behaviour stable.
"""
import os
import re
import shlex
import subprocess


def run(args, env=None):
    # Capture output: compose may echo configuration; never include it in Discord/errors.
    return subprocess.run(args, check=True, text=True, capture_output=True, env=env).stdout.strip()


def dotenv(path):
    if not path.exists():
        return {}
    result = {}
    for line in path.read_text().splitlines():
        line = line.strip()
        if line.startswith('export '):
            line = line[7:].lstrip()
        if not line or line.startswith('#') or '=' not in line:
            continue
        key, value = line.split('=', 1)
        value = value.strip()
        if value.startswith(('"', "'")):
            parts = shlex.split(value, comments=True)
            if len(parts) != 1:
                raise ValueError('invalid quoted dotenv value')
            value = parts[0]
        else:
            value = re.split(r'\s+#', value, maxsplit=1)[0].rstrip()
        result[key.strip()] = value
    return result


def persist(path, value, mode=None):
    """Atomically replace path with value; mode=None keeps the process umask's default."""
    temporary = path.with_name(path.name + '.next')
    with temporary.open('w') as output:
        if mode is not None:
            os.fchmod(output.fileno(), mode)
        output.write(value)
        output.flush()
        os.fsync(output.fileno())
    os.replace(temporary, path)
    sync_directory(path.parent)


def sync_directory(path):
    fd = os.open(path, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


PINNED = re.compile(r'[a-zA-Z0-9./:_-]+@sha256:[a-f0-9]{64}')


def repository(image):
    """The repository of an image reference: digest dropped, and a tag dropped only from
    the last path segment, so a registry port (host:5000/club/repo) survives."""
    image_name = image.partition('@')[0]
    registry, separator, name = image_name.rpartition('/')
    return registry + separator + name.partition(':')[0]

