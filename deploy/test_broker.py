import importlib.util
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('broker', HERE / 'broker.py')
broker = importlib.util.module_from_spec(spec)
spec.loader.exec_module(broker)


class BoundaryTests(unittest.TestCase):
    def test_every_deployed_file_is_bound_to_the_fingerprint(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for name in broker.FILES:
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text('original')
            original = broker.fingerprint(root)
            for name in broker.FILES:
                path = root / name
                path.write_text('changed')
                self.assertNotEqual(original, broker.fingerprint(root))
                path.write_text('original')

    def test_ssh_rejects_arbitrary_commands_and_arguments(self):
        for command in ['', 'id', 'docker system dial-stdio', 'internal-sftp',
                        'infrastructure ' + 'a' * 64 + ';id',
                        'infrastructure ' + 'a' * 64 + '\n', 'infrastructure ../../tmp']:
            result = subprocess.run(['python3', '-I', str(HERE / 'ssh-command.py')],
                                    env={**os.environ, 'SSH_ORIGINAL_COMMAND': command},
                                    capture_output=True, text=True)
            self.assertEqual(result.returncode, 1)
            self.assertIn('Only the reviewed infrastructure deployment', result.stderr)


if __name__ == '__main__':
    unittest.main()
