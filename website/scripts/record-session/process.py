"""Turn a tracer JSONL file and the server's log into app/components/session/recording.json.

Usage: python3 process.py TRACE.jsonl SERVER.log OUT.json --version 7.0.0 --date 2026-11-15 --command '...'
"""
import argparse
import json

ap = argparse.ArgumentParser()
ap.add_argument("trace")
ap.add_argument("server_log")
ap.add_argument("out")
ap.add_argument("--version", required=True, help="ffmpeg-over-ip release used for the recording")
ap.add_argument("--date", required=True, help="recording date, YYYY-MM-DD")
ap.add_argument("--command", required=True, help="the command line exactly as typed in the client container")
a = ap.parse_args()
src, server_log, out = a.trace, a.server_log, a.out
rs = [json.loads(l) for l in open(src)]
rs = [r for r in rs if r['dir'] != 'meta']
replies = {}
for r in rs:
    if r['dir'] == 'c2s' and 'req' in r and (r['type'].endswith('_OK') or r['type'] == 'IO_ERROR'):
        replies[r['req']] = r
events = []
for r in rs:
    if r['dir'] == 'c2s' and r['type'].endswith('_OK') or r['type'] == 'IO_ERROR':
        continue
    e = {'t': round(r['t'], 3), 'dir': r['dir'], 'type': r['type']}
    for k in ('path', 'n', 'offset', 'whence', 'fid', 'flags', 'code', 'text', 'version', 'args', 'program'):
        if k in r:
            e[k] = r[k]
    if 'req' in r and r['req'] in replies:
        p = replies[r['req']]
        e['reply'] = {k: (round(v, 3) if k == 't' else v) for k, v in p.items() if k in ('t', 'type', 'n', 'size', 'offset', 'mode', 'errno')}
    events.append(e)
log = [l.rstrip('\n') for l in open(server_log) if l.strip()]
accepted = next(e['t'] for e in events if e['type'] == 'ACCEPTED')
exit_t = next(e['t'] for e in events if e['type'] == 'EXIT_CODE')
server = []
for l in log:
    if 'listening on' in l: server.append({'t': None, 'text': l})
    elif 'running ' in l: server.append({'t': accepted, 'text': l})
    elif 'exited' in l: server.append({'t': exit_t, 'text': l})
data = {
    'recordedOn': a.date,
    'version': a.version,
    'command': a.command,
    'duration': round(rs[-1]['t'], 3),
    'serverLog': server,
    'events': events,
}
json.dump(data, open(out, 'w'), separators=(',', ':'))
print(len(events), 'events;', len(json.dumps(data)), 'bytes')
