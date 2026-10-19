#!/usr/bin/env bash
# RC checkout + already-cached v0.1.35 images. NEVER pulls or builds.
# Disposable stacks only. --replace explicitly authorizes replacing even the
# separate rangerdanger-us checkout's stack (it uses the SAME project label).
# Local: run this script, then --known-defects --replace if --keep was used.
# CI: explicitly boot cached images, --existing-stack hard gates, one advisory
# known-defects step, then CI teardown.
#
# --existing-stack checks an already-running disposable compat stack without
# stopping, starting, rebuilding, pulling, or deleting its database/volumes.
# Gates still mutate policy/device state: never use a student's active stack.
set -euo pipefail
export VERSION=v0.1.35
export COMPOSE_FILE=docker-compose.release.yml
export COMPOSE_PROJECT_NAME=rangerdanger
API="${RANGERDANGER_API:-http://localhost:8088}"
receipts="${COMPAT_ARTIFACT_DIR:-build/release-compat}"
replace=0
keep=0
known=0
existing=0
for arg in "$@"; do
  case "$arg" in
    --replace) replace=1 ;;
    --keep) keep=1 ;;
    --known-defects) known=1 ;;
    --existing-stack) existing=1 ;;
    *) echo "unknown argument: $arg" >&2; exit 2 ;;
  esac
done

docker_bounded() {
  python3 -c 'import subprocess,sys; sys.exit(subprocess.run(["docker", *sys.argv[1:]], timeout=300).returncode)' "$@"
}
compose() {
  docker_bounded compose -p rangerdanger -f docker-compose.release.yml "$@"
}
assert_version() {
  curl -fsS --max-time 10 "$API/api/build" >"$receipts/api-build.json"
  jq -e '.version == "v0.1.35"' "$receipts/api-build.json" >/dev/null
}

# Guard BEFORE any stop/up/down/pull, including containers from another cwd.
running=$(docker_bounded ps -q --filter label=com.docker.compose.project=rangerdanger)
if [ -n "$running" ] && [ "$replace" -ne 1 ] && [ "$existing" -ne 1 ]; then
  echo "REFUSED: rangerdanger project is up (possibly rangerdanger-us). Stop it yourself or explicitly pass --replace." >&2
  exit 1
fi
if [ "$existing" -eq 1 ] && [ "$replace" -eq 1 ]; then
  echo "--existing-stack and --replace are mutually exclusive" >&2
  exit 2
fi
if [ "$existing" -eq 1 ] && [ -z "$running" ]; then
  echo "--existing-stack needs a running disposable v0.1.35 compat stack" >&2
  exit 1
fi
mkdir -p "$receipts"

if [ "$known" -eq 1 ]; then
  [ -n "$running" ] || { echo "known-defects needs the compat stack left up with --keep" >&2; exit 1; }
  assert_version
  # Full-plan fixture is generated from 5805c11's real frontend, never from
  # the reference policy. D2 waives ONLY this step; restore weak even on red.
  # shellcheck disable=SC2329 # Invoked by the EXIT trap below.
  restore_weak() {
    local rc=$?
    trap - EXIT
    if ! curl -fsS --max-time 30 -X POST -H 'Content-Type: application/json' \
        -d '{"config":"weak"}' "$API/api/firewall/apply" >"$receipts/rd01-restore.json"; then
      echo "RD-01 cleanup failed" >&2
      rc=1
    fi
    exit "$rc"
  }
  trap restore_weak EXIT
  jq '.config' scripts/tests/fixtures/rd01-full-plan.json >"$receipts/rd01-plan.json"
  curl -fsS --max-time 60 -X POST -H 'Content-Type: application/json' \
    --data-binary @"$receipts/rd01-plan.json" "$API/api/firewall/apply-custom" >"$receipts/rd01-apply.json"
  # Do not retry an unauthorized leak until it passes.
  sleep 3
  curl -fsS --max-time 90 -X POST "$API/api/firewall/validation-report" >"$receipts/rd01-report.json"
  jq -e '.summary | .result == "PASS" and .authorized_pass == 8 and
    .authorized_total == 8 and .unauthorized_pass == 11 and
    .unauthorized_total == 11 and .skipped == 0' "$receipts/rd01-report.json"
  exit 0
fi

# CI needs ~21 GB before caching these images (a stock ~14 GB runner is
# insufficient). A local cached-image run does not need another 21 GB free.
echo "compat image budget: ~21 GB expanded; all images must already be cached"
compose config >"$receipts/compose.resolved.yml"
if [ "$existing" -eq 1 ]; then
  # A running install may use :latest (students keep VERSION=latest), so
  # record the images the containers actually run; /api/build proves v0.1.35.
  images=$(docker_bounded ps --filter label=com.docker.compose.project=rangerdanger --format '{{.Image}}' | sort -u)
else
  images=$(compose config --images)
fi
image_list=()
while IFS= read -r image; do
  [ -n "$image" ] && image_list+=("$image")
done <<<"$images"
[ "${#image_list[@]}" -gt 0 ]
# A cache miss fails here, rather than fetching over the workshop's Wi-Fi.
# Full inspect includes RepoDigests + image IDs, including containd:latest.
docker_bounded image inspect "${image_list[@]}" >"$receipts/images.json"
if [ -n "$running" ] && [ "$existing" -ne 1 ]; then
  echo "WARNING: --replace stopping the existing rangerdanger project, without deleting volumes." >&2
  compose down --remove-orphans
fi
started=0
# shellcheck disable=SC2329 # Invoked by the EXIT trap below.
cleanup() {
  local rc=$?
  trap - EXIT
  if ! compose logs --no-color >"$receipts/compose.log" 2>&1; then
    echo "could not archive compose logs" >&2
    rc=1
  fi
  if [ "$started" -eq 1 ] && [ "$keep" -ne 1 ]; then
    if ! compose down --remove-orphans; then rc=1; fi
  fi
  exit "$rc"
}
trap cleanup EXIT
if [ "$existing" -ne 1 ]; then
  started=1
  compose up -d --no-build --pull never
fi
ready=0
for _ in $(seq 1 60); do
  if curl -fsS --max-time 3 "$API/api/health" >"$receipts/health.json" 2>/dev/null; then
    ready=1
    break
  fi
  sleep 5
done
[ "$ready" -eq 1 ] || { echo "backend did not become healthy" >&2; exit 1; }
assert_version
curl -fsS --max-time 10 "$API/api/firewall/active" >"$receipts/firewall-active.json"
compose ps --format json >"$receipts/containers.json"

# Do not short-circuit: preserve the outputs of every hard gate, then fail if
# ANY failed. Only the separate RD-01 workflow step is continue-on-error.
failed=0
for gate in smoke-test proxy-smoke firewall-smoke lab-commands-smoke events-smoke substation-smoke test-suite-smoke; do
  echo "=== compat: $gate ==="
  args=()
  if [ "$gate" = smoke-test ]; then args=(--existing-stack); fi
  if python3 -c 'import subprocess,sys; sys.exit(subprocess.run(["bash",*sys.argv[1:]], timeout=1200).returncode)' \
      "scripts/$gate.sh" "${args[@]}" >"$receipts/$gate.log" 2>&1; then
    cat "$receipts/$gate.log"
  else
    cat "$receipts/$gate.log"
    failed=1
  fi
done
exit "$failed"
