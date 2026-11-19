#!/bin/sh
set -eu
label=${LABEL:-after}
mkdir -p "/scratch/bench-$label" /scratch/tmp
cp -r /repo/server/. "/scratch/bench-$label/"
cd "/scratch/bench-$label"
export PANE_SCRATCH_DIR=/scratch TMPDIR=/scratch/tmp GOCACHE=/scratch/go-cache GOPATH=/scratch/go-path
go version
echo "nproc=$(nproc)"
cat /sys/fs/cgroup/cpu.max /sys/fs/cgroup/cpuset.cpus.effective /sys/fs/cgroup/memory.max
go test -c -o "/scratch/api-$label.test" ./internal/api >/dev/null
for run in 1 2 3; do
 echo "=== bench run $run"
 go test -bench . -benchmem -run '^$' ./...
done
BENCH_LATENCY=1 go test -count=1 -run LatencyReport -v ./internal/api
