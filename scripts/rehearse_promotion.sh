#!/usr/bin/env bash
# Rehearse the release workflow's publish paths against a scratch registry on
# loopback, so no real tag is ever touched. It proves four properties the
# release depends on and cannot otherwise be exercised without registry write
# access:
#
#   1. `docker buildx imagetools create` reports the pushed root digest only
#      through --metadata-file. Its stdout is empty.
#   2. Re-tagging a recorded index root preserves that root digest, and
#      scripts/verify_published_image.sh accepts the result.
#   3. A single-manifest source needs --prefer-index=false to keep its digest;
#      without the flag buildx wraps it in a new index with a new root.
#   4. verify_published_image.sh rejects a wrong platform set.
#
# Requirements: docker with buildx, jq, curl. Run it before a release that will
# promote images, and after any buildx upgrade.
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="$ROOT/build/promotion-rehearsal"
PORT="${REHEARSAL_PORT:-5555}"
REGISTRY="127.0.0.1:$PORT"
REPO="$REGISTRY/rehearsal/multi"
SINGLE="$REGISTRY/rehearsal/single"
BUILDER=rd-promotion-rehearsal
CONTAINER=rd-promotion-rehearsal-registry
fail=0

note() { printf '\n== %s ==\n' "$1"; }
check() {
  if [ "$1" = 0 ]; then
    printf 'PASS %s\n' "$2"
  else
    printf 'FAIL %s\n' "$2"
    fail=$((fail + 1))
  fi
}
root_of() {
  docker buildx imagetools inspect --format '{{json .}}' "$1" 2>/dev/null |
    jq -r '.manifest.digest // empty'
}
is_digest() { [[ "${1:-}" =~ ^sha256:[0-9a-f]{64}$ ]]; }

cleanup() {
  docker stop "$CONTAINER" >/dev/null 2>&1 || true
  docker rm "$CONTAINER" >/dev/null 2>&1 || true
  docker buildx rm "$BUILDER" >/dev/null 2>&1 || true
}
trap cleanup EXIT

mkdir -p "$WORK/ctx" "$WORK/meta"
printf 'rehearsal\n' > "$WORK/ctx/payload.txt"
printf 'FROM scratch\nCOPY payload.txt /payload.txt\n' > "$WORK/ctx/Dockerfile"
# BuildKit must be told this registry is plain HTTP, and it needs the host
# network namespace to reach a loopback-published port.
cat > "$WORK/buildkitd.toml" <<EOF
[registry."$REGISTRY"]
  http = true
  insecure = true
EOF

note "scratch registry on $REGISTRY"
cleanup
docker run -d --name "$CONTAINER" -p "127.0.0.1:$PORT:5000" registry:2 >/dev/null 2>&1
check $? "registry started"
until curl -fsS "http://$REGISTRY/v2/" >/dev/null 2>&1; do sleep 1; done
docker buildx create --name "$BUILDER" --driver docker-container \
  --driver-opt network=host --config "$WORK/buildkitd.toml" --bootstrap >/dev/null 2>&1
check $? "builder bootstrapped"
# verify_published_image.sh inspects through the default builder.
export BUILDX_BUILDER="$BUILDER"

note "per-arch builds pushed by digest, as the frontend matrix does"
digest_refs=()
for platform in linux/amd64 linux/arm64; do
  arch="${platform#linux/}"
  docker buildx build --builder "$BUILDER" --platform "$platform" --provenance=false \
    --metadata-file "$WORK/meta/build-$arch.json" \
    --output "type=image,name=$REPO,push-by-digest=true,name-canonical=true,push=true" \
    "$WORK/ctx" > "$WORK/meta/build-$arch.log" 2>&1
  check $? "built $platform"
  digest="$(jq -r '."containerimage.digest" // empty' "$WORK/meta/build-$arch.json" 2>/dev/null)"
  if is_digest "$digest"; then rc=0; else rc=1; fi
  check "$rc" "recorded a $platform manifest digest"
  [ "$rc" = 0 ] && digest_refs+=("$REPO@$digest")
done
[ "${#digest_refs[@]}" = 2 ] || { printf '\nREHEARSAL: fail=%s (builds did not produce two digests)\n' "$((fail + 1))"; exit 1; }

