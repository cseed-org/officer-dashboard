#!/bin/sh
set -eu
export PANE_SCRATCH_DIR=/scratch TMPDIR=/scratch/tmp GOCACHE=/scratch/go-cache GOPATH=/scratch/go-path GOMAXPROCS=4 GOFLAGS=-p=2
mkdir -p /scratch/tmp /scratch/event-list-bench
cp -r /repo/server/. /scratch/event-list-bench/
cd /scratch/event-list-bench
go version
echo 'cpuset and memory.max:'
cat /sys/fs/cgroup/cpuset.cpus.effective /sys/fs/cgroup/memory.max
echo 'Seeded production-handler event benchmark (timestamp functions in queries only).'
for run in 1 2 3; do
  echo "=== event benchmark run $run"
  go test -run '^$' -bench '^BenchmarkReadEndpoints$/^$/^api$/^events$' -benchmem ./internal/api
done
