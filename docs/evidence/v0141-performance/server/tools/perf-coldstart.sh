#!/usr/bin/env bash
# Cold start x5: container StartedAt -> first 200 from /api/health (polled from busybox on the network,
# same VM clock), plus the server's own startup_ms log field. Run from the worktree root in Git Bash.
set -uo pipefail
export MSYS_NO_PATHCONV=1
B=${B:-.pane/scratch/perf-after}; RAW=$B/raw/server; IMG=${IMG:-review-v0141-release}
NET=fix-server-perf; VOL=fix-server-perf-data; SRV=fix-server-perf-cold
docker network create $NET >/dev/null 2>&1; docker volume create $VOL >/dev/null
docker run --rm --user 10001 -v $VOL:/data --entrypoint /app/dashboard $IMG seed -reset >/dev/null 2>&1
docker rm -f $SRV >/dev/null 2>&1
docker create --name $SRV --network $NET --network-alias dashboard --cpuset-cpus 0-3 --memory 2g -e APP_URL=http://localhost:8090 -v $VOL:/data $IMG >/dev/null
: > $RAW/coldstart.jsonl
for i in 1 2 3 4 5; do
  docker run -d --name fix-server-perf-poll --network $NET python:3.13-slim python -c '
import time,urllib.request
while True:
    try:
        if urllib.request.urlopen("http://dashboard:8090/api/health",timeout=1).status==200: break
    except Exception: time.sleep(0.002)
print(time.time_ns(),flush=True); time.sleep(30)' >/dev/null
  sleep 2   # poller up before the server starts
  docker start $SRV >/dev/null
  for j in $(seq 1 200); do t=$(docker logs fix-server-perf-poll 2>/dev/null | tail -1); [ -n "$t" ] && break; sleep 0.05; done
  started=$(docker inspect $SRV --format '{{.State.StartedAt}}')
  s_ns=$(python -I -c 'import sys,datetime as d;s=sys.argv[1].rstrip("Z");b,f=(s.split(".")+["0"])[:2];t=d.datetime.fromisoformat(b).replace(tzinfo=d.timezone.utc);print(int(t.timestamp())*10**9+int((f+"000000000")[:9]))' "$started")
  ms=$(docker logs $SRV 2>&1 | grep -oE 'startup_ms=[0-9]+' | tail -1 | cut -d= -f2)
  echo "{\"run\": $i, \"startedAt\": \"$started\", \"started_to_healthy_ms\": $(( (t - s_ns) / 1000000 )), \"server_startup_ms\": ${ms:-null}}" | tee -a $RAW/coldstart.jsonl
  docker rm -f fix-server-perf-poll >/dev/null; docker stop -t 5 $SRV >/dev/null
done
docker rm -f $SRV >/dev/null; docker volume rm $VOL >/dev/null; docker network rm $NET >/dev/null
