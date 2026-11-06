import importlib.util
import http.client
import http.server
import io
import json
from pathlib import Path
import tempfile
import threading
import unittest
import urllib.error
from unittest.mock import patch, Mock

spec = importlib.util.spec_from_file_location("relay", Path(__file__).with_name("yunyoo_subscription_service.py"))
relay = importlib.util.module_from_spec(spec)
spec.loader.exec_module(relay)


class RelayTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory(prefix="quota-relay-tests-")
        self.addCleanup(self.directory.cleanup)
        self.state = Path(self.directory.name)
        self.state_patch = patch.object(relay, "STATE", self.state)
        self.state_patch.start(); self.addCleanup(self.state_patch.stop)
        self.config = {"server": "192.0.2.10", "service_id": "123456", "read_token": "synthetic-read-token", "write_token": "synthetic-write-token", "public_url": "https://example.invalid", "proxies": [{"name": "test-node", "type": "hysteria2"}]}

    def response(self, used=7.4, status=200):
        raw = json.dumps({"status": status, "data": {"host_data": {"bwlimit": 1024, "bwusage": used, "dedicatedip": self.config["server"], "password": "must-never-be-persisted"}}}).encode()
        result = Mock()
        result.__enter__ = Mock(return_value=io.BytesIO(raw))
        result.__exit__ = Mock(return_value=False)
        return result

    def test_provider_mapping_and_password_is_discarded(self):
        quota = relay.quota_fields({"bwlimit": 1024, "bwusage": 3.63, "dedicatedip": "192.0.2.10", "password": "do-not-copy"}, "192.0.2.10", "123456", 100)
        self.assertNotIn("password", quota)
        header = relay.usage_header(quota, "auth_required")
        self.assertIn("upload=0", header)
        self.assertIn("stale=1", header)
        self.assertIn("updated=100", header)
        self.assertEqual(quota["totalGB"], 1024)

    def test_bad_quota_and_wrong_product_rejected(self):
        for used in [True, float("nan"), -1, float("inf")]:
            with self.assertRaises((ValueError, TypeError)):
                relay.quota_fields({"bwlimit": 1024, "bwusage": used, "dedicatedip": "192.0.2.10"}, "192.0.2.10", "123456")
        with self.assertRaises(ValueError):
            relay.quota_fields({"bwlimit": 1024, "bwusage": 1, "dedicatedip": "192.0.2.1"}, "192.0.2.10", "123456")

    def test_private_atomic_storage(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "session.json"
            relay.atomic_json(path, {"cookie": "synthetic-only"})
            self.assertEqual(path.stat().st_mode & 0o777, 0o600)
            self.assertEqual(relay.read_json(path), {"cookie": "synthetic-only"})

    def test_subscription_header_uses_provider_panel_rounding(self):
        for amount, displayed in [(3.25, "3.3"), (3.15, "3.1")]:
            quota = {"usedGB": amount,"totalGB":1024,"updated":100}
            self.assertIn("download=" + str(int(relay.Decimal(displayed) * 1024 ** 3)),relay.usage_header(quota,"ok"))

    def test_provider_session_is_never_forwarded_on_redirect(self):
        self.assertIsNone(relay.NoRedirect().redirect_request(None,None,302,"",{},"https://other.invalid/"))

    def test_saved_usage_does_not_expire_just_because_time_passed(self):
        header = relay.usage_header({"usedGB": 4.8, "totalGB": 1024, "updated": 1}, "ok")
        self.assertIn("stale=0", header)
        self.assertIn("auth_required=0", header)

    def test_expired_session_preserves_quota_and_marks_reauthorization(self):
        relay.atomic_json(self.state / "session.json", {"cookie": "synthetic-old", "user_agent": "test"})
        cached = relay.quota_fields({"bwlimit": 1024, "bwusage": 4.8, "dedicatedip": self.config["server"]}, self.config["server"], self.config["service_id"], 1)
        relay.atomic_json(self.state / "quota.json", cached)
        opener = Mock()
        opener.open.side_effect = urllib.error.HTTPError("https://yunyoo.cc/", 401, "expired", {}, None)
        with patch.object(relay.urllib.request, "build_opener", return_value=opener):
            self.assertEqual(relay.collect(self.config), "auth_required")
        self.assertEqual(relay.read_json(self.state / "quota.json"), cached)
        self.assertIn("auth_required=1", relay.usage_header(cached, "auth_required"))

    def test_manual_refresh_and_authorization_update_http_payload_immediately(self):
        class PlainHandler(relay.Handler):
            def setup(self):
                http.server.BaseHTTPRequestHandler.setup(self)
        PlainHandler.config = self.config
        server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), PlainHandler)
        thread = threading.Thread(target=server.serve_forever, daemon=True); thread.start()
        self.addCleanup(server.server_close); self.addCleanup(server.shutdown)
        relay.atomic_json(self.state / "session.json", {"cookie": "synthetic-old", "user_agent": "test"})
        cached = relay.quota_fields({"bwlimit": 1024, "bwusage": 4.8, "dedicatedip": self.config["server"]}, self.config["server"], self.config["service_id"], 1)
        relay.atomic_json(self.state / "quota.json", cached)
        opener = Mock(); opener.open.side_effect = [self.response(status=401), self.response(used=7.4)]
        def request(method, path, body=None, authorized=False):
            conn = http.client.HTTPConnection(*server.server_address, timeout=4)
            headers = {"Content-Type": "application/json"}
            if authorized: headers["Authorization"] = "Bearer " + self.config["write_token"]
            conn.request(method, path, body=body, headers=headers)
            response = conn.getresponse(); result = (response.status, dict(response.getheaders()), response.read()); conn.close()
            return result
        with patch.object(relay.urllib.request, "build_opener", return_value=opener):
            self.assertEqual(opener.open.call_count, 0)
            status, headers, body = request("GET", "/s/" + self.config["read_token"])
            self.assertEqual(status, 200)
            self.assertIn("auth_required=1", headers["subscription-userinfo"])
            self.assertEqual(relay.read_json(self.state / "quota.json"), cached)
            self.assertNotIn("profile-update-interval", headers)
            payload = json.dumps({"cookie": "synthetic-new", "user_agent": "test"})
            status, _, body = request("POST", "/admin/session", payload, authorized=True)
            self.assertEqual(status, 202)
            self.assertEqual(json.loads(body), {"accepted": True, "status": "ok"})
            self.assertEqual(opener.open.call_count, 2)
            self.assertEqual(relay.read_json(self.state / "quota.json")["usedGB"], 7.4)
            status, headers, body = request("GET", "/s/" + self.config["read_token"])
            self.assertIn("stale=0", headers["subscription-userinfo"])
            self.assertIn("auth_required=0", headers["subscription-userinfo"])
            self.assertIn("download=" + str(int(relay.Decimal("7.4") * 1024**3)), headers["subscription-userinfo"])
            self.assertEqual(opener.open.call_count, 2)  # authorization's fresh result is reused
            for file in ["quota.json", "health.json"]:
                self.assertNotIn("must-never-be-persisted", (self.state / file).read_text())
            self.assertNotIn("synthetic-new", body.decode())

    def test_inflight_old_authorization_cannot_overwrite_a_new_session(self):
        relay.atomic_json(self.state / "session.json", {"cookie": "synthetic-old", "user_agent": "test"})
        entered, resume = threading.Event(), threading.Event()
        opener = Mock()
        def fetch(*args, **kwargs):
            entered.set()
            self.assertTrue(resume.wait(3))
            return self.response(used=99)
        opener.open.side_effect = fetch
        values = []
        with patch.object(relay.urllib.request, "build_opener", return_value=opener):
            worker = threading.Thread(target=lambda: values.append(relay.collect(self.config)))
            worker.start()
            self.assertTrue(entered.wait(3))
            try:
                with relay.LOCK:
                    relay.atomic_json(self.state / "session.json", {"cookie": "synthetic-new", "user_agent": "test"})
                    relay.atomic_json(self.state / "health.json", {"status": "refreshing", "checked": 1})
            finally:
                resume.set(); worker.join(3)
        self.assertFalse(worker.is_alive())
        self.assertEqual(values, ["refreshing"])
        self.assertIsNone(relay.read_json(self.state / "quota.json"))
        self.assertEqual(relay.read_json(self.state / "health.json")["status"], "refreshing")


if __name__ == "__main__":
    unittest.main()
