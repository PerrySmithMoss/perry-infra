#!/usr/bin/python3 -I
import os
import re
import sys

match = re.fullmatch(r'infrastructure ([0-9a-f]{64})', os.environ.get('SSH_ORIGINAL_COMMAND', ''))
if not match:
    print('Only the reviewed infrastructure deployment is allowed.', file=sys.stderr)
    sys.exit(1)
os.execv('/usr/bin/sudo', ['/usr/bin/sudo', '-n', '/usr/local/libexec/perry-infra-release', match[1]])
