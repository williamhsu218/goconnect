#!/usr/bin/env python3
"""Private subscription/usage relay. Never log URLs, tokens or provider bodies."""
import hashlib
import hmac
import http.server
import json
import math
import os
from pathlib import Path
import ssl
import threading
import time
import urllib.request
import urllib.error
from decimal import Decimal, ROUND_HALF_UP

STATE = Path(os.environ.get("SUBSCRIPTION_STATE", "/var/lib/vps-subscription"))
CONFIG = Path(os.environ.get("SUBSCRIPTION_CONFIG", "/etc/vps-subscription/config.json"))
LOCK = threading.RLock()
REFRESH_LOCK = threading.Lock()


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, request, fp, code, msg, headers, newurl):
        return None


def atomic_json(path, value):
    tmp = path.with_suffix(".tmp")
    fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(value, f, separators=(",", ":"))
    os.replace(tmp, path)


def read_json(path, default=None):
    try:
        return json.loads(path.read_text())
    except (OSError, ValueError):
        return default


def quota_fields(host, server, service_id, now=None):
    total, used = host.get("bwlimit"), host.get("bwusage")
    if isinstance(total, bool) or isinstance(used, bool):
        raise ValueError("invalid billing amounts")
    total, used = float(total), float(used)
    if not (math.isfinite(total) and math.isfinite(used) and 0 < total <= 1e9 and 0 <= used <= 1e9):
        raise ValueError("invalid billing amounts")
    if host.get("dedicatedip") != server:
        raise ValueError("wrong provider product")
    return {"totalGB": total, "usedGB": used, "updated": int(now or time.time()),
            "serviceID": service_id, "server": server, "status": "ok"}


def collect(config, *, force=False, wait=2):
    """One bounded refresh, called only by an explicit HTTP request."""
    if not REFRESH_LOCK.acquire(timeout=wait):
        return "refreshing"
    try:
        with LOCK:
            session = read_json(STATE / "session.json")
            quota = read_json(STATE / "quota.json")
            health = read_json(STATE / "health.json", {})
        # Reuse the result of authorization when the client immediately reloads
        # the subscription. This is request coalescing, not scheduled polling.
        if not force and session and quota and health.get("status") == "ok" and 0 <= time.time() - quota["updated"] < 2:
            return "ok"
        state, value = "auth_required", None
        if session:
            try:
                request = urllib.request.Request(
                    "https://yunyoo.cc/host/dedicatedserver?host_id=" + config["service_id"],
                    headers={"Cookie": session["cookie"], "User-Agent": session["user_agent"], "Accept": "application/json"})
                with urllib.request.build_opener(NoRedirect()).open(request, timeout=18) as response:
                    raw = response.read(2_097_153)
                if len(raw) > 2_097_152:
                    raise ValueError("response too large")
                if raw.lstrip().startswith(b"<"):
                    state = "auth_required"
                else:
                    data = json.loads(raw)
                    if data.get("status") in (401, 403, 405):
                        state = "auth_required"
                    elif data.get("status") != 200:
                        state = "unavailable"
                    else:
                        value = quota_fields(data["data"]["host_data"], config["server"], config["service_id"])
                        state = "ok"
            except urllib.error.HTTPError as error:
                state = "auth_required" if error.code in (301, 302, 303, 307, 308, 401, 403) else "unavailable"
            except Exception:
                state = "unavailable"
        with LOCK:
            # A result obtained with an old session must not overwrite a newer
            # authorization while its request is waiting for this refresh.
            if read_json(STATE / "session.json") != session:
                return "refreshing"
            if value is not None:
                atomic_json(STATE / "quota.json", value)
            atomic_json(STATE / "health.json", {"status": state, "checked": int(time.time())})
        return state
    finally:
        REFRESH_LOCK.release()


def usage_header(quota, status):
    # subscription-userinfo counts bytes; bwusage is the provider's aggregate
    # billed amount, so encode it once, not as both upload and download.
    panel_used = Decimal.from_float(float(quota["usedGB"])).quantize(Decimal("0.1"), rounding=ROUND_HALF_UP)
    used = int(panel_used * 1024 ** 3)
    total = int(quota["totalGB"] * 1024 ** 3)
    stale = status != "ok"
    return f"upload=0; download={used}; total={total}; updated={quota['updated']}; stale={int(stale)}; auth_required={int(status in ('auth_required', 'not_authorized'))}"


