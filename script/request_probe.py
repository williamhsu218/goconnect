#!/usr/bin/env python3
"""Read-only request diagnostics. Never changes routes or signals VPN processes.

The caller supplies a new output directory outside the repository. Session
credentials stay in memory; only hashes, timings and public endpoint metadata
are written. STUN late responses are discarded within the original deadline.
"""
import argparse
import ctypes
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import ssl
import struct
import subprocess
import threading
import time


def receive_stun(sock, transaction, deadline, decode=lambda data: data):
    ignored = 0
    while True:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            raise TimeoutError("STUN deadline")
        sock.settimeout(remaining)
        data = decode(sock.recv(65535))
        if time.monotonic() >= deadline:
            raise TimeoutError("STUN response arrived after deadline")
        # Validate the full header, declared length and transaction. An old
        # datagram must not poison every subsequent request on this socket.
        if (len(data) >= 20 and data[:2] == b"\x01\x01"
                and data[4:8] == b"\x21\x12\xa4\x42"
                and len(data) == 20 + int.from_bytes(data[2:4], "big")
                and data[8:20] == transaction):
            return data, ignored
        ignored += 1


def exact(sock, n):
    result = b""
    while len(result) < n:
        chunk = sock.recv(n - len(result))
        if not chunk:
            raise EOFError("SOCKS reply ended")
        result += chunk
    return result


def socks_open(session, command, host="0.0.0.0", port=0):
    sock = socket.create_connection(("127.0.0.1", session["socksPort"]), 20)
    try:
        sock.sendall(b"\x05\x01\x02")
        if exact(sock, 2) != b"\x05\x02":
            raise OSError("SOCKS auth unavailable")
        token = session["token"].encode()
        sock.sendall(b"\x01\x09goconnect" + bytes([len(token)]) + token)
        if exact(sock, 2) != b"\x01\x00":
            raise OSError("SOCKS auth failed")
        sock.sendall(bytes([5, command, 0, 1]) + socket.inet_aton(host) + struct.pack("!H", port))
        header = exact(sock, 4)
        if header != b"\x05\x00\x00\x01":
            raise OSError("SOCKS IPv4 request failed")
        address = socket.inet_ntoa(exact(sock, 4))
        return sock, (address, struct.unpack("!H", exact(sock, 2))[0])
    except BaseException:
        sock.close()
        raise


def unwrap_udp(data):
    # This diagnostic sends only IPv4 requests; reject unsupported framing.
    return data[10:] if len(data) >= 10 and data[:4] == b"\0\0\0\x01" else b""


def stun_transaction(marked=False):
    if marked:
        ident = (int.from_bytes(os.urandom(8), "big") & ((1 << 53) - 1)) or 1
        return b"GCD1" + ident.to_bytes(8, "big")
    return os.urandom(12)


def manifest(root):
    return {str(p.relative_to(root)): hashlib.sha256(p.read_bytes()).hexdigest()
            for p in sorted(root.rglob("*")) if p.is_file()}


def active_session():
    found = []
    for p in Path(os.environ.get("TMPDIR", "/tmp")).glob("GoConnect-*/status"):
        try:
            status = json.loads(p.read_text())
            session = json.loads((p.parent / "session.json").read_text())
            os.kill(session["ownerPID"], 0)  # existence check, not a signal
            if status.get("state") == "ready" and status.get("vpnObserved"):
                found.append((p.parent, session))
        except (OSError, ValueError, KeyError):
            continue
    if len(found) != 1:
        raise RuntimeError("Expected exactly one active GoConnect session")
    return found[0]


def cpu_ticks():
    lib = ctypes.CDLL("/usr/lib/libSystem.B.dylib")
    lib.mach_host_self.restype = ctypes.c_uint
    ticks = (ctypes.c_uint * 4)()
    count = ctypes.c_uint(4)
    if lib.host_statistics(lib.mach_host_self(), 3, ticks, ctypes.byref(count)):
        raise OSError("host_statistics")
    return list(ticks)


