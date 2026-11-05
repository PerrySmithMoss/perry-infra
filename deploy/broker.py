#!/usr/bin/python3 -I
"""Deploy only the administrator-installed infrastructure snapshot."""
import fcntl
import hashlib
import os
from pathlib import Path
import re
import subprocess
import sys

ROOT = Path('/opt/perry-infra-release')
FILES = ('docker-compose.stack.yaml', 'postgres/pg_hba.conf', 'docker-proxy/haproxy.cfg')


def fingerprint(root):
    digest = hashlib.sha256()
    for name in FILES:
        digest.update(name.encode() + b'\0' + (root / name).read_bytes() + b'\0')
    return digest.hexdigest()


def main():
    if len(sys.argv) != 2 or not re.fullmatch('[0-9a-f]{64}', sys.argv[1]):
        raise ValueError('Expected the reviewed manifest fingerprint.')
    if os.geteuid() != 0 or fingerprint(ROOT) != sys.argv[1]:
        raise ValueError('An administrator must install this exact infrastructure snapshot first.')
    os.umask(0o077)
    with open('/run/lock/perry-infra-release.lock', 'w') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        env = {'PATH': '/usr/sbin:/usr/bin:/sbin:/bin', 'HOME': '/root',
               'DOCKER_HOST': 'unix:///var/run/docker.sock'}
        subprocess.run(['/bin/sh', '-c',
                        'set -eu; set -a; . ./deployment.env; '
                        'docker stack config -c docker-compose.stack.yaml >/dev/null; '
                        'docker stack deploy --resolve-image always --detach=false -c docker-compose.stack.yaml perry-infra; '
                        'curl --fail --retry 12 --retry-delay 5 --retry-all-errors --max-time 10 https://bulkbets-api.perrysmithmoss.com/api/v1/health'],
                       cwd=ROOT, env=env, check=True, timeout=900)


if __name__ == '__main__':
    try:
        main()
    except (ValueError, OSError, subprocess.SubprocessError):
        print('Infrastructure release rejected or failed. Verify the administrator-installed snapshot and service health.', file=sys.stderr)
        sys.exit(1)
