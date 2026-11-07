#!/usr/bin/env python3
"""Build/run a disposable user-space company tunnel tester; never install or alter the live VPN."""
import argparse
from contextlib import contextmanager
import hashlib
import importlib.util
import ipaddress
import json
import os
from pathlib import Path
import plistlib
import re
import shutil
import signal
import socket
import subprocess
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
INSTALLED = Path('/Applications/GoConnect.app')
BUNDLE_ID = 'com.willhsu.GoConnect.CompanyTunnelTester'


def private_json(path, value):
    descriptor = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(descriptor, 'w') as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)


@contextmanager
def tester_workspace():
    work = Path(tempfile.mkdtemp(prefix='gcc-tester-')).resolve()
    try:
        yield work
    finally:
        active = False
        for row in subprocess.check_output(['ps', '-axo', 'comm='], text=True).splitlines():
            if row.strip().startswith('/') and Path(row.strip()).resolve().is_relative_to(work):
                active = True
        if active:
            print('Owned tester process still exists; temporary material preserved at ' + str(work), flush=True)
            raise RuntimeError('Temporary tester still has an owned live process; cleanup not accepted.')
        else:
            shutil.rmtree(work)


def run():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--profile', required=True, help='Existing AnyConnect profile UUID; read only.')
    parser.add_argument('--target', required=True, help='One explicitly authorized private IPv4 address.')
    parser.add_argument('--dns-name', required=True, help='One explicitly authorized company hostname.')
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--run', action='store_true', help='Open local password window after physical preflight.')
    args = parser.parse_args()
    evidence = args.evidence.resolve()
    if evidence == ROOT or ROOT in evidence.parents:
        parser.error('Evidence must be outside the repository.')
    evidence.mkdir(mode=0o700, parents=True, exist_ok=True)
    if evidence.stat().st_mode & 0o777 != 0o700 or evidence.stat().st_uid != os.getuid():
        parser.error('Evidence directory must be owned by current user with mode0700.')
    target = ipaddress.IPv4Address(args.target)
    if not any(target in ipaddress.ip_network(x) for x in ('10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16')):
        parser.error('Target must be an explicit RFC1918 IPv4 address.')
    configuration = Path.home() / 'Library/Application Support/com.willhsu.GoConnect/configuration.json'
    original = configuration.read_bytes()
    selected = next((x for x in json.loads(original)['connections'] if x['profile']['id'].lower() == args.profile.lower()), None)
    if not selected or selected.get('subscription'):
        parser.error('Choose an existing AnyConnect profile.')
    profile = selected['profile']
    server = profile['server'] if '://' in profile['server'] else 'https://' + profile['server']
    from urllib.parse import urlsplit
    parsed = urlsplit(server)
    if parsed.scheme != 'https' or not parsed.hostname or parsed.username or parsed.password or parsed.query or parsed.fragment:
        parser.error('Invalid company gateway.')
    addresses = sorted({x[4][0] for x in socket.getaddrinfo(parsed.hostname, parsed.port or 443, socket.AF_INET, socket.SOCK_STREAM)})
    if not 1 <= len(addresses) <= 4:
        parser.error('Gateway requires 1..4 pre-approved IPv4 addresses.')
    route = subprocess.check_output(['/sbin/route', '-n', 'get', '-inet', 'default'], text=True, timeout=5)
    match = re.search(r'^\s*interface:\s*((?:en|eth)\d+)\s*$', route, re.M)
    if not match:
        parser.error('No verified physical IPv4 default interface; no automatic VPN fallback.')
    interface = match.group(1)
    os.environ['PYTHONDONTWRITEBYTECODE'] = '1'
    with tester_workspace() as work:
        app = work / 'Company Tunnel Tester.app'
        contents = app / 'Contents'
        (contents / 'MacOS').mkdir(parents=True)
        shutil.copytree(INSTALLED / 'Contents/Resources/Runtime', contents / 'Resources/Runtime', symlinks=True)
        executable = contents / 'Resources/Runtime/bin/GoConnectTransport'
        environment = os.environ.copy()
        environment.update({'CGO_ENABLED': '1', 'MACOSX_DEPLOYMENT_TARGET': '14.0', 'CGO_CFLAGS': '-mmacosx-version-min=14.0', 'CGO_LDFLAGS': '-mmacosx-version-min=14.0'})
        with (evidence / 'tester-build.log').open('w') as log:
            subprocess.run(['go', 'build', '-trimpath', '-o', str(executable), './cmd/goconnect-transport'], cwd=ROOT / 'Transport', env=environment, stdout=log, stderr=subprocess.STDOUT, check=True)
            subprocess.run(['xcrun', 'swiftc', '-swift-version', '6', '-warnings-as-errors', '-target', 'arm64-apple-macosx14.0', '-framework', 'AppKit', str(ROOT / 'Tools/CompanyTunnelTester/main.swift'), '-o', str(contents / 'MacOS/CompanyTunnelTester')], stdout=log, stderr=subprocess.STDOUT, check=True)
        with (contents / 'Info.plist').open('wb') as stream:
            plistlib.dump({'CFBundleIdentifier': BUNDLE_ID, 'CFBundleExecutable': 'CompanyTunnelTester', 'CFBundleName': 'Company Tunnel Tester', 'CFBundleDisplayName': '公司隧道独立测试', 'CFBundlePackageType': 'APPL', 'LSMinimumSystemVersion': '14.0', 'NSPrincipalClass': 'NSApplication', 'CFBundleDevelopmentRegion': 'zh-Hans'}, stream)
        spec = importlib.util.spec_from_file_location('tester_signing', ROOT / 'script/signing.py')
        signing = importlib.util.module_from_spec(spec)
        import sys
        sys.modules[spec.name] = signing
        spec.loader.exec_module(signing)
        sign_env = dict(os.environ, GOCONNECT_REQUIRE_TEAM_SIGN='1', GOCONNECT_SIGN_TEAM='RX44JC39GB')
        signing.sign_bundle(app, signing.resolve_configuration(sign_env))
        request = {'server': server, 'username': profile['username'], 'group': profile.get('group', ''), 'password': '', 'openconnectPath': str(contents / 'Resources/Runtime/bin/openconnect'), 'interface': interface, 'gatewayIPs': addresses, 'network': str(target) + '/32', 'dnsName': args.dns_name.lower().rstrip('.'), 'evidenceDirectory': str(evidence)}
        preflight = subprocess.run([str(executable), 'company-test-preflight'], input=json.dumps(request) + '\n', capture_output=True, text=True, timeout=25)
        (evidence / 'physical-preflight.ndjson').write_text(preflight.stdout)
        os.chmod(evidence / 'physical-preflight.ndjson', 0o600)
        if preflight.returncode:
            raise RuntimeError('Physical gateway preflight failed; no authentication or primary-VPN fallback was attempted. See private evidence.')
        config = dict(request, transportPath=str(executable), profileName=profile['name'])
        del config['password']
        path = work / 'test-configuration.json'
        private_json(path, config)
        record = {'app': str(app), 'root': str(work), 'config': str(path), 'evidence': str(evidence), 'bundleID': BUNDLE_ID, 'profileID': args.profile, 'interface': interface, 'gatewayIPs': addresses, 'productionConfigSHA256': hashlib.sha256(original).hexdigest(), 'sourceCommit': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()}
        private_json(evidence / ('session-' + str(time.time_ns()) + '.json'), record)
        print(json.dumps({'app': str(app), 'config': str(path), 'evidence': str(evidence), 'physicalPreflight': 'passed', 'passwordInConfig': False}), flush=True)
        if not args.run:
            return
        launcher = subprocess.Popen(['open', '-n', '-W', str(app), '--args', '--config', str(path)])
        def stop(_signum, _frame):
            raise KeyboardInterrupt
        signal.signal(signal.SIGTERM, stop)
        try:
            result = launcher.wait()
            if result:
                raise RuntimeError('Native tester did not exit normally.')
        finally:
            # Only this exact temporary GUI binary can be signalled by cleanup.
            expected = (contents / 'MacOS/CompanyTunnelTester').resolve()
            owned = []
            for row in subprocess.check_output(['ps', '-axo', 'pid=,comm='], text=True).splitlines():
                columns = row.strip().split(None, 1)
                if len(columns) == 2 and columns[1].startswith('/') and Path(columns[1]).resolve() == expected:
                    pid = int(columns[0]); owned.append(pid); os.kill(pid, signal.SIGTERM)
            for _ in range(160):
                alive = []
                for pid in owned:
                    try:
                        os.kill(pid, 0); alive.append(pid)
                    except ProcessLookupError:
                        pass
                if not alive:
                    break
                time.sleep(.1)
            else:
                # Preserve running material rather than unlink an owned live app.
                raise RuntimeError('Tester has not exited; preserve the temporary directory and inspect its owned process.')
            if launcher.poll() is None:
                launcher.terminate(); launcher.wait(timeout=5)
            if configuration.read_bytes() != original:
                print('Production configuration changed concurrently; this tester did not write it.', flush=True)
    print('Temporary tester app, runtime and non-secret launch configuration removed.', flush=True)

if __name__ == '__main__':
    run()
