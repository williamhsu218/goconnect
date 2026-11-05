#!/usr/bin/env python3
"""Correlate opt-in probe transactions with private bridge logs, read-only.

Missing events are not proof of packet loss when logs are incomplete or dropped.
No raw configuration, packet body, address, token or error string is exported.
"""
import argparse
from collections import Counter
import json
from pathlib import Path

STAGES = {'socks_receive', 'udp_dial', 'udp_queue', 'udp_write_start', 'udp_write',
          'pipe_write_start', 'pipe_write', 'pipe_read', 'stack_deliver',
          'udp_read', 'socks_send', 'status'}
OUTCOMES = {'ok', 'io_error', 'short_write', 'canceled', 'closed', 'timeout'}
ROUND_TRIP = STAGES - {'udp_dial', 'udp_queue', 'status'}


def read_rows(path):
    for line in Path(path).read_text().splitlines():
        try:
            yield json.loads(line)
        except ValueError:
            continue  # A truncated log must not become an invented observation.


def analyze(samples, logs):
    start = next(row for row in samples if row.get('kind') == 'start')
    observations = []
    for row in logs:
        if row.get('event') != 'bridge.trace' or row.get('pid') != start['ownerPID']:
            continue
        d = row.get('details', {})
        if d.get('stage') not in STAGES or d.get('outcome') not in OUTCOMES:
            continue
        keys = ('transaction', 'flow', 'packetKind', 'bytes', 'errno', 'timeMicros', 'sequence', 'dropped', 'pid')
        if not all(isinstance(d.get(k), int) and 0 <= d[k] < 1 << 53 for k in keys):
            continue
        observations.append({**{k: d[k] for k in keys}, 'stage': d['stage'], 'outcome': d['outcome'],
                             'appDropped': row.get('appDropped', 0)})
    observations.sort(key=lambda x: x['timeMicros'])
    bridge_pids = sorted({o['pid'] for o in observations})
    sequences = sorted({o['sequence'] for o in observations if o['stage'] != 'status'})
    intact = (len(bridge_pids) == 1 and sequences and sequences[0] == 1
              and sequences[-1] == len(sequences)
              and max(o['sequence'] for o in observations) == sequences[-1]
              and all(o['dropped'] == 0 and o['appDropped'] == 0 for o in observations))
    results = []
    for r in samples:
        if r.get('kind') != 'udp' or r.get('path') not in ('transparent', 'socks'):
            continue
        ident = r.get('traceID')
        found = [o for o in observations if ident is not None and o['transaction'] == ident]
        stages = {o['stage'] for o in found if o['outcome'] == 'ok'}
        failed = [o for o in found if o['outcome'] != 'ok']
        begin, end = r['wall'] - r['elapsed'], r['wall']
        covered = bool(intact and observations[0]['timeMicros'] / 1e6 <= begin
                       and observations[-1]['timeMicros'] / 1e6 >= end)
        if r['ok']:
            conclusion = 'round_trip_traced' if ROUND_TRIP <= stages else 'success_trace_incomplete'
        elif failed:
            conclusion = 'bridge_operation_failed'
        elif not covered:
            conclusion = 'insufficient_trace'
        elif 'socks_send' in stages:
            conclusion = ('stun_error_reply_delivered' if any(o['packetKind'] == 0x111 for o in found)
                          else 'after_socks_send_or_client_validation')
        elif 'udp_read' in stages:
            conclusion = 'between_udp_read_and_socks_send'
        elif 'pipe_read' in stages:
            conclusion = 'returned_to_bridge_without_udp_delivery'
        elif 'pipe_write' in stages:
            conclusion = 'after_bridge_packet_write_no_return_observed'
        elif 'udp_write' in stages:
            conclusion = 'between_udp_write_and_packet_write'
        elif 'socks_receive' in stages:
            conclusion = 'between_socks_receive_and_udp_write'
        else:
            conclusion = 'not_observed_at_bridge'
        results.append(dict(path=r['path'], t=r['t'], ok=r['ok'], traceID=ident,
                            conclusion=conclusion, completeLogWindow=covered,
                            stages=sorted(stages), errors=[{'stage': o['stage'], 'outcome': o['outcome'], 'errno': o['errno']} for o in failed]))
    return dict(bridgePIDs=bridge_pids, logSequenceIntact=bool(intact),
                classifications=dict(Counter(r['conclusion'] for r in results)), requests=results,
                boundary='A successful UNIX packet write proves handoff to the local channel, not delivery by OpenConnect or the remote gateway.')


if __name__ == '__main__':
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument('--samples', required=True)
    p.add_argument('--bridge-log', required=True, action='append')
    args = p.parse_args()
    print(json.dumps(analyze(list(read_rows(args.samples)),
                             [r for path in args.bridge_log for r in read_rows(path)]), indent=2))
