# All credentials here are synthetic and usable only on this isolated local fake.
# Requires Docker Desktop. Runs no production API, migration, or deploy.
$ErrorActionPreference = 'Stop'
$r2Repo = Split-Path $PSScriptRoot -Parent
$r2Name = 'officer-r2-p5-minio'
docker build -f "$r2Repo/server/testdata/r2/Dockerfile.minio" -t officer-r2-p5-minio "$r2Repo/server/testdata/r2"
if ($LASTEXITCODE -ne 0) { throw 'MinIO build failed' }
docker run -d --rm --name $r2Name -p 127.0.0.1:19000:9000 -e MINIO_ROOT_USER=r2-test-access -e MINIO_ROOT_PASSWORD=r2-test-secret-only officer-r2-p5-minio server /data
if ($LASTEXITCODE -ne 0) { throw 'MinIO startup failed (port/name may already be used)' }
try {
  $r2Ready = $false
  for ($r2Attempt=0; $r2Attempt -lt 30; $r2Attempt++) {
    try { Invoke-WebRequest -UseBasicParsing http://127.0.0.1:19000/minio/health/ready | Out-Null; $r2Ready=$true; break } catch { Start-Sleep -Seconds 1 }
  }
  if (!$r2Ready) { throw 'MinIO did not become ready' }
  docker run --rm -e R2_TEST_ENDPOINT=http://host.docker.internal:19000 -v "${r2Repo}/server:/src" -w /src golang:1.27.4-bookworm sh -c 'go test -v ./internal/files -run TestR2Minio && go test -v ./internal/api -run TestR2ReceiptAuthorizationEndToEnd'
  if ($LASTEXITCODE -ne 0) { throw 'R2 integration tests failed' }
} finally {
  docker stop $r2Name | Out-Null
}
