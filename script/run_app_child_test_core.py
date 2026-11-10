#!/usr/bin/env python3
"""Explicitly authorized, bounded root core for native process tests; never a TUN."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import time
from urllib.parse import urlsplit

TEAM = 'RX44JC39GB'

def loopback_upstream(value):
    if not isinstance(value, str):
        return False
    value = value.split('#', 1)[0]
    parsed = urlsplit(value if '://' in value else '//' + value)
    return parsed.hostname == '127.0.0.1' and (parsed.port is None or 1024 < parsed.port <= 65535)

def validate_config(original):
    cfg = json.loads(json.dumps(original))
    if cfg.get('tun', {}).get('enable') is not False:
        raise ValueError('Native test requires TUN explicitly disabled')
    if cfg.get('redir-port') or cfg.get('tproxy-port'):
        raise ValueError('Transparent listeners are forbidden')
    for key in ['port', 'socks-port', 'mixed-port']:
        value = cfg.get(key, 0)
        if value and not 1024 < value <= 65535:
            raise ValueError('Privileged listener ports are forbidden')
    allowed_rules = {'IP-CIDR', 'IP-CIDR6', 'DOMAIN', 'DOMAIN-SUFFIX', 'DOMAIN-KEYWORD',
                     'PROCESS-PATH-REGEX', 'GOCONNECT-APP-CHILD', 'MATCH', 'AND', 'OR', 'NOT', 'NETWORK'}
    rule_sets = [cfg.get('rules', [])] + list(cfg.get('sub-rules', {}).values())
    for rules in rule_sets:
        if any(rule.split(',', 1)[0] not in allowed_rules for rule in rules):
            raise ValueError('Only inline fixture rules are allowed')
    for proxy in cfg.get('proxies', []):
        kind = proxy.get('type')
        if kind == 'socks5':
            if proxy.get('server') != '127.0.0.1' or not 1024 < proxy.get('port', 0) <= 65535:
                raise ValueError('Only loopback fixture egress is allowed')
        elif kind != 'rematch':
            raise ValueError('Non-fixture egress is forbidden')
    if not cfg.get('proxies'):
        raise ValueError('Fixture egress is required')
    dns = cfg.get('dns', {})
    if not loopback_upstream(dns.get('listen')):
        raise ValueError('DNS listener must be loopback and unprivileged')
    for key in ['nameserver', 'direct-nameserver', 'proxy-server-nameserver', 'fallback']:
        if any(not loopback_upstream(value) for value in dns.get(key, [])):
            raise ValueError('Only loopback DNS fixtures are allowed')
    for values in dns.get('nameserver-policy', {}).values():
        if isinstance(values, str):
            values = [values]
        if any(not loopback_upstream(value) for value in values):
            raise ValueError('Non-loopback policy DNS is forbidden')
    dns['default-nameserver'] = ['127.0.0.1']
    for key in ['external-controller', 'external-controller-tls', 'external-controller-unix',
                'external-controller-pipe', 'external-ui', 'external-ui-url', 'secret', 'listeners',
                'rule-providers', 'proxy-providers']:
        cfg.pop(key, None)
    cfg.update({'mode': 'rule', 'allow-lan': False, 'bind-address': '127.0.0.1',
                'geo-auto-update': False, 'profile': {'store-selected': False, 'store-fake-ip': False}})
    return cfg

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('binary', type=Path)
    parser.add_argument('config', type=Path)
    parser.add_argument('lease', type=Path)
    parser.add_argument('uid', type=int)
    parser.add_argument('owner', type=int)
    parser.add_argument('sha256')
    args = parser.parse_args()
    if os.geteuid() != 0 or args.uid <= 0 or args.owner <= 1 or not re.fullmatch('[0-9a-f]{64}', args.sha256):
        parser.error('Explicit local administrator authorization and a frozen binary hash are required')
    lease = args.lease.resolve(strict=True)
    st = lease.stat()
    if st.st_uid != args.uid or st.st_mode & 0o077 or not st.st_size:
        raise ValueError('Invalid caller-owned test lease')
    expected_lease = lease.read_bytes()
    owner_uid = int(subprocess.check_output(['/bin/ps', '-p', str(args.owner), '-o', 'uid='], text=True).strip())
    if owner_uid != args.uid:
        raise ValueError('Test owner changed')
    cfg_data = args.config.read_bytes()
    if len(cfg_data) > 1024 * 1024:
        raise ValueError('Oversized fixture configuration')
    cfg = validate_config(json.loads(cfg_data))
    payload = args.binary.read_bytes()
    if hashlib.sha256(payload).hexdigest() != args.sha256:
        raise ValueError('Candidate core bytes changed')
    job = Path(tempfile.mkdtemp(prefix='goconnect-child-root-', dir='/private/tmp'))
    child = None
    try:
        binary = job / 'mihomo'
        binary.write_bytes(payload)
        binary.chmod(0o700)
        subprocess.run(['/usr/bin/codesign', '--verify', '--strict', str(binary)], check=True)
        identity = subprocess.run(['/usr/bin/codesign', '-d', '--verbose=4', str(binary)], capture_output=True, text=True, check=True)
        if 'TeamIdentifier=' + TEAM not in identity.stderr:
            raise ValueError('Unexpected candidate signing team')
        config = job / 'config.json'
        config.write_text(json.dumps(cfg))
        config.chmod(0o600)
        child = subprocess.Popen([str(binary), '-d', str(job), '-f', str(config)],
                                 env={'PATH': '/usr/bin:/bin:/usr/sbin:/sbin', 'HOME': str(job),
                                      'TMPDIR': str(job), 'GOMAXPROCS': '2'})
        deadline = time.monotonic() + 120
        while child.poll() is None and time.monotonic() < deadline:
            try:
                os.kill(args.owner, 0)
                current = lease.stat()
                if current.st_ino != st.st_ino or current.st_uid != args.uid or lease.read_bytes() != expected_lease:
                    break
            except OSError:
                break
            time.sleep(0.1)
    finally:
        if child is not None and child.poll() is None:
            child.terminate()
            try:
                child.wait(timeout=3)
            except subprocess.TimeoutExpired:
                child.kill()
                child.wait()
        shutil.rmtree(job)

if __name__ == '__main__':
    main()
