"""scripts/setup with every Docker and systemd command intercepted; Linux host tests."""
import contextlib
import csv
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
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
        self.compose_containers = []  # label dicts of containers Docker has
        self.volumes = []             # label dicts of volumes Docker has
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
        if args[:3] == ['docker', 'ps', '-a']:
            wanted = args[args.index('--filter') + 1].split('=', 1)[1].split('=', 1)[1]
            return '\n'.join(str(i) for i, c in enumerate(self.compose_containers)
                             if c.get('com.docker.compose.project') == wanted)
        if args[:2] == ['docker', 'inspect']:
            ids = [a for a in args[2:] if a.isdigit()]
            return '\n'.join(json.dumps(self.compose_containers[int(i)]) for i in ids)
        if args[:3] == ['docker', 'volume', 'ls']:
            wanted = args[args.index('--filter') + 1].split('=', 1)[1].split('=', 1)[1]
            return '\n'.join('v%d' % i for i, v in enumerate(self.volumes) if v.get('com.docker.compose.project') == wanted)
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


class LauncherCase(unittest.TestCase):
    """A deploy directory and every Docker and systemd command intercepted."""

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


class SetupTests(LauncherCase):
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

    def test_preview_runs_unprivileged_and_sees_no_secrets(self):
        # Review F-5: the networked preview gets only the tunnel token file and settings.
        self.pin()
        token = self.deploy.parent / 'cf token'
        token.write_text(TOKEN + '\n')
        (self.deploy / '.env').write_text('APP_URL=https://dash.club.example\nTUNNEL_TOKEN_FILE=./secrets/tunnel-token\n')
        (self.deploy / 'secrets').mkdir()
        (self.deploy / 'secrets/tunnel-token').write_text('tunnel')
        environ = dict(os.environ)
        self.assertEqual(self.launch('preview', '--token-file', str(token)), 0, self.err)
        args, stdin = self.container()
        for call in self.docker.calls:
            self.assertFalse(any(TOKEN in a for a in call), 'token on a command line: %r' % call)
        self.assertEqual(dict(os.environ), environ, 'token placed in the environment')
        self.assertEqual(stdin.strip(), TOKEN, 'the token goes over stdin')
        self.assertIn('CLOUDFLARE_SETUP_TOKEN_FILE=/dev/stdin', args)
        self.assertEqual(args[args.index('--user') + 1], '65532:65532')
        self.assertNotIn('--cap-add', args)
        self.assertEqual(args[args.index('--cap-drop') + 1], 'ALL')
        mounts = [mount_fields(args[i + 1]) for i, a in enumerate(args) if a == '--mount']
        self.assertEqual(mounts, [{'type': 'bind', 'source': str(self.deploy / 'secrets/tunnel-token'),
                                   'target': '/run/setup/tunnel-token', 'readonly': True}],
                         'only the tunnel token file, never the deploy directory or secrets/')
        self.assertIn('TUNNEL_TOKEN_FILE=/run/setup/tunnel-token', args)
        self.assertIn('APP_URL=https://dash.club.example', args)
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
        self.assertFalse(any(a == '--mount' for a in args), 'no tunnel token file, nothing to mount')

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

    # ── Compose project name and subnet (review F-1) ───────────────────────────────

    def project(self, working_dir, project='officer-dashboard'):
        """A container Docker has for project, started from working_dir."""
        self.docker.compose_containers.append({'com.docker.compose.project': project,
                                               'com.docker.compose.project.working_dir': str(working_dir)})
        self.docker.networks.append({'Name': project + '_tunnel', 'Labels': {'com.docker.compose.project': project},
                                     'IPAM': {'Config': [{'Subnet': '10.250.89.0/28'}]}})

    def assert_stopped_before_writing(self, code):
        self.assertEqual(code, 1, self.out)
        self.assertIn('--compose-project-name', self.err)
        self.assertIn('--tunnel-net', self.err)
        self.assertEqual(self.docker.containers, [], 'setup ran although it had to stop')
        self.assertFalse((self.deploy / '.env').exists())

    def test_defaults_left_alone_when_free(self):
        self.pin()
        self.assertEqual(self.launch('host', '--yes'), 0, self.err)
        args, _ = self.container()
        for flag in ['--compose-project-name', '--tunnel-net', '--install-exists']:
            self.assertNotIn(flag, args)

    def test_never_picks_a_name_beside_another_install(self):
        # A fresh deploy directory; another install already runs under the defaults.
        other = Path(self.temp.name) / 'other install/server/deploy'
        other.mkdir(parents=True)
        self.project(other)
        self.assert_stopped_before_writing(self.launch('host', '--yes', '--image', PIN))
        self.assertIn('officer-dashboard', self.err)
        self.assertIn(str(other), self.err)
        self.assertIn('10.250.89', self.err)
        self.assertFalse((self.deploy / 'prod-image.env').exists(), 'pin written before stopping')

    def test_explicit_values_are_passed_on(self):
        other = Path(self.temp.name) / 'other'
        other.mkdir()
        self.project(other)
        code = self.launch('host', '--yes', '--image', PIN, '--compose-project-name', 'club-two', '--tunnel-net', '10.250.90')
        self.assertEqual(code, 0, self.err)
        args, _ = self.container()
        tail = args[args.index(PIN) + 1:]
        self.assertEqual(tail[tail.index('--compose-project-name') + 1], 'club-two')
        self.assertEqual(tail[tail.index('--tunnel-net') + 1], '10.250.90')
        self.assertNotIn('--install-exists', tail, 'club-two has nothing in Docker yet')

    def test_a_used_subnet_alone_also_stops(self):
        self.docker.networks.append({'Name': 'lan', 'Labels': {}, 'IPAM': {'Config': [{'Subnet': '10.250.89.0/24'}]}})
        self.assert_stopped_before_writing(self.launch('host', '--yes', '--image', PIN))
        self.assertIn('lan', self.err)

    def test_running_install_started_by_the_documented_command(self):
        # `docker compose ... up -d` run from the deploy directory: its own stack.
        self.pin()
        self.project(self.deploy)
        self.docker.volumes.append({'com.docker.compose.project': 'officer-dashboard'})
        self.assertEqual(self.launch('host', '--yes'), 0, self.err)
        args, _ = self.container()
        self.assertNotIn('--compose-project-name', args)
        self.assertNotIn('--tunnel-net', args)
        self.assertIn('--install-exists', args)

    def test_install_reached_through_a_symlink_is_recognised(self):
        # Compose recorded the path through a symlink; setup was given the real path.
        link = Path(self.temp.name) / 'link'
        try:
            os.symlink(self.install, link)
        except OSError as error:
            self.skipTest('no symlinks: %s' % error)
        self.pin()
        self.project(link / 'server/deploy')
        self.assertEqual(self.launch('host', '--yes'), 0, self.err)
        args, _ = self.container()
        self.assertNotIn('--compose-project-name', args)
        self.assertIn('--install-exists', args)
        # And the other way round: setup reached through the symlink.
        self.docker.containers.clear()
        self.docker.compose_containers[0]['com.docker.compose.project.working_dir'] = str(self.deploy)
        out, err = io.StringIO(), io.StringIO()
        with contextlib.ExitStack() as stack:
            for name, value in [('run', self.docker.run), ('interactive', self.docker.interactive),
                                ('is_linux', lambda: True), ('is_root', lambda: True),
                                ('which', lambda name: '/usr/bin/' + name), ('SELINUX_ENFORCE', self.selinux),
                                ('stdin_is_tty', lambda: False)]:
                stack.enter_context(patch.object(setup, name, value))
            stack.enter_context(contextlib.redirect_stdout(out))
            stack.enter_context(contextlib.redirect_stderr(err))
            code = setup.main(['--deploy-dir', str(link / 'server/deploy'), 'host', '--yes'])
        self.assertEqual(code, 0, err.getvalue())

    def test_moved_checkout_stops_instead_of_renaming(self):
        # The stack was created from the old path, which no longer exists.
        self.pin()
        self.project(Path(self.temp.name) / 'old place/server/deploy')
        code = self.launch('host', '--yes')
        self.assertEqual(code, 1)
        self.assertIn('--compose-project-name', self.err)
        self.assertIn('confirm', self.err, 'a moved checkout may confirm its own values')
        self.assertEqual(self.docker.containers, [])
        # Confirming the current values goes ahead, and tells Go the install exists.
        code = self.launch('host', '--yes', '--compose-project-name', 'officer-dashboard', '--tunnel-net', '10.250.89')
        self.assertEqual(code, 0, self.err)
        self.assertIn('--install-exists', self.container()[0])

    def test_values_already_in_env_count_as_given(self):
        self.pin()
        (self.deploy / '.env').write_text('COMPOSE_PROJECT_NAME=club-two\nTUNNEL_NET=10.250.91\n')
        self.project(Path(self.temp.name) / 'elsewhere')
        self.assertEqual(self.launch('host', '--yes'), 0, self.err)
        args, _ = self.container()
        self.assertNotIn('--compose-project-name', args, 'nothing to pass: .env already says')

    def other_install(self, project='officer-dashboard'):
        """Install A: a live stack whose recorded working directory exists elsewhere."""
        other = Path(self.temp.name) / 'club-a/server/deploy'
        other.mkdir(parents=True, exist_ok=True)
        self.project(other, project)
        self.docker.volumes.append({'com.docker.compose.project': project})
        return other

    def assert_refused_without_confirm(self, code):
        self.assertEqual(code, 1, self.out)
        self.assertEqual(self.docker.containers, [], 'setup ran although another install owns the values')
        self.assertFalse((self.deploy / '.env').exists())
        self.assertFalse((self.deploy / 'prod-image.env').exists())
        self.assertNotIn('confirm', self.err.lower(), 'another install\'s values must never be offered to confirm')
        self.assertIn('free', self.err)

    def test_new_install_cannot_take_another_installs_project(self):
        # Review R-1 (S4b): B passes A's project and subnet, as if confirming them.
        other = self.other_install()
        code = self.launch('host', '--yes', '--image', PIN, '--compose-project-name', 'officer-dashboard', '--tunnel-net', '10.250.89')
        self.assert_refused_without_confirm(code)
        self.assertIn(str(other), self.err)
        # Unchosen defaults: refused the same way, still without a confirm hint.
        self.docker.containers.clear()
        self.assert_refused_without_confirm(self.launch('host', '--yes', '--image', PIN))

    def test_chosen_subnet_on_another_installs_network_is_refused(self):
        # Review R-1 (1): a free project name, but A's subnet.
        self.other_install()
        code = self.launch('host', '--yes', '--image', PIN, '--compose-project-name', 'club-b', '--tunnel-net', '10.250.89')
        self.assert_refused_without_confirm(code)
        self.assertIn('10.250.89', self.err)

    def test_env_copied_from_another_install_is_refused(self):
        # Review R-1 (4): .env copied from A names A's project.
        self.other_install()
        (self.deploy / '.env').write_text('COMPOSE_PROJECT_NAME=officer-dashboard\nTUNNEL_NET=10.250.89\n')
        code = self.launch('host', '--yes', '--image', PIN)
        self.assertEqual(code, 1, self.out)
        self.assertEqual(self.docker.containers, [])
        self.assertNotIn('confirm', self.err.lower())

    def test_stopped_install_may_confirm_its_own_values(self):
        # Review R-3: after `compose down` only the volume and network are left.
        self.pin()
        self.docker.volumes.append({'com.docker.compose.project': 'officer-dashboard'})
        self.docker.networks.append({'Name': 'officer-dashboard_tunnel', 'Labels': {'com.docker.compose.project': 'officer-dashboard'},
                                     'IPAM': {'Config': [{'Subnet': '10.250.89.0/28'}]}})
        self.assertEqual(self.launch('host', '--yes'), 1)
        self.assertIn('confirm', self.err)
        self.assertIn('stopped', self.err)
        self.assertEqual(self.launch('host', '--yes', '--compose-project-name', 'officer-dashboard', '--tunnel-net', '10.250.89'), 0, self.err)
        self.assertIn('--install-exists', self.container()[0])

    def test_copied_env_beside_a_stopped_install_reaches_go_as_an_existing_project(self):
        # Review T-1 (S6): A is stopped (volume and network, no containers); B has A's
        # .env copied but no pin, and confirms A's values. The launcher cannot tell A from
        # B, so it must hand Go --install-exists, which Go refuses without prod-image.env.
        self.docker.volumes.append({'com.docker.compose.project': 'officer-dashboard'})
        self.docker.networks.append({'Name': 'officer-dashboard_tunnel', 'Labels': {'com.docker.compose.project': 'officer-dashboard'},
                                     'IPAM': {'Config': [{'Subnet': '10.250.89.0/28'}]}})
        (self.deploy / '.env').write_text('APP_URL=https://club-a.example\n')
        self.docker.exit = 1  # what dashboard setup host answers
        code = self.launch('host', '--yes', '--image', PIN, '--compose-project-name', 'officer-dashboard', '--tunnel-net', '10.250.89')
        self.assertEqual(code, 1)
        self.assertIn('--install-exists', self.container()[0])
        self.assertFalse((self.deploy / 'prod-image.env').exists(), 'pin written although setup failed')

    def test_lan_subnet_asks_for_a_free_value(self):
        self.pin()
        self.docker.networks.append({'Name': 'lan', 'Labels': {}, 'IPAM': {'Config': [{'Subnet': '10.250.89.0/24'}]}})
        self.assertEqual(self.launch('host', '--yes'), 1)
        self.assertNotIn('confirm', self.err.lower())
        self.assertIn('free', self.err)

    def test_pin_is_written_only_after_setup_succeeds(self):
        self.docker.exit = 1
        self.assertEqual(self.launch('host', '--yes', '--image', 'registry.example.org/club/dashboard'), 1)
        self.assertFalse((self.deploy / 'prod-image.env').exists())

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

    def test_hand_rendered_unit_with_the_template_comments_is_the_same(self):
        # Review N-6: the documented sed keeps the template's comments; that is not a conflict.
        self.units = self.deploy.parent / 'systemd units'
        self.units.mkdir()
        template = (self.deploy / 'officer-dashboard-update.service').read_text()
        (self.units / 'officer-dashboard-update.service').write_text(template.replace('@INSTALL_DIR@', self.unit_install))
        self.assertEqual(self.install_units(), 0, self.err)
        self.assertNotIn('conflict', self.out)
        self.assertIn('exists', self.out)

    def test_units_refuse_paths_systemd_cannot_hold(self):
        for bad in ['relative/path', '/a\\b', '/a\nb', '/srv/$HOME', '/srv/"q"']:
            with self.subTest(path=bad):
                self.assertEqual(self.launch('install-units', '--unit-dir', str(self.deploy), '--install-dir', bad), 1)

    def test_percent_is_escaped_for_systemd(self):
        self.assertEqual(setup.render_unit('X=@INSTALL_DIR@/a\nExecStart="@INSTALL_DIR@/b"\n', '/srv/100% club'),
                         'X=/srv/100%% club/a\nExecStart="/srv/100%% club/b"\n')

    @unittest.skipUnless(shutil.which('systemd-analyze'), 'systemd-analyze not installed')
    def test_rendered_unit_passes_systemd_analyze_verify(self):
        for install in ['/opt/club-dashboard', '/srv/club dashboard', '/srv/100% club']:
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