note "property 1: the pushed root digest is not on stdout"
create_stdout="$(docker buildx imagetools create --builder "$BUILDER" \
  --metadata-file "$WORK/meta/create.json" \
  -t "$REPO:v0.0.1" -t "$REPO:latest" \
  "${digest_refs[0]}" "${digest_refs[1]}" 2> "$WORK/meta/create.stderr")"
check $? "imagetools create pushed the index"
printf 'stdout bytes: %s\n' "$(printf '%s' "$create_stdout" | wc -c | tr -d ' ')"
if grep -Eq 'sha256:[0-9a-f]{64}' <<< "$create_stdout"; then rc=1; else rc=0; fi
check "$rc" "stdout carries no digest, so the workflow must read --metadata-file"
merged_root="$(jq -r '."containerimage.descriptor".digest // empty' "$WORK/meta/create.json" 2>/dev/null)"
if is_digest "$merged_root"; then rc=0; else rc=1; fi
check "$rc" "--metadata-file reports containerimage.descriptor.digest"
[ "$rc" = 0 ] || { printf '\nREHEARSAL: fail=%s\n' "$fail"; exit "$fail"; }
if [ "$(root_of "$REPO:v0.0.1")" = "$merged_root" ]; then rc=0; else rc=1; fi
check "$rc" "the pushed tag resolves to the reported root"

note "property 2: promoting an index root preserves it"
cd "$ROOT" || exit 1
scripts/verify_published_image.sh "$REPO" "$merged_root" \
  "$(printf '%s:v0.0.1\n%s:latest' "$REPO" "$REPO")" '["linux/amd64","linux/arm64"]'
check $? "verification accepts the merged index"
media_type="$(docker buildx imagetools inspect --format '{{json .}}' "$REPO@$merged_root" |
  jq -r '.manifest.mediaType // empty')"
printf 'source mediaType: %s\n' "$media_type"
docker buildx imagetools create -t "$REPO:v0.0.2" "$REPO@$merged_root" > "$WORK/meta/promote.log" 2>&1
check $? "promote re-tagged the recorded root"
if [ "$(root_of "$REPO:v0.0.2")" = "$merged_root" ]; then rc=0; else rc=1; fi
check "$rc" "the promoted tag resolves to the recorded root digest"
scripts/verify_published_image.sh "$REPO" "$merged_root" "$REPO:v0.0.2" '["linux/amd64","linux/arm64"]'
check $? "verification accepts the promoted root"

note "property 3: a single manifest needs --prefer-index=false"
docker buildx build --builder "$BUILDER" --platform linux/amd64 --provenance=false \
  --output "type=image,name=$SINGLE:v0.0.1,push=true" "$WORK/ctx" > "$WORK/meta/single.log" 2>&1
check $? "single-platform image pushed"
single_root="$(root_of "$SINGLE:v0.0.1")"
docker buildx imagetools create --prefer-index=false -t "$SINGLE:v0.0.2" "$SINGLE@$single_root" \
  > "$WORK/meta/single-promote.log" 2>&1
check $? "single-manifest promote succeeded"
if is_digest "$single_root" && [ "$(root_of "$SINGLE:v0.0.2")" = "$single_root" ]; then rc=0; else rc=1; fi
check "$rc" "--prefer-index=false preserves the single-manifest root"
docker buildx imagetools create -t "$SINGLE:v0.0.3" "$SINGLE@$single_root" \
  > "$WORK/meta/single-wrapped.log" 2>&1
if [ "$(root_of "$SINGLE:v0.0.3")" != "$single_root" ]; then rc=0; else rc=1; fi
check "$rc" "without the flag buildx wraps it in a new index with a new root"

note "property 4: verification rejects a wrong platform set"
if scripts/verify_published_image.sh "$REPO" "$merged_root" "$REPO:v0.0.1" '["linux/amd64"]' \
  > "$WORK/meta/negative.log" 2>&1; then rc=1; else rc=0; fi
check "$rc" "a short platform list fails verification"

printf '\nREHEARSAL: fail=%s\n' "$fail"
exit "$fail"
