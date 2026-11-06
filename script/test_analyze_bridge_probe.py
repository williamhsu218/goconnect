import unittest
from analyze_bridge_probe import analyze


def observation(stage, sequence, when, **changes):
    return {'event': 'bridge.trace', 'pid': 101, 'appDropped': 0,
            'details': dict(stage=stage, outcome='ok', transaction=42, flow=1,
                            packetKind=1, bytes=20, errno=0, timeMicros=int(when*1e6),
                            sequence=sequence, dropped=0, pid=202, **changes)}


class ClassificationTests(unittest.TestCase):
    def setUp(self):
        self.samples = [dict(kind='start', ownerPID=101),
                        dict(kind='udp', path='socks', traceID=42, t=20, wall=120., elapsed=15., ok=False)]
        self.logs = [observation('socks_receive', 1, 104.99), observation('udp_write', 2, 105.01),
                     observation('pipe_write', 3, 105.02), observation('status', 3, 121.)]

    def result(self):
        return analyze(self.samples, self.logs)['requests'][0]

    def test_handoff_does_not_claim_remote_server_cause(self):
        self.assertEqual(self.result()['conclusion'], 'after_bridge_packet_write_no_return_observed')

    def test_missing_rotation_drops_and_incomplete_time_window_are_inconclusive(self):
        original = self.logs
        for logs in (original[1:], original[:-1], [*original[:2], original[-1]]):
            self.logs = logs
            self.assertEqual(self.result()['conclusion'], 'insufficient_trace')
        self.logs = original
        self.logs[-1]['details']['dropped'] = 1
        self.assertEqual(self.result()['conclusion'], 'insufficient_trace')
        self.logs[-1]['details']['dropped'] = 0
        self.logs[-1]['appDropped'] = 1
        self.assertEqual(self.result()['conclusion'], 'insufficient_trace')

    def test_explicit_failure_is_evidence_even_with_other_missing_logs(self):
        self.logs = [observation('udp_write', 8, 106.)]
        self.logs[0]['details'].update(outcome='io_error', errno=32)
        self.assertEqual(self.result()['conclusion'], 'bridge_operation_failed')

    def test_other_session_and_unknown_labels_are_not_evidence(self):
        for row in self.logs:
            row['pid'] = 999
        self.assertEqual(self.result()['conclusion'], 'insufficient_trace')
        self.logs = [observation('PRIVATE-SECRET', 1, 106.)]
        self.assertNotIn('PRIVATE-SECRET', str(analyze(self.samples, self.logs)))

    def test_queue_failure_is_localized_without_requiring_queue_on_success(self):
        self.logs = [observation('udp_queue', 1, 106.)]
        self.logs[0]['details'].update(outcome='io_error', errno=55)
        self.assertEqual(self.result()['conclusion'], 'bridge_operation_failed')
        self.assertEqual(self.result()['errors'][0]['stage'], 'udp_queue')
        stages = ['socks_receive', 'udp_write_start', 'udp_write', 'pipe_write_start',
                  'pipe_write', 'pipe_read', 'stack_deliver', 'udp_read', 'socks_send']
        self.logs = [observation(stage, i+1, 105.+i) for i, stage in enumerate(stages)]
        self.samples[-1]['ok'] = True
        self.assertEqual(self.result()['conclusion'], 'round_trip_traced')


if __name__ == '__main__':
    unittest.main()