class ApplyTests(LauncherCase):
    """scripts/setup apply: the Cloudflare apply, the only phase that writes to Cloudflare."""

    def setUp(self):
        super().setUp()
        # Built at run time: the scanner checks test files too.
        self.token_value = 'SENTINEL-' + 'cf-apply-' + 'Zq9' * 6
        self.token = self.deploy.parent / 'cf apply token'
        self.token.write_text(self.token_value + '\n')
        self.pin()
        (self.deploy / '.env').write_text('APP_URL=https://dash.club.example\nTUNNEL_TOKEN_FILE=./secrets/tunnel-token\n')
        (self.deploy / 'backup.env').write_text('BACKUP_SINK_URL=https://backups.club.example\n')

    def assert_token_never_leaves_the_file(self):
        for call in self.docker.calls:
            self.assertFalse(any(self.token_value in a for a in call), 'token on a command line: %r' % call)
        for _, stdin in self.docker.containers:
            self.assertFalse(stdin and self.token_value in stdin, 'token on stdin of a writing container')
        self.assertNotIn(self.token_value, self.out + self.err)

    def test_apply_runs_as_root_with_the_deploy_directory_writable_and_the_token_mounted(self):
        environ = dict(os.environ)
        before = (self.deploy / 'prod-image.env').read_text()
        self.assertEqual(self.launch('apply', '--yes', '--token-file', str(self.token), '--officers-email', 'p@club.example'), 0, self.err)
        args, stdin = self.container()
        self.assertIsNone(stdin)
        self.assert_token_never_leaves_the_file()
        self.assertEqual(dict(os.environ), environ, 'token placed in the environment')
        self.assertFalse(any(self.token_value in a for a in args[args.index('-e'):] if a != '-e'), 'token in -e')
        self.assertEqual(args[args.index('--user') + 1], '0:0')
        self.assertEqual(sorted(args[i + 1] for i, a in enumerate(args) if a == '--cap-add'), ['CHOWN', 'DAC_OVERRIDE', 'FOWNER'])
        self.assertEqual(args[args.index('--cap-drop') + 1], 'ALL')
        self.assertIn('--read-only', args)
        self.assertNotEqual(args[args.index('--network') + 1], 'none', 'the apply talks to Cloudflare')
        mounts = [mount_fields(args[i + 1]) for i, a in enumerate(args) if a == '--mount']
        self.assertEqual(mounts, [
            {'type': 'bind', 'source': str(self.deploy), 'target': '/deploy'},
            {'type': 'bind', 'source': str(self.token), 'target': '/run/setup/cloudflare-token', 'readonly': True},
        ])
        self.assertIn('CLOUDFLARE_SETUP_TOKEN_FILE=/run/setup/cloudflare-token', args)
        self.assertIn('APP_URL=https://dash.club.example', args)
        self.assertIn('BACKUP_SINK_URL=https://backups.club.example', args)
        tail = args[args.index(PIN) + 1:]
        self.assertEqual(tail[:4], ['setup', '--deploy-dir', '/deploy', '--yes'])
        self.assertNotIn('--dry-run', tail)
        self.assertEqual(tail[tail.index('--officers-email') + 1], 'p@club.example')
        self.assertEqual((self.deploy / 'prod-image.env').read_text(), before, 'the pin is never touched by apply')

    def test_apply_without_yes_gets_a_terminal_to_confirm_on(self):
        self.assertEqual(self.launch('apply', '--token-file', str(self.token), tty=True), 0, self.err)
        args, stdin = self.container()
        self.assertIn('-t', args)
        self.assertIn('-i', args)
        self.assertIsNone(stdin, 'the terminal stays free for the confirmation')
        self.assertNotIn('--yes', args[args.index(PIN) + 1:])
        self.assert_token_never_leaves_the_file()

    def test_apply_without_yes_or_terminal_still_runs_and_go_refuses_to_write(self):
        # Go prints the plan and exits 1 without a confirmation; the launcher passes that on.
        self.docker.exit = 1
        self.assertEqual(self.launch('apply', '--token-file', str(self.token)), 1)
        args, _ = self.container()
        self.assertNotIn('-t', args)
        self.assertNotIn('--yes', args)

    def test_apply_without_token_file_or_terminal_stops(self):
        self.assertEqual(self.launch('apply', '--yes'), 1)
        self.assertIn('--token-file', self.err)
        self.assertEqual(self.docker.containers, [])

    def test_apply_types_the_token_in_the_container_when_there_is_no_file(self):
        self.assertEqual(self.launch('apply', tty=True), 0, self.err)
        args, stdin = self.container()
        self.assertIsNone(stdin)
        self.assertIn('-t', args)
        self.assertFalse(any('CLOUDFLARE_SETUP_TOKEN_FILE' in a for a in args))
        self.assertEqual(len([a for a in args if a == '--mount']), 1, 'only the deploy directory')

    def test_apply_needs_the_host_phase_first(self):
        (self.deploy / 'prod-image.env').unlink()
        self.assertEqual(self.launch('apply', '--yes', '--token-file', str(self.token)), 1)
        self.assertIn('scripts/setup host --yes', self.err)
        self.assertEqual(self.docker.containers, [])
        self.assertFalse((self.deploy / 'prod-image.env').exists(), 'apply never writes a pin')

    def test_apply_refuses_flags_that_would_change_what_it_is(self):
        for flag in ['--dry-run', '--deploy-dir=/elsewhere', '--tunnel-token-file', '--image', '--json']:
            self.docker.containers.clear()
            self.assertEqual(self.launch('apply', '--yes', '--token-file', str(self.token), flag), 1, flag)
            self.assertIn(flag.split('=')[0], self.err)
            self.assertEqual(self.docker.containers, [], flag)

    def test_apply_keeps_every_host_refusal(self):
        for name, breaks in [('rootless', lambda: self.docker.security.append('name=rootless')),
                             ('userns-remap', lambda: self.docker.security.append('name=userns')),
                             ('SELinux', lambda: self.selinux.write_text('1\n'))]:
            self.docker = FakeDocker()
            self.selinux.unlink(missing_ok=True)
            breaks()
            self.assertEqual(self.launch('apply', '--yes', '--token-file', str(self.token)), 1, name)
            self.assertIn(name, self.err)
            self.assertEqual(self.docker.containers, [], name)

    def test_unreadable_token_file_names_the_path_only(self):
        self.assertEqual(self.launch('apply', '--yes', '--token-file', str(self.deploy.parent / 'missing')), 1)
        self.assertIn('missing', self.err)
        self.assertEqual(self.docker.containers, [])

    def test_refusals_match_every_spelling(self):
        # Review N-1: Go's flag package also takes one dash and =value.
        for flag in ['-dry-run', '--dry-run=true', '-json', '--json=1', '-deploy-dir=/elsewhere', '-tunnel-token-file=/x', '-image=x']:
            self.docker.containers.clear()
            self.assertEqual(self.launch('apply', '--yes', '--token-file', str(self.token), flag), 1, flag)
            self.assertIn('apply does not take', self.err, flag)
            self.assertEqual(self.docker.containers, [], flag)

    def test_preview_refuses_any_dry_run_or_yes_spelling(self):
        # Review N-1: `preview --dry-run=false` would otherwise reach Go's apply path.
        for flag in ['--dry-run=false', '-dry-run=false', '--dry-run', '--yes', '-yes', '--yes=true']:
            self.docker.containers.clear()
            self.assertEqual(self.launch('preview', '--token-file', str(self.token), flag), 1, flag)
            self.assertIn('preview does not take', self.err, flag)
            self.assertEqual(self.docker.containers, [], flag)

    def test_token_file_symlink_is_refused(self):
        # Review N-2: Docker would mount whatever the link points at.
        link = self.deploy.parent / 'token link'
        link.symlink_to(self.token)
        for command in ['apply', 'preview']:
            self.assertEqual(self.launch(command, '--yes' if command == 'apply' else '--json', '--token-file', str(link)), 1, command)
            self.assertIn('symbolic link', self.err)
        self.assertEqual(self.docker.containers, [])

    def test_token_file_readable_by_others_warns(self):
        self.token.chmod(0o600)
        self.assertEqual(self.launch('apply', '--yes', '--token-file', str(self.token)), 0, self.err)
        self.assertNotIn('warning', self.err)
        self.token.chmod(0o644)
        self.assertEqual(self.launch('apply', '--yes', '--token-file', str(self.token)), 0, self.err)
        self.assertIn('warning', self.err)
        self.assertIn('0644', self.err)
        self.assert_token_never_leaves_the_file()

    def test_relative_token_file_is_relative_to_the_current_directory(self):
        with contextlib.chdir(self.token.parent):
            self.assertEqual(self.launch('apply', '--yes', '--token-file', self.token.name), 0, self.err)
        args, _ = self.container()
        mounts = [mount_fields(args[i + 1]) for i, a in enumerate(args) if a == '--mount']
        self.assertEqual(mounts[1]['source'], str(self.token))

    def test_token_file_from_env_is_relative_to_the_deploy_directory(self):
        (self.deploy / 'cf-token').write_text(self.token_value + '\n')
        with open(self.deploy / '.env', 'a') as env:
            env.write('CLOUDFLARE_SETUP_TOKEN_FILE=cf-token\n')
        self.assertEqual(self.launch('apply', '--yes'), 0, self.err)
        args, _ = self.container()
        mounts = [mount_fields(args[i + 1]) for i, a in enumerate(args) if a == '--mount']
        self.assertEqual(mounts[1]['source'], str(self.deploy / 'cf-token'))

    def test_preview_is_unchanged_by_apply(self):
        # The dry run stays unprivileged and read-only: cloudflared's UID, no capabilities,
        # no deploy directory, token on stdin.
        self.assertEqual(self.launch('preview', '--token-file', str(self.token)), 0, self.err)
        args, stdin = self.container()
        self.assertEqual(args[args.index('--user') + 1], '65532:65532')
        self.assertNotIn('--cap-add', args)
        self.assertFalse(any(mount_fields(args[i + 1])['target'] == '/deploy' for i, a in enumerate(args) if a == '--mount'))
        self.assertEqual(args[args.index(PIN) + 1:][:2], ['setup', '--dry-run'])


