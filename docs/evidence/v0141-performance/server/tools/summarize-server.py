# python -I summarize-server.py <baseline dir>  -> raw/server-summary.json (+ prints a table)
import json, re, sys, os, statistics as st, glob
B = sys.argv[1]; R = os.path.join(B, 'raw', 'server')
procs = [json.loads(l) for l in open(os.path.join(R, 'proc.jsonl'))]
by = {p['label']: p for p in procs}
def hey(path):
    t = open(path, encoding='utf8').read()
    q = {m.group(1): float(m.group(2)) * 1000 for m in re.finditer(r'(\d+)%+ in ([\d.]+) secs', t)}
    codes = {m.group(1): int(m.group(2)) for m in re.finditer(r'\[(\d+)\]\s+(\d+) responses', t)}
    return dict(rps=float(re.search(r'Requests/sec:\s+([\d.]+)', t).group(1)), p50_ms=q.get('50'), p95_ms=q.get('95'), p99_ms=q.get('99'),
                slowest_ms=float(re.search(r'Slowest:\s+([\d.]+)', t).group(1)) * 1000, codes=codes, requests=sum(codes.values()))
paths = []
for p in procs:
    m = re.match(r'pre r1 (.*)$', p['label'])
    if m: paths.append(m.group(1))
rounds = sorted({int(m.group(1)) for p in procs for m in [re.match(r'pre r(\d+) ', p['label'])] if m})
out = {}
for path in paths:
    slug = re.sub(r'[/?=&]', '_', path).lstrip('_') or 'root'
    rs = []
    for r in rounds:
        h = hey(os.path.join(R, f'hey-r{r}-{slug}.txt'))
        a, b = by[f'pre r{r} {path}'], by[f'post r{r} {path}']
        cpu = (b['utime_s'] + b['stime_s']) - (a['utime_s'] + a['stime_s'])
        h.update(cpu_s=round(cpu, 2), cpu_ms_per_1k_req=round(cpu * 1e6 / h['requests'], 1), rss_after_kb=b['vmrss_kb'])
        rs.append(h)
    med = lambda k: round(st.median(x[k] for x in rs), 2)
    out[path] = dict(rounds=rs, median={k: med(k) for k in ['rps', 'p50_ms', 'p95_ms', 'p99_ms', 'cpu_s', 'cpu_ms_per_1k_req']},
                     all_200=all(set(x['codes']) == {'200'} for x in rs))
first, last = by['idle-2'], by[f'after round {rounds[-1]}']
summary = dict(
    idle={k: by[k] for k in ('idle-1', 'idle-2')},
    after_load=last, settled_30s=by.get('settled-30s'), under_load_overview=by.get('under-load-overview'),
    load_cpu_total_s=round((last['utime_s'] + last['stime_s']) - (first['utime_s'] + first['stime_s']), 2),
    load_wall_s=round((last['t_ms'] - first['t_ms']) / 1000, 1),
    peak_rss_kb=max(p['vmhwm_kb'] for p in procs),
    goroutines=dict(idle=json.load(open(os.path.join(R, 'goroutines-idle.json'))), under_load=json.load(open(os.path.join(R, 'goroutines-load.json')))),
    coldstart=[json.loads(l) for l in open(os.path.join(R, 'coldstart.jsonl'))],
    endpoints=out)
json.dump(summary, open(os.path.join(B, 'raw', 'server-summary.json'), 'w'), indent=2)
print(f"{'path':48} {'req/s':>8} {'p50':>6} {'p95':>6} {'p99':>6} {'cpu s':>6} {'cpu ms/1k':>9} 200s")
for p, v in out.items():
    m = v['median']; print(f"{p:48} {m['rps']:8.0f} {m['p50_ms']:6.2f} {m['p95_ms']:6.2f} {m['p99_ms']:6.2f} {m['cpu_s']:6.1f} {m['cpu_ms_per_1k_req']:9.1f} {v['all_200']}")
print('load cpu total', summary['load_cpu_total_s'], 's over', summary['load_wall_s'], 's wall; peak RSS', summary['peak_rss_kb'], 'kB')
