#!/usr/bin/env bash
# Server perf baseline: release image (server/Dockerfile, embedded web/dist), seeded, Pi-like limits.
# Run from the worktree root in Git Bash:  bash .pane/prompts/reviews/baseline/tools/perf-server.sh
set -uo pipefail
export MSYS_NO_PATHCONV=1
B=${B:-.pane/scratch/perf-after}; RAW=$B/raw/server; mkdir -p $RAW
BW="$(cygpath -w "$PWD/$B")"
IMG=${IMG:-review-v0141-release}; NET=fix-server-perf; VOL=fix-server-perf-data; SRV=fix-server-perf-server
LIMITS="--cpuset-cpus 0-3 --memory 2g"            # Pi 4/5-like: 4 cores, 2 GB
LOADLIMITS="--cpuset-cpus 4-7 --memory 512m"      # load generator on other cores
ROUNDS=${ROUNDS:-3}; DUR=${DUR:-8s}; CONC=${CONC:-8}
PATHS=(/api/members /api/me /api/lookups /api/overview /api/events /api/events/event-kickoff /api/events/event-kickoff/metrics
  "/api/requests?scope=all" /api/requests/req-0002 /api/finance/summary /api/finance/budgets
  /api/finance/reports/budget-vs-actual /api/finance/reports/statement-of-activities "/api/search?q=demo")
STATIC=(/ /login/)
now() { date +%s%3N; }
stats() { # label -> one JSON line in $RAW/proc.jsonl
  local s; s=$(docker exec $SRV sh -c 'cat /proc/1/status; echo STAT $(cat /proc/1/stat); echo MEMCUR $(cat /sys/fs/cgroup/memory.current); grep -E "^(anon|file) " /sys/fs/cgroup/memory.stat; echo CLK $(getconf CLK_TCK)' 2>/dev/null)
  python -I -c 'import sys,json,re
s=sys.stdin.read(); g=lambda k: (re.search(r"^"+k+r":\s+(\d+)",s,re.M) or [0,0])[1]
st=re.search(r"^STAT (.*)$",s,re.M).group(1).rsplit(")",1)[1].split()
clk=int(re.search(r"^CLK (\d+)",s,re.M).group(1))
o=dict(label=sys.argv[1],t_ms=int(sys.argv[2]),vmrss_kb=int(g("VmRSS")),vmhwm_kb=int(g("VmHWM")),threads=int(g("Threads")),
 utime_s=int(st[11])/clk,stime_s=int(st[12])/clk,cgroup_memory_current=int(re.search(r"^MEMCUR (\d+)",s,re.M).group(1)),
 cgroup_anon=int(re.search(r"^anon (\d+)",s,re.M).group(1)),cgroup_file=int(re.search(r"^file (\d+)",s,re.M).group(1)))
print(json.dumps(o))' "$1" "$(now)" <<<"$s" | tee -a $RAW/proc.jsonl
}
wait_healthy() { local t0=$1; for i in $(seq 1 2000); do curl -sf http://127.0.0.1:8142/api/health >/dev/null && { echo $(( $(now) - t0 )); return 0; }; sleep 0.01; done; echo -1; }
hey() { docker run --rm --network $NET $LOADLIMITS -v "$BW\tools\bin:/b:ro" debian:bookworm-slim /b/hey "$@"; }
goroutines() { docker logs $SRV 2>&1 | grep -cE '^goroutine [1-9][0-9]* .*\['; }

rm -f $RAW/*; docker rm -f $SRV >/dev/null 2>&1; docker network create $NET >/dev/null 2>&1; docker volume rm $VOL >/dev/null 2>&1; docker volume create $VOL >/dev/null
docker run --rm --user 10001 -v $VOL:/data --entrypoint /app/dashboard $IMG seed -reset > $RAW/seed.log 2>&1
COOKIE=$(docker run --rm --user 10001 -v $VOL:/data python:3.13-slim python -c '
import sqlite3,hashlib,secrets,time
c=secrets.token_hex(32); now=int(time.time()*1000); db=sqlite3.connect("/data/dashboard.db")
db.execute("INSERT INTO sessions (id,user_id,expires_at,user_agent,created_at) VALUES (?,?,?,?,?)",(hashlib.sha256(c.encode()).hexdigest(),"user-director",now+7*86400000,"perf-baseline",now)); db.commit(); print(c)')
H="Cookie: cseed_session=$COOKIE"
docker create --name $SRV --network $NET --network-alias dashboard $LIMITS -e APP_URL=http://localhost:8090 -e GOTRACEBACK=all \
  -v $VOL:/data -p 127.0.0.1:8142:8090 $IMG > /dev/null
docker inspect $SRV --format '{{json .HostConfig.NanoCpus}} cpuset={{.HostConfig.CpusetCpus}} mem={{.HostConfig.Memory}}' > $RAW/limits.txt

# 1) cold start, idle, idle goroutines (SIGQUIT ends this process)
t0=$(now); docker start $SRV >/dev/null; echo "{\"start_to_healthy_ms\": $(wait_healthy $t0)}" > $RAW/coldstart-1.json
curl -s -H "$H" http://127.0.0.1:8142/api/me > $RAW/me.json; echo; head -c 200 $RAW/me.json; echo
sleep 10; stats idle-1
docker kill -s QUIT $SRV >/dev/null; sleep 2; docker logs $SRV > $RAW/server-log-1.txt 2>&1; echo "{\"idle_goroutines\": $(python -I "$B/tools/goroutine-count.py" "$RAW/server-log-1.txt")}" > $RAW/goroutines-idle.json
# 2) restart (same DB), idle, load rounds, settled, goroutines under load
t0=$(now); docker start $SRV >/dev/null; echo "{\"start_to_healthy_ms\": $(wait_healthy $t0)}" > $RAW/coldstart-2.json
sleep 10; stats idle-2
for r in $(seq 1 $ROUNDS); do
  for p in "${PATHS[@]}" "${STATIC[@]}"; do
    slug=$(echo "$p" | tr '/?=&' '____' | sed 's/^_//'); [ -z "$slug" ] && slug=root
    stats "pre r$r $p" >/dev/null
    hey -z $DUR -c $CONC -H "$H" "http://dashboard:8090$p" > $RAW/hey-r$r-$slug.txt 2>&1
    stats "post r$r $p" >/dev/null
  done
  stats "after round $r"
done
sleep 30; stats settled-30s
( hey -z 10s -c $CONC -H "$H" "http://dashboard:8090/api/overview" > $RAW/hey-goroutine-probe.txt 2>&1 ) &
sleep 5; stats under-load-overview; docker kill -s QUIT $SRV >/dev/null; wait
sleep 2; docker logs $SRV > $RAW/server-log-2.txt 2>&1
echo "{\"under_load_goroutines\": $(python -I "$B/tools/goroutine-count.py" "$RAW/server-log-2.txt")}" > $RAW/goroutines-load.json
docker rm -f $SRV >/dev/null; docker volume rm $VOL >/dev/null; docker network rm $NET >/dev/null
echo done