class DiscordTests(LauncherCase):
    """scripts/setup discord [connect]: values reach Go only as read-only file mounts or typed
    in the container's terminal; the deploy directory is writable only when Go may write."""

    def setUp(self):
        super().setUp()
        self.pin()
        # Built at run time: the scanner checks test files too.
        self.values = {'client-id': '123456789012345678', 'client-secret': 'SENTINEL-' + 'dc-secret-' + 'Qx7' * 6,
                       'bot-token': 'SENTINEL-' + 'dc-bot-' + 'Lm4' * 8}
        self.files = {}
        for name, value in self.values.items():
            path = self.deploy.parent / ('my ' + name)
            path.write_text(value + '\n')
            path.chmod(0o600)
            self.files[name] = path

    def file_args(self):
        return [a for name, path in self.files.items() for a in ('--' + name + '-file', str(path))]

    def assert_values_never_leave_their_files(self):
        secrets = [self.values['client-secret'], self.values['bot-token']]
        for call in self.docker.calls:
            for secret in secrets:
                self.assertFalse(any(secret in a for a in call), 'a value on a command line: %r' % call)
        for _, stdin in self.docker.containers:
            self.assertIsNone(stdin, 'a value on stdin')
        for secret in secrets:
            self.assertNotIn(secret, self.out + self.err)

    def mounts(self, args):
        return [mount_fields(args[i + 1]) for i, a in enumerate(args) if a == '--mount']

    def caps(self, args):
        return sorted(args[i + 1] for i, a in enumerate(args) if a == '--cap-add')

    def test_yes_mounts_the_value_files_read_only_and_the_deploy_directory_writable(self):
        environ = dict(os.environ)
        self.assertEqual(self.launch('discord', '--yes', *self.file_args()), 0, self.err)
        args, stdin = self.container()
        self.assertIsNone(stdin)
        self.assert_values_never_leave_their_files()
        self.assertEqual(dict(os.environ), environ, 'a value placed in the environment')
        self.assertNotIn('-e', args, 'nothing passes through the environment')
        self.assertEqual(args[args.index('--user') + 1], '0:0')
        self.assertEqual(self.caps(args), ['CHOWN', 'DAC_OVERRIDE', 'FOWNER'])
        self.assertEqual(args[args.index('--cap-drop') + 1], 'ALL')
        self.assertIn('--read-only', args)
        self.assertEqual(args[args.index('--network') + 1], 'bridge', 'it checks the values with Discord')
        self.assertEqual(self.mounts(args), [
            {'type': 'bind', 'source': str(self.deploy), 'target': '/deploy'},
            {'type': 'bind', 'source': str(self.files['client-id']), 'target': '/run/setup/discord-client-id', 'readonly': True},
            {'type': 'bind', 'source': str(self.files['client-secret']), 'target': '/run/setup/discord-client-secret', 'readonly': True},
            {'type': 'bind', 'source': str(self.files['bot-token']), 'target': '/run/setup/discord-bot-token', 'readonly': True},
        ])
        tail = args[args.index(PIN) + 1:]
        self.assertEqual(tail[:5], ['setup', 'discord', '--deploy-dir', '/deploy', '--yes'])
        self.assertEqual(tail[tail.index('--client-secret-file') + 1], '/run/setup/discord-client-secret')

    def test_without_yes_nothing_is_writable(self):
        for mode in ([], ['--dry-run']):
            self.docker.containers.clear()
            self.assertEqual(self.launch('discord', *mode, *self.file_args()), 0, self.err)
            args, _ = self.container()
            self.assertEqual(self.mounts(args)[0], {'type': 'bind', 'source': str(self.deploy), 'target': '/deploy', 'readonly': True}, mode)
            self.assertEqual(self.caps(args), ['DAC_READ_SEARCH'], mode)
            self.assertNotIn('--yes', args, mode)
            self.assert_values_never_leave_their_files()

    def test_values_are_typed_in_the_container_terminal_when_there_is_no_file(self):
        self.assertEqual(self.launch('discord', '--yes', tty=True), 0, self.err)
        args, stdin = self.container()
        self.assertIsNone(stdin)
        self.assertIn('-t', args)
        self.assertEqual(len(self.mounts(args)), 1, 'only the deploy directory')

    def test_values_are_never_flags(self):
        for flag in ['--client-secret=x', '--bot-token', '--client-id=1', '--deploy-dir=/elsewhere', '--image=x', '-yes=false', '--unknown']:
            self.docker.containers.clear()
            self.assertEqual(self.launch('discord', flag), 1, flag)
            self.assertIn('discord does not take', self.err, flag)
            self.assertNotIn('=x', self.err, 'the refusal repeats a value')
            self.assertEqual(self.docker.containers, [], flag)

    def test_a_value_typed_after_a_flag_is_never_repeated(self):
        value = 'SENTINEL-' + 'typed-' + 'Wq2' * 5
        for argv in (['discord', '--client-secret', value], ['discord', value], ['discord', 'connect', '--bot-token', value],
                     ['discord', 'connect', value], ['discord', '--yes', value], ['discord', 'connect', '--x=' + value]):
            self.docker.containers.clear()
            self.assertEqual(self.launch(*argv), 1, argv)
            self.assertNotIn(value, self.out + self.err, argv)
            self.assertEqual(self.docker.containers, [], argv)

    def test_passes_on_the_flags_go_knows(self):
        self.assertEqual(self.launch('discord', '--dry-run', '--app-url', 'https://dash.club.example', '--no-bot-token', *self.file_args()), 0, self.err)
        tail = self.container()[0]
        tail = tail[tail.index(PIN) + 1:]
        self.assertEqual(tail[tail.index('--app-url') + 1], 'https://dash.club.example')
        self.assertIn('--no-bot-token', tail)

    def test_value_file_symlink_is_refused_and_a_shared_one_warns(self):
        link = self.deploy.parent / 'secret link'
        link.symlink_to(self.files['client-secret'])
        self.assertEqual(self.launch('discord', '--yes', '--client-secret-file', str(link)), 1)
        self.assertIn('symbolic link', self.err)
        self.assertEqual(self.docker.containers, [])
        self.files['client-secret'].chmod(0o644)
        self.assertEqual(self.launch('discord', '--yes', *self.file_args()), 0, self.err)
        self.assertIn('warning', self.err)
        self.assert_values_never_leave_their_files()

    def test_missing_value_file_names_the_path_only(self):
        self.assertEqual(self.launch('discord', '--yes', '--client-secret-file', str(self.deploy.parent / 'missing')), 1)
        self.assertIn('missing', self.err)
        self.assertEqual(self.docker.containers, [])

    def test_needs_the_host_phase_first(self):
        (self.deploy / 'prod-image.env').unlink()
        for step in (['discord', '--yes'], ['discord', 'connect']):
            self.assertEqual(self.launch(*step, tty=True), 1, step)
            self.assertIn('scripts/setup host --yes', self.err)
        self.assertEqual(self.docker.containers, [])

    def test_connect_writes_with_a_terminal_and_network(self):
        self.assertEqual(self.launch('discord', 'connect', tty=True), 0, self.err)
        args, stdin = self.container()
        self.assertIsNone(stdin)
        self.assertIn('-t', args)
        self.assertEqual(self.caps(args), ['CHOWN', 'DAC_OVERRIDE', 'FOWNER'])
        self.assertEqual(args[args.index('--network') + 1], 'bridge')
        self.assertEqual(self.mounts(args), [{'type': 'bind', 'source': str(self.deploy), 'target': '/deploy'}])
        self.assertEqual(args[args.index(PIN) + 1:], ['setup', 'discord', 'connect', '--deploy-dir', '/deploy'])

    def test_connect_needs_a_terminal(self):
        self.assertEqual(self.launch('discord', 'connect'), 1)
        self.assertIn('terminal', self.err)
        self.assertEqual(self.docker.containers, [])

    def test_connect_dry_run_reads_only_and_has_no_network(self):
        self.assertEqual(self.launch('discord', 'connect', '--dry-run'), 0, self.err)
        args, _ = self.container()
        self.assertEqual(args[args.index('--network') + 1], 'none')
        self.assertEqual(self.caps(args), ['DAC_READ_SEARCH'])
        self.assertEqual(self.mounts(args), [{'type': 'bind', 'source': str(self.deploy), 'target': '/deploy', 'readonly': True}])

    def test_connect_takes_no_value_files(self):
        self.assertEqual(self.launch('discord', 'connect', '--client-secret-file', str(self.files['client-secret']), tty=True), 1)
        self.assertIn('connect', self.err)
        self.assertEqual(self.docker.containers, [])

    def test_keeps_every_host_refusal(self):
        self.docker.security.append('name=rootless')
        self.assertEqual(self.launch('discord', '--yes', *self.file_args()), 1)
        self.assertIn('rootless', self.err)
        self.assertEqual(self.docker.containers, [])


class BytecodeTests(unittest.TestCase):
    def test_update_and_setup_write_no_bytecode_beside_themselves(self):
        # Review N-3: the updater runs as root from the production checkout every five
        # minutes; importing hostlib must not leave scripts/__pycache__ there.
        with tempfile.TemporaryDirectory(dir=os.environ.get('PANE_SCRATCH_DIR')) as temp:
            scripts = Path(temp) / 'scripts'
            scripts.mkdir()
            for name in ['update', 'setup', 'hostlib.py']:
                shutil.copy(ROOT / 'scripts' / name, scripts / name)
            for name in ['update', 'setup']:
                result = subprocess.run([sys.executable, str(scripts / name), '--help'], capture_output=True, text=True,
                                        env={k: v for k, v in os.environ.items() if k != 'PYTHONDONTWRITEBYTECODE'})
                self.assertEqual(result.returncode, 0, result.stderr)
            self.assertFalse((scripts / '__pycache__').exists())


if __name__ == '__main__':
    unittest.main()

