"""scripts/setup with every Docker and systemd command intercepted; Linux host tests."""
import contextlib
import csv
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

from test_operations import ROOT, load

setup = load('setup_launcher', ROOT / 'scripts/setup')
PIN = 'registry.example.org/club/dashboard@sha256:' + 'a' * 64
TOKEN = 'SENTINEL-cloudflare-token-value'
SECRET = 'SENTINEL-discord-client-secret'


def mount_fields(spec):
    """Parse a --mount value the way Docker does (CSV), into a dict."""
    fields = next(csv.reader([spec]))
    return dict(f.split('=', 1) if '=' in f else (f, True) for f in fields)


class FakeDocker:
    def __init__(self):
        self.security = ['name=seccomp,profile=builtin', 'name=cgroupns']
        self.compose = True
        self.daemon = True
        self.projects = []
        self.networks = []
        self.digest = 'sha256:' + 'b' * 64
        self.calls = []
        self.containers = []
        self.exit = 0

    def run(self, args, env=None):
        self.calls.append(list(args))
        if args[:2] == ['docker', 'info']:
            if not self.daemon:
                raise subprocess.CalledProcessError(1, args)
            return json.dumps({'SecurityOptions': self.security})
        if args[:3] == ['docker', 'compose', 'version']:
            if not self.compose:
                raise subprocess.CalledProcessError(1, args)
            return 'Docker Compose version v2.40.0'
        if args[:4] == ['docker', 'buildx', 'imagetools', 'inspect']:
            return json.dumps({'digest': self.digest})
        if args[:3] == ['docker', 'compose', 'ls']:
            return json.dumps(self.projects)
        if args[:3] == ['docker', 'network', 'ls']:
            return '\n'.join(str(i) for i in range(len(self.networks)))
        if args[:3] == ['docker', 'network', 'inspect']:
            return '\n'.join(json.dumps(n) for n in self.networks)
        if args[:1] == ['systemctl']:
            return ''
        raise AssertionError('unexpected command: %r' % args)

    def interactive(self, args, input=None):
        self.calls.append(list(args))
        self.containers.append((list(args), input))
        return self.exit


class SetupTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir=os.environ.get('PANE_SCRATCH_DIR'))
        self.addCleanup(self.temp.cleanup)
        self.install = Path(self.temp.name) / 'club install, "two"'
        self.deploy = self.install / 'server/deploy'
        self.deploy.mkdir(parents=True)
        for name in ['compose.cloudflare.yml', '.env.example', 'dashboard.env.example', 'backup.env.example',
                     'officer-dashboard-update.service', 'officer-dashboard-update.timer']:
            shutil.copy(ROOT / 'server/deploy' / name, self.deploy / name)
        self.docker = FakeDocker()
        self.linux = True
        self.root = True
        self.selinux = self.deploy.parent / 'selinux-enforce'  # absent: SELinux off
        self.unit_install = '/srv/club dashboard'  # systemd cannot hold the quote in self.install

    def launch(self, *argv, tty=False):
        out, err = io.StringIO(), io.StringIO()
        with contextlib.ExitStack() as stack:
            stack.enter_context(patch.object(setup, 'run', self.docker.run))
            stack.enter_context(patch.object(setup, 'interactive', self.docker.interactive))
            stack.enter_context(patch.object(setup, 'is_linux', lambda: self.linux))
            stack.enter_context(patch.object(setup, 'is_root', lambda: self.root))
            stack.enter_context(patch.object(setup, 'which', lambda name: '/usr/bin/' + name))
            stack.enter_context(patch.object(setup, 'SELINUX_ENFORCE', self.selinux))
            stack.enter_context(patch.object(setup, 'stdin_is_tty', lambda: tty))
            stack.enter_context(contextlib.redirect_stdout(out))
            stack.enter_context(contextlib.redirect_stderr(err))
            code = setup.main(['--deploy-dir', str(self.deploy)] + list(argv))
        self.out, self.err = out.getvalue(), err.getvalue()
        return code

    def pin(self, value=PIN):
        (self.deploy / 'prod-image.env').write_text('DASHBOARD_IMAGE=' + value + '\n')

    def container(self):
        self.assertEqual(len(self.docker.containers), 1, self.err)
        return self.docker.containers[0]

    # ── prerequisites ──────────────────────────────────────────────────────────────

    def assert_stops(self, *fragments):
        code = self.launch('host', '--dry-run')
        self.assertEqual(code, 1)
        for fragment in fragments:
            self.assertIn(fragment, self.err)
        self.assertEqual(self.docker.containers, [], 'a container ran despite a failed check')

    def test_needs_linux(self):
        self.linux = False
        self.assert_stops('Linux')

    def test_needs_root(self):
        self.root = False
        self.assert_stops('root', 'sudo -i')

    def test_needs_docker_daemon(self):
        self.docker.daemon = False
        self.assert_stops('Docker', 'systemctl start docker')

    def test_needs_compose_plugin(self):
        self.docker.compose = False
        self.assert_stops('Compose', 'docker-compose-plugin')

    def test_needs_the_deploy_directory(self):
        (self.deploy / 'compose.cloudflare.yml').unlink()
        self.assert_stops('compose.cloudflare.yml', '--deploy-dir')

    def test_stops_on_rootless_docker(self):
        self.docker.security.append('name=rootless')
        self.assert_stops('rootless', '10001', 'rootful')

    def test_stops_on_user_namespace_remapping(self):
        self.docker.security.append('name=userns')
        self.assert_stops('userns-remap', '10001')

    def test_stops_on_selinux_enforcing(self):
        self.selinux.write_text('1\n')
        self.assert_stops('SELinux', 'enforcing')

    def test_selinux_permissive_is_fine(self):
        self.selinux.write_text('0\n')
        self.pin()
        self.assertEqual(self.launch('host', '--dry-run'), 0, self.err)

    def test_check_command_runs_only_the_checks(self):
        self.pin()
        self.assertEqual(self.launch('check'), 0, self.err)
        self.assertIn('ready', self.out)
        self.assertEqual(self.docker.containers, [])

    # ── image pin ──────────────────────────────────────────────────────────────────

    def test_existing_pin_is_never_replaced(self):
        self.pin()
        before = (self.deploy / 'prod-image.env').read_bytes()
        for args in [('host', '--yes'), ('host', '--yes', '--image', 'other.example/x'), ('host', '--dry-run')]:
            self.docker.containers.clear()
            self.assertEqual(self.launch(*args), 0, self.err)
            self.assertEqual((self.deploy / 'prod-image.env').read_bytes(), before)
            self.assertIn(PIN, self.container()[0], 'the container must run the pinned image')
        self.assertFalse(any(c[:3] == ['docker', 'buildx', 'imagetools'] for c in self.docker.calls))

    def test_unpinned_existing_file_is_an_error_not_a_rewrite(self):
        self.pin('registry.example.org/club/dashboard:prod')
        self.assertEqual(self.launch('host', '--yes', '--image', 'registry.example.org/club/dashboard'), 1)
        self.assertIn('not pinned by digest', self.err)
        self.assertEqual((self.deploy / 'prod-image.env').read_text(), 'DASHBOARD_IMAGE=registry.example.org/club/dashboard:prod\n')

    def test_missing_pin_is_resolved_from_prod_and_written_once(self):
        self.assertEqual(self.launch('host', '--yes', '--image', 'registry.example.org/club/dashboard:release'), 0, self.err)
        self.assertIn(['docker', 'buildx', 'imagetools', 'inspect', 'registry.example.org/club/dashboard:prod', '--format', '{{json .Manifest}}'], self.docker.calls)
        state = self.deploy / 'prod-image.env'
        self.assertEqual(setup.dotenv(state)['DASHBOARD_IMAGE'], 'registry.example.org/club/dashboard@' + self.docker.digest)
        self.assertEqual(state.stat().st_mode & 0o777, 0o600)

    def test_dry_run_resolves_the_pin_but_does_not_write_it(self):
        self.assertEqual(self.launch('host', '--dry-run', '--image', 'registry.example.org/club/dashboard'), 0, self.err)
        self.assertFalse((self.deploy / 'prod-image.env').exists())
        self.assertIn('prod-image.env', self.out)

    def test_missing_pin_without_image_says_what_to_pass(self):
        self.assertEqual(self.launch('host', '--dry-run'), 1)
        self.assertIn('--image', self.err)

    # ── running the Go tool from the image ─────────────────────────────────────────

    def test_dry_run_mounts_read_only_and_drops_privileges(self):
        self.pin()
        self.assertEqual(self.launch('host', '--dry-run'), 0, self.err)
        args, stdin = self.container()
        self.assertEqual(args[:3], ['docker', 'run', '--rm'])
        mount = mount_fields(args[args.index('--mount') + 1])
        self.assertEqual(mount['source'], str(self.deploy))
        self.assertEqual(mount['target'], '/deploy')
        self.assertIn('readonly', mount)
        self.assertEqual(args[args.index('--cap-drop') + 1], 'ALL')
        caps = [args[i + 1] for i, a in enumerate(args) if a == '--cap-add']
        self.assertEqual(caps, ['DAC_READ_SEARCH'], 'a dry run may read, never write or chown')
        self.assertIn('--read-only', args)
        self.assertEqual(args[args.index('--network') + 1], 'none')
        self.assertEqual(args[args.index('--entrypoint') + 1], '/app/dashboard')
        tail = args[args.index(PIN) + 1:]
        self.assertEqual(tail[:2], ['setup', 'host'])
        self.assertIn('--dry-run', tail)
        self.assertEqual(tail[tail.index('--deploy-dir') + 1], '/deploy')

    def test_write_phase_runs_as_root_with_only_file_capabilities(self):
        self.pin()
        self.assertEqual(self.launch('host', '--yes', '--app-url', 'https://dash.club.example'), 0, self.err)
        args, _ = self.container()
        mount = mount_fields(args[args.index('--mount') + 1])
        self.assertNotIn('readonly', mount)
        self.assertEqual(args[args.index('--user') + 1], '0:0')
        caps = sorted(args[i + 1] for i, a in enumerate(args) if a == '--cap-add')
        self.assertEqual(caps, ['CHOWN', 'DAC_OVERRIDE', 'FOWNER'])
        self.assertEqual(args[args.index('--network') + 1], 'none')
        tail = args[args.index(PIN) + 1:]
        self.assertIn('--yes', tail)
        self.assertEqual(tail[tail.index('--app-url') + 1], 'https://dash.club.example')

    def test_preview_is_read_only_and_token_never_on_a_command_line(self):
        self.pin()
        token = self.deploy.parent / 'cf token'
        token.write_text(TOKEN + '\n')
        (self.deploy / '.env').write_text('APP_URL=https://dash.club.example\nTUNNEL_TOKEN_FILE=./secrets/tunnel-token\n')
        environ = dict(os.environ)
        self.assertEqual(self.launch('preview', '--token-file', str(token)), 0, self.err)
        args, stdin = self.container()
        for call in self.docker.calls:
            self.assertFalse(any(TOKEN in a for a in call), 'token on a command line: %r' % call)
        self.assertEqual(dict(os.environ), environ, 'token placed in the environment')
        self.assertTrue(stdin is None or TOKEN not in stdin)
        mounts = [mount_fields(args[i + 1]) for i, a in enumerate(args) if a == '--mount']
        self.assertTrue(all('readonly' in m for m in mounts), mounts)
        self.assertIn({'type': 'bind', 'source': str(token), 'target': '/run/setup/cloudflare-token', 'readonly': True}, mounts)
        self.assertIn('CLOUDFLARE_SETUP_TOKEN_FILE=/run/setup/cloudflare-token', args)
        self.assertIn('APP_URL=https://dash.club.example', args)
        self.assertIn('TUNNEL_TOKEN_FILE=/deploy/secrets/tunnel-token', args)
        tail = args[args.index(PIN) + 1:]
        self.assertEqual(tail[:2], ['setup', '--dry-run'])
        self.assertNotEqual(args[args.index('--network') + 1], 'none', 'the preview reads Cloudflare')

    def test_preview_without_a_token_file_prompts_in_the_container(self):
        self.pin()
        self.assertEqual(self.launch('preview', tty=True), 0, self.err)
        args, _ = self.container()
        self.assertIn('-t', args)
        self.assertIn('-i', args)
        self.assertFalse(any('CLOUDFLARE_SETUP_TOKEN_FILE' in a for a in args))

    def test_secrets_go_over_stdin_never_argv(self):
        self.pin()
        secret = self.deploy.parent / 'discord secret.txt'
        secret.write_text(SECRET + '\n')
        self.assertEqual(self.launch('host', '--yes', '--secret', 'discord-client-secret=' + str(secret)), 0, self.err)
        args, stdin = self.container()
        self.assertFalse(any(SECRET in a for c in self.docker.calls for a in c))
        self.assertEqual(json.loads(stdin), {'discord-client-secret': SECRET})
        self.assertIn('-i', args)
        self.assertNotIn('-t', args)
        self.assertIn('--secrets-stdin', args)

    def test_secret_can_be_typed_hidden(self):
        self.pin()
        with patch.object(setup.getpass, 'getpass', return_value=SECRET) as prompt:
            self.assertEqual(self.launch('host', '--yes', '--secret', 'tunnel-token=-', tty=True), 0, self.err)
        prompt.assert_called_once()
        self.assertEqual(json.loads(self.container()[1]), {'tunnel-token': SECRET})

    def test_container_failure_is_the_exit_code(self):
        self.pin()
        self.docker.exit = 2
        self.assertEqual(self.launch('host', '--dry-run', '--app-url', 'bad'), 2)

    def test_works_from_any_working_directory(self):
        self.pin()
        cwd = Path.cwd()
        try:
            os.chdir(self.temp.name)
            self.assertEqual(self.launch('host', '--dry-run'), 0, self.err)
        finally:
            os.chdir(cwd)
        mount = mount_fields(self.container()[0][self.container()[0].index('--mount') + 1])
        self.assertEqual(mount['source'], str(self.deploy))

    # ── Compose project name and subnet ────────────────────────────────────────────

    def test_defaults_left_alone_when_free(self):
        self.pin()
        self.assertEqual(self.launch('host', '--yes'), 0, self.err)
        args, _ = self.container()
        self.assertNotIn('--compose-project-name', args)
        self.assertNotIn('--tunnel-net', args)

    def test_picks_free_project_name_and_subnet_beside_another_install(self):
        self.pin()
        self.docker.projects = [{'Name': 'officer-dashboard', 'Status': 'running(3)', 'ConfigFiles': '/srv/other/server/deploy/compose.cloudflare.yml'}]
        self.docker.networks = [
            {'Name': 'officer-dashboard_tunnel', 'Labels': {'com.docker.compose.project': 'officer-dashboard'}, 'IPAM': {'Config': [{'Subnet': '10.250.89.0/28'}]}},
            {'Name': 'lan', 'Labels': {}, 'IPAM': {'Config': [{'Subnet': '10.250.90.0/24'}]}},
        ]
        self.assertEqual(self.launch('host', '--yes'), 0, self.err)
        args, _ = self.container()
        self.assertEqual(args[args.index('--compose-project-name') + 1], 'officer-dashboard-2')
        self.assertEqual(args[args.index('--tunnel-net') + 1], '10.250.91')

    def test_own_running_project_is_not_a_collision(self):
        self.pin()
        self.docker.projects = [{'Name': 'officer-dashboard', 'ConfigFiles': str(self.deploy / 'compose.cloudflare.yml')}]
        self.docker.networks = [{'Name': 'officer-dashboard_tunnel', 'Labels': {'com.docker.compose.project': 'officer-dashboard'}, 'IPAM': {'Config': [{'Subnet': '10.250.89.0/28'}]}}]
        self.assertEqual(self.launch('host', '--yes'), 0, self.err)
        args, _ = self.container()
        self.assertNotIn('--compose-project-name', args)
        self.assertNotIn('--tunnel-net', args)

    def test_values_already_in_env_are_kept(self):
        self.pin()
        (self.deploy / '.env').write_text('COMPOSE_PROJECT_NAME=officer-dashboard\nTUNNEL_NET=10.250.89\n')
        self.docker.projects = [{'Name': 'officer-dashboard', 'ConfigFiles': '/srv/other/compose.cloudflare.yml'}]
        self.assertEqual(self.launch('host', '--yes'), 0, self.err)
        args, _ = self.container()
        self.assertNotIn('--compose-project-name', args)
        self.assertNotIn('--tunnel-net', args)

    # ── systemd units ──────────────────────────────────────────────────────────────

    def install_units(self, *extra, tty=False):
        self.units = self.deploy.parent / 'systemd units'
        self.units.mkdir(exist_ok=True)
        return self.launch('install-units', '--unit-dir', str(self.units), '--install-dir', self.unit_install, *extra, tty=tty)

    def test_units_render_the_install_path_and_leave_the_timer_off(self):
        self.assertEqual(self.install_units(), 0, self.err)
        unit = (self.units / 'officer-dashboard-update.service').read_text()
        self.assertIn('WorkingDirectory=/srv/club dashboard/server/deploy\n', unit)
        self.assertIn('ExecStart=/usr/bin/python3 "/srv/club dashboard/scripts/update"\n', unit)
        self.assertNotIn('@INSTALL_DIR@', unit)
        self.assertTrue((self.units / 'officer-dashboard-update.timer').exists())
        self.assertEqual((self.units / 'officer-dashboard-update.service').stat().st_mode & 0o777, 0o644)
        self.assertIn(['systemctl', 'daemon-reload'], self.docker.calls)
        self.assertFalse(any(c[:2] == ['systemctl', 'enable'] for c in self.docker.calls), 'timer enabled without being asked')
        self.assertIn('--enable-updates', self.out)

    def test_units_enable_the_timer_only_when_asked(self):
        self.assertEqual(self.install_units('--enable-updates'), 0, self.err)
        self.assertIn(['systemctl', 'enable', '--now', 'officer-dashboard-update.timer'], self.docker.calls)

    def test_units_ask_and_default_to_off(self):
        with patch('builtins.input', return_value='') as ask:
            self.assertEqual(self.install_units(tty=True), 0, self.err)
        ask.assert_called_once()
        self.assertFalse(any(c[:2] == ['systemctl', 'enable'] for c in self.docker.calls))

    def test_existing_different_unit_is_left_alone(self):
        self.units = self.deploy.parent / 'systemd units'
        self.units.mkdir()
        (self.units / 'officer-dashboard-update.service').write_text('[Service]\nExecStart=/old\n')
        self.assertEqual(self.install_units(), 0, self.err)
        self.assertEqual((self.units / 'officer-dashboard-update.service').read_text(), '[Service]\nExecStart=/old\n')
        self.assertIn('differs', self.out)
        before = (self.units / 'officer-dashboard-update.timer').stat()
        self.docker.calls.clear()
        self.assertEqual(self.install_units(), 0, self.err)
        self.assertEqual((self.units / 'officer-dashboard-update.timer').stat().st_mtime_ns, before.st_mtime_ns)
        self.assertNotIn(['systemctl', 'daemon-reload'], self.docker.calls, 'nothing changed, nothing to reload')

    def test_units_refuse_paths_systemd_cannot_hold(self):
        for bad in ['relative/path', '/a\\b', '/a\nb']:
            with self.subTest(path=bad):
                self.assertEqual(self.launch('install-units', '--unit-dir', str(self.deploy), '--install-dir', bad), 1)

    def test_percent_is_escaped_for_systemd(self):
        self.assertEqual(setup.render_unit('X=@INSTALL_DIR@/a\nExecStart="@INSTALL_DIR@/b"\n', '/srv/100% club'),
                         'X=/srv/100%% club/a\nExecStart="/srv/100%% club/b"\n')

    @unittest.skipUnless(shutil.which('systemd-analyze'), 'systemd-analyze not installed')
    def test_rendered_unit_passes_systemd_analyze_verify(self):
        for install in ['/srv/club dashboard', '/srv/100% club']:
            with self.subTest(install=install):
                units = Path(tempfile.mkdtemp(dir=self.temp.name))
                service = units / 'officer-dashboard-update.service'
                service.write_text(setup.render_unit((ROOT / 'server/deploy/officer-dashboard-update.service').read_text(), install))
                # A stub docker.service, as in PR #119's check, so Requires= resolves anywhere.
                (units / 'docker.service').write_text('[Service]\nExecStart=/bin/true\n')
                result = subprocess.run(['systemd-analyze', 'verify', '--man=no', str(service)], capture_output=True, text=True,
                                        env=dict(os.environ, SYSTEMD_UNIT_PATH=str(units) + ':'))
                problems = [line for line in result.stderr.splitlines()
                            if 'officer-dashboard-update' in line and 'Failed to' not in line]
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertEqual(problems, [], result.stderr)


if __name__ == '__main__':
    unittest.main()