def run(args):
    root = Path(__file__).resolve().parent.parent
    output = Path(args.output).resolve()
    if output == root or root in output.parents:
        raise ValueError("Evidence must be outside the repository")
    output.mkdir(mode=0o700, parents=True, exist_ok=False)
    os.umask(0o077)
    directory, session = active_session()
    userdata = Path.home() / "Library/Application Support/com.willhsu.GoConnect"
    app = Path("/Applications/GoConnect.app")
    initial = dict(userdata=manifest(userdata), app=manifest(app))
    (output / "before.json").write_text(json.dumps(initial, indent=2))
    stop = threading.Event()
    lock = threading.Lock()
    start = time.monotonic()
    events = (output / "samples.jsonl").open("w")
    in_flight = {}
    ifindex = socket.if_nametoindex(args.interface)
    stun_ip = socket.gethostbyname(args.stun_host)
    direct_ip = socket.gethostbyname(args.direct_https_host)
    context = ssl.create_default_context()

    def emit(kind, **data):
        with lock:
            events.write(json.dumps(dict(t=round(time.monotonic() - start, 6),
                                         wall=time.time(), kind=kind, **data)) + "\n")
            events.flush()

    def bound_socket(kind, path):
        sock = socket.socket(socket.AF_INET, kind)
        if path == "direct":
            # Darwin SDK netinet/in.h: IP_BOUND_IF=25; affects this socket only.
            sock.setsockopt(socket.IPPROTO_IP, 25, ifindex)
            if sock.getsockopt(socket.IPPROTO_IP, 25) != ifindex:
                sock.close()
                raise OSError("interface binding did not stick")
        return sock

    def https(path):
        conn = None
        generation = 0
        host = args.direct_https_host if path == "direct" else "1.1.1.1"
        destination = direct_ip if path == "direct" else "1.1.1.1"
        while not stop.is_set():
            tick = time.monotonic()
            stage = "tcp_connect"
            times = {}
            raw = None
            in_flight[path + "/https"] = tick
            try:
                if conn is None:
                    if path == "socks":
                        raw, _ = socks_open(session, 1, "1.1.1.1", 443)
                    else:
                        raw = bound_socket(socket.SOCK_STREAM, path)
                        raw.settimeout(20)
                        raw.connect((destination, 443))
                    times[stage] = time.monotonic() - tick
                    stage = "tls_handshake"
                    raw.settimeout(max(.001, 20 - (time.monotonic() - tick)))
                    tls = context.wrap_socket(raw, server_hostname=host)
                    raw = None
                    conn = http.client.HTTPConnection(host, timeout=20)
                    conn.sock = tls
                    generation += 1
                    times[stage] = time.monotonic() - tick
                local_port = conn.sock.getsockname()[1]
                for stage in ("send", "headers", "body"):
                    if conn.sock is not None:
                        conn.sock.settimeout(max(.001, 20 - (time.monotonic() - tick)))
                    if stage == "send":
                        conn.request("HEAD" if path == "direct" else "GET",
                                     "/" if path == "direct" else "/cdn-cgi/trace",
                                     headers={"Connection": "keep-alive", "User-Agent": "GoConnect-readonly-probe",
                                              "Host": host if path == "direct" else "one.one.one.one"})
                    elif stage == "headers":
                        response = conn.getresponse()
                    else:
                        body = response.read(65536)
                    times[stage] = time.monotonic() - tick
                    if times[stage] >= 20:
                        raise TimeoutError("HTTPS request deadline")
                trace = dict(line.split("=", 1) for line in body.decode(errors="replace").splitlines() if "=" in line)
                emit("https", path=path, ok=response.status == 200, status=response.status,
                     generation=generation, localPort=local_port, phases=times,
                     elapsed=time.monotonic() - tick,
                     egressHash=hashlib.sha256(trace.get("ip", "").encode()).hexdigest())
                if response.will_close:
                    conn.close()
                    conn = None
            except Exception as ex:
                emit("https", path=path, ok=False, generation=generation, stage=stage,
                     error=type(ex).__name__, errno=getattr(ex, "errno", None),
                     elapsed=time.monotonic() - tick, phases=times)
                if raw:
                    raw.close()
                if conn:
                    conn.close()
                conn = None
            finally:
                in_flight.pop(path + "/https", None)
            stop.wait(max(0, 2 - (time.monotonic() - tick)))
        if conn:
            conn.close()

    def udp(path):
        control = None
        sock = bound_socket(socket.SOCK_DGRAM, path)
        try:
            prefix = b""
            if path == "socks":
                control, target = socks_open(session, 3)
                prefix = b"\0\0\0\x01" + socket.inet_aton(stun_ip) + struct.pack("!H", args.stun_port)
            else:
                target = (stun_ip, args.stun_port)
            sock.connect(target)
            while not stop.is_set():
                tick = time.monotonic()
                in_flight[path + "/udp"] = tick
                tx = stun_transaction(args.bridge_diagnostics)
                trace = {"traceID": int.from_bytes(tx[4:], "big")} if args.bridge_diagnostics else {}
                try:
                    sock.settimeout(15)
                    sock.send(prefix + b"\0\x01\0\0\x21\x12\xa4\x42" + tx)
                    data, ignored = receive_stun(sock, tx, tick + 15,
                                                unwrap_udp if prefix else lambda data: data)
                    emit("udp", path=path, ok=True, elapsed=time.monotonic() - tick,
                         localPort=sock.getsockname()[1], ignored=ignored, bytes=len(data), **trace)
                except Exception as ex:
                    emit("udp", path=path, ok=False, elapsed=time.monotonic() - tick,
                         error=type(ex).__name__, errno=getattr(ex, "errno", None),
                         localPort=sock.getsockname()[1], **trace)
                finally:
                    in_flight.pop(path + "/udp", None)
                stop.wait(max(0, 5 - (time.monotonic() - tick)))
        except Exception as ex:
            emit("probe_setup_error", path=path, protocol="udp", error=type(ex).__name__)
        finally:
            sock.close()
            if control:
                control.close()

    def monitor():
        previous = cpu_ticks()
        while not stop.wait(2):
            tick = time.monotonic()
            try:
                state = json.loads((directory / "status").read_text())
                current = cpu_ticks()
                delta = [(a - b) % (2**32) for a, b in zip(current, previous)]
                previous = current
                processes = subprocess.run(["ps", "-axo", "pid=,ppid=,%cpu=,state=,comm="],
                                           capture_output=True, text=True, timeout=5)
                rows = []
                for line in processes.stdout.splitlines():
                    fields = line.split(None, 4)
                    if len(fields) == 5 and Path(fields[4]).name.lower() in ("goconnect", "goconnecttransport", "openconnect", "mihomo", "ffmpeg"):
                        rows.append(dict(pid=int(fields[0]), ppid=int(fields[1]), cpu=float(fields[2]), state=fields[3], name=Path(fields[4]).name))
                emit("system", cpuBusy=100 * (1 - delta[2] / max(1, sum(delta))),
                     load=os.getloadavg(), processes=rows, state=state.get("state"),
                     device=state.get("systemDevice"), vpnObserved=state.get("vpnObserved"),
                     leaseAge=time.time() - (directory / "lease").stat().st_mtime,
                     elapsed=time.monotonic() - tick,
                     pending={k:time.monotonic()-v for k,v in list(in_flight.items())})
            except Exception as ex:
                emit("monitor_error", error=type(ex).__name__)

    def scheduler():
        while not stop.is_set():
            tick = time.monotonic()
            if stop.wait(.1):
                break
            lag = time.monotonic() - tick - .1
            if lag >= .01:
                emit("scheduler_lag", seconds=lag)

    emit("start", seconds=args.seconds, pid=os.getpid(), ownerPID=session["ownerPID"],
         sessionHash=hashlib.sha256(directory.name.encode()).hexdigest(), interface=args.interface,
         socksPort=session["socksPort"], stunEndpoint=stun_ip, stunPort=args.stun_port,
         bridgeDiagnostics=args.bridge_diagnostics,
         directHTTPSEndpoint=args.direct_https_host,
         note="Direct HTTPS is a different reachable origin; UDP uses the same endpoint on all paths.")
    threads = [threading.Thread(target=fn, args=(path,)) for path in ("transparent", "socks", "direct") for fn in (https, udp)]
    threads += [threading.Thread(target=fn) for fn in (monitor, scheduler)]
    for thread in threads:
        thread.start()
    try:
        stop.wait(args.seconds)
    finally:
        stop.set()
        for thread in threads:
            thread.join(timeout=45)
        if any(thread.is_alive() for thread in threads):
            raise RuntimeError("probe failed to finish")
        after = dict(userdata=manifest(userdata), app=manifest(app))
        (output / "after.json").write_text(json.dumps(after, indent=2))
        emit("complete", userdataUnchanged=initial["userdata"] == after["userdata"],
             installedUnchanged=initial["app"] == after["app"])
        events.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", required=True)
    parser.add_argument("--interface", required=True, help="Physical interface for socket-only direct control")
    parser.add_argument("--seconds", type=int, default=600)
    parser.add_argument("--direct-https-host", default="www.apple.com")
    parser.add_argument("--stun-host", default="stun.cloudflare.com")
    parser.add_argument("--stun-port", type=int, default=3478)
    parser.add_argument("--bridge-diagnostics", action="store_true", help="Mark only test STUN transactions for bounded bridge tracing")
    run(parser.parse_args())
