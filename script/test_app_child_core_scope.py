import unittest
from run_app_child_test_core import validate_config

class IsolatedRootCoreScopeTests(unittest.TestCase):
    def fixture(self):
        return {'tun': {'enable': False}, 'proxies': [{'name': 'GoConnect', 'type': 'socks5', 'server': '127.0.0.1', 'port': 23456}],
                'dns': {'listen': '127.0.0.1:23457', 'nameserver': ['tcp://127.0.0.1:23458']},
                'external-controller': '127.0.0.1:23459', 'secret': 'fixture', 'rules': ['MATCH,GoConnect']}
    def test_loopback_only_and_no_mutation_api(self):
        cfg = self.fixture(); safe = validate_config(cfg)
        self.assertNotIn('external-controller', safe)
        self.assertNotIn('secret', safe)
        self.assertFalse(safe['tun']['enable'])
        self.assertEqual(safe['bind-address'], '127.0.0.1')
        self.assertIn('external-controller', cfg)
    def test_tun_and_real_egress_rejected(self):
        for field in ['tun', 'proxy', 'dns', 'transparent']:
            cfg = self.fixture()
            if field == 'tun': cfg['tun']['enable'] = True
            if field == 'proxy': cfg['proxies'][0]['server'] = '192.0.2.1'
            if field == 'dns': cfg['dns']['nameserver'] = ['https://example.com/dns-query']
            if field == 'transparent': cfg['redir-port'] = 23460
            with self.subTest(field=field), self.assertRaises(ValueError): validate_config(cfg)

if __name__ == '__main__': unittest.main()
