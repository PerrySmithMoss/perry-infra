"""Exercise the fail-closed startup guard with valid and unsafe configurations."""
import os
import shutil
import sys
import pathlib
import subprocess
import tempfile
import unittest

ROOT = pathlib.Path(__file__).resolve().parent


class StartupGuardTests(unittest.TestCase):
    def check(self, content, allowed):
        with tempfile.NamedTemporaryFile(mode="w") as config:
            config.write(content)
            config.flush()
            for command in (["sh", str(ROOT / "check-config.sh"), config.name],
                            ["sh", str(ROOT / "verify.sh"), "--config-only", config.name]):
                result = subprocess.run(command, capture_output=True, text=True)
                self.assertEqual(result.returncode == 0, allowed, result.stderr)

    def test_managed_config(self):
        self.check((ROOT / "netdata.conf").read_text(), True)

    def test_formatted_config(self):
        self.check("# header\n [web] \n  mode = none \n[db]\n mode = dbengine\n", True)

    def test_unsafe_configs(self):
        for text in ("", "[web]\n", "[web]\nmode = static-threaded\n",
                     "[web]\n# mode = none\n", "[db]\nmode = none\n",
                     "[web]\nmode = none\nmode = static-threaded\n",
                     "[web]\nmode = static-threaded\nmode = none\n",
                     "[web]\nmode = none\n[web]\nmode = none\n",
                     "[web]\nmode = none\nmode = none\n"):
            with self.subTest(config=text):
                self.check(text, False)

    def test_missing_file(self):
        result = subprocess.run(["sh", str(ROOT / "check-config.sh"),
                                 "/nonexistent-perry-netdata-config"], capture_output=True)
        self.assertNotEqual(result.returncode, 0)


class RuntimeVerificationTests(unittest.TestCase):
    def verify(self, *, runtime="[web]\nmode = none\n", listener="",
               cloud="Claimed: Yes\nOnline: Yes\n", socket_error=False, local=False, ping_failures=0):
        # Substitute only host paths/command lookup in a disposable script copy.
        # All real privileged/system commands are replaced by deterministic stubs.
        with tempfile.TemporaryDirectory() as directory:
            root = pathlib.Path(directory)
            config = root / "netdata.conf"
            config.write_text("[web]\nmode = none\n")
            shutil.copy(ROOT / "check-config.sh", root / "check-config.sh")
            script = (ROOT / "verify.sh").read_text().replace(
                "PATH=/usr/sbin:/usr/bin:/sbin:/bin",
                'PATH="' + str(root) + ':$PATH"').replace(
                    "/etc/netdata/netdata.conf", str(config))
            (root / "verify.sh").write_text(script)
            mock = "#!" + sys.executable + "\n" + (
                "import os,sys\n"
                "from pathlib import Path\n"
                "name=Path(sys.argv[0]).name\n"
                "if name=='id': print('0')\n"
                "elif name=='netdatacli':\n"
                " if sys.argv[1]=='ping':\n"
                "  counter=Path(os.environ['MOCK_PING_COUNTER'])\n"
                "  count=int(counter.read_text()) if counter.exists() else 0\n"
                "  counter.write_text(str(count+1))\n"
                "  sys.exit(1 if count<int(os.environ['MOCK_PING_FAILURES']) else 0)\n"
                " print(os.environ['MOCK_RUNTIME' if sys.argv[1]=='dumpconfig' else 'MOCK_CLOUD'])\n"
                "elif name=='ss':\n"
                " print(os.environ['MOCK_LISTENER'],end='')\n"
                " sys.exit(int(os.environ['MOCK_SOCKET_ERROR']))\n"
            )
            for name in ("id", "netdatacli", "ss", "systemctl", "free", "df", "sleep"):
                command = root / name
                command.write_text(mock)
                command.chmod(0o755)
            env = dict(os.environ, MOCK_RUNTIME=runtime, MOCK_LISTENER=listener,
                       MOCK_CLOUD=cloud, MOCK_SOCKET_ERROR=str(int(socket_error)),
                       MOCK_PING_COUNTER=str(root / "ping-counter"),
                       MOCK_PING_FAILURES=str(ping_failures))
            return subprocess.run(["sh", str(root / "verify.sh")] +
                                  (["--local-only"] if local else []),
                                  env=env, capture_output=True, text=True)

    def test_cloud_online(self):
        self.assertEqual(self.verify().returncode, 0)

    def test_waits_for_control_pipe_before_reading_config(self):
        self.assertEqual(self.verify(ping_failures=2).returncode, 0)

    def test_control_pipe_readiness_times_out(self):
        result = self.verify(ping_failures=100)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("did not become ready", result.stderr)

    def test_local_check_before_claim(self):
        self.assertEqual(self.verify(cloud="Claimed: No", local=True).returncode, 0)

    def test_rejects_stale_public_runtime(self):
        self.assertNotEqual(self.verify(runtime="[web]\nmode = static-threaded\n").returncode, 0)

    def test_rejects_listener_or_failed_socket_inspection(self):
        self.assertNotEqual(self.verify(listener="LISTEN 0 128 0.0.0.0:19999").returncode, 0)
        self.assertNotEqual(self.verify(socket_error=True).returncode, 0)

    def test_rejects_unclaimed_or_offline(self):
        self.assertNotEqual(self.verify(cloud="Claimed: No\nOnline: Yes").returncode, 0)
        self.assertNotEqual(self.verify(cloud="Claimed: Yes\nOnline: No").returncode, 0)


if __name__ == "__main__":
    unittest.main()
