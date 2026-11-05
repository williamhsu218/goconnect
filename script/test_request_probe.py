import socket
import threading
import time
import unittest

from request_probe import receive_stun, unwrap_udp, stun_transaction


def reply(tx):
    return b"\x01\x01\0\0\x21\x12\xa4\x42" + tx


class STUNDeadlineTests(unittest.TestCase):
    def test_diagnostic_transaction_is_opt_in_and_json_safe(self):
        plain = stun_transaction()
        self.assertEqual(len(plain), 12)
        marked = [stun_transaction(True) for _ in range(20)]
        self.assertEqual(len(set(marked)), 20)
        for tx in marked:
            self.assertEqual(tx[:4], b"GCD1")
            self.assertTrue(0 < int.from_bytes(tx[4:], "big") < 1 << 53)
    def setUp(self):
        self.a, self.b = socket.socketpair(type=socket.SOCK_DGRAM)
        self.addCleanup(self.a.close)
        self.addCleanup(self.b.close)

    def test_late_response_does_not_poison_next_request(self):
        old, current = b"o" * 12, b"n" * 12
        self.b.send(reply(old))
        self.b.send(reply(current))
        data, ignored = receive_stun(self.a, current, time.monotonic() + .3)
        self.assertEqual((data, ignored), (reply(current), 1))
        self.b.send(reply(old))
        self.assertEqual(receive_stun(self.a, old, time.monotonic() + .3)[1], 0)

    def test_mismatches_do_not_reset_deadline(self):
        done = threading.Event()
        def flood():
            while not done.wait(.005):
                self.b.send(reply(b"o" * 12))
        worker = threading.Thread(target=flood)
        worker.start()
        tick = time.monotonic()
        try:
            with self.assertRaises(TimeoutError):
                receive_stun(self.a, b"n" * 12, tick + .08)
            self.assertLess(time.monotonic() - tick, .5)
        finally:
            done.set()
            worker.join()

    def test_malformed_response_and_socks_framing(self):
        tx = b"n" * 12
        for data in (b"", reply(tx)[:19], reply(tx) + b"extra", b"\x01\x01\0\0bad!" + tx):
            self.b.send(data)
        self.b.send(reply(tx))
        self.assertEqual(receive_stun(self.a, tx, time.monotonic() + .3)[1], 4)
        self.assertEqual(unwrap_udp(b"\0\0\0\x01" + b"\0" * 6 + reply(tx)), reply(tx))
        self.assertEqual(unwrap_udp(b"\0\0\x01\x01" + b"\0" * 6 + reply(tx)), b"")

    def test_late_matching_response_does_not_bypass_deadline(self):
        tx = b"n" * 12
        class DelayedSocket:
            def settimeout(self, value):
                pass
            def recv(self, size):
                time.sleep(.02)
                return reply(tx)
        with self.assertRaises(TimeoutError):
            receive_stun(DelayedSocket(), tx, time.monotonic() + .005)


if __name__ == "__main__":
    unittest.main()