class Handler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    server_version = "PrivateSubscription"
    config = {}

    def setup(self):
        self.request.settimeout(10)
        self.request = self.server.tls_context.wrap_socket(self.request, server_side=True)
        super().setup()

    def log_message(self, format, *args):
        pass

    def reply(self, status, body, content_type="application/json", extra=None):
        self.send_response(status)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "private, no-store")
        self.send_header("Referrer-Policy", "no-referrer")
        self.send_header("X-Content-Type-Options", "nosniff")
        for key, value in (extra or {}).items():
            self.send_header(key, value)
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        kind, _, token = self.path.lstrip("/").partition("/")
        if kind not in ("s", "q") or not hmac.compare_digest(token, self.config["read_token"]):
            self.reply(404, b"{}")
            return
        status = collect(self.config)
        with LOCK:
            quota = read_json(STATE / "quota.json")
        if kind == "q":
            value = dict(quota or {})
            value["status"] = status
            value["stale"] = not quota or status != "ok"
            self.reply(200, json.dumps(value).encode())
            return
        extra = {"content-disposition": "attachment; filename=Tokyo-HY2-private.yaml"}
        if quota:
            extra["subscription-userinfo"] = usage_header(quota, status)
        value = {"x-goconnect": {"subscription-url": self.config["public_url"] + "/s/" + self.config["read_token"]},
                 "proxies": self.config["proxies"],
                 "proxy-groups": [{"name": "PROXY", "type": "select", "proxies": [x["name"] for x in self.config["proxies"]]}],
                 "rules": ["MATCH,PROXY"]}
        self.reply(200, json.dumps(value, ensure_ascii=False, indent=2).encode(), "application/yaml; charset=utf-8", extra)

    def do_POST(self):
        authorized = hmac.compare_digest(self.headers.get("Authorization", ""), "Bearer " + self.config["write_token"])
        if self.path != "/admin/session" or not authorized:
            try:
                length = int(self.headers.get("Content-Length", "0"))
                if 0 < length <= 32768:
                    self.rfile.read(length)
            except (OSError, ValueError):
                pass
            self.reply(404, b"{}")
            self.close_connection = True
            return
        try:
            length = int(self.headers.get("Content-Length", "0"))
            if not 0 < length <= 32768:
                raise ValueError("invalid length")
            data = json.loads(self.rfile.read(length))
            cookie, agent = data["cookie"], data["user_agent"]
            if not isinstance(cookie, str) or not isinstance(agent, str) or not cookie or any(c in cookie + agent for c in "\r\n\x00"):
                raise ValueError("invalid session")
            if len(cookie) > 24576 or not 0 < len(agent) <= 1024:
                raise ValueError("invalid session")
            with LOCK:
                atomic_json(STATE / "session.json", {"cookie": cookie, "user_agent": agent})
                atomic_json(STATE / "health.json", {"status": "refreshing", "checked": int(time.time())})
            status = collect(self.config, force=True, wait=20)
            self.reply(202, json.dumps({"accepted": True, "status": status}).encode())
        except Exception:
            self.reply(400, b'{"accepted":false}')


class BoundedHTTPServer(http.server.ThreadingHTTPServer):
    daemon_threads = True
    request_queue_size = 16
    slots = threading.BoundedSemaphore(8)

    def process_request(self, request, address):
        if not self.slots.acquire(blocking=False):
            request.close()
            return
        try:
            super().process_request(request, address)
        except Exception:
            self.slots.release()
            raise

    def process_request_thread(self, request, address):
        try:
            super().process_request_thread(request, address)
        finally:
            self.slots.release()

    def handle_error(self, request, address):
        pass


def main():
    config = json.loads(CONFIG.read_text())
    STATE.mkdir(mode=0o700, parents=True, exist_ok=True)
    Handler.config = config
    httpd = BoundedHTTPServer(("0.0.0.0", config.get("port", 8443)), Handler)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_2
    context.load_cert_chain(config["certificate"], config["private_key"])
    httpd.tls_context = context
    httpd.serve_forever()


if __name__ == "__main__":
    main()
