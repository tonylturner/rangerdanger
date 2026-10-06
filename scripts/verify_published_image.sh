#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "Usage: $0 <repository> <expected-root-digest> <planned-tags> <expected-platforms-json>" >&2
  exit 2
}

[[ $# -eq 4 ]] || usage

repository=$1
expected_root_digest=$2
planned_tags=$3
expected_platforms_json=$4

[[ -n "$repository" ]] || usage
[[ "$expected_root_digest" =~ ^sha256:[0-9a-f]{64}$ ]] || {
  echo "Expected root digest is not a full sha256 digest: $expected_root_digest" >&2
  exit 1
}

if ! expected_platforms="$(jq -er '
  if type == "array"
     and length > 0
     and all(.[]; type == "string" and test("^[^/[:space:]]+/[^/[:space:]]+$"))
     and (unique | length) == length
  then sort | join(",")
  else error("expected platforms must be a non-empty array of unique OS/architecture strings")
  end
' <<< "$expected_platforms_json")"; then
  echo "Expected platform list is invalid." >&2
  exit 1
fi

tags=()
while IFS= read -r image_tag; do
  [[ -n "$image_tag" ]] && tags+=("$image_tag")
done <<< "$planned_tags"
[[ ${#tags[@]} -gt 0 ]] || {
  echo "No planned image tags." >&2
  exit 1
}

root_metadata=""
for image_tag in "${tags[@]}"; do
  if ! metadata="$(docker buildx imagetools inspect --format '{{json .}}' "$image_tag")"; then
    echo "Could not inspect planned tag $image_tag." >&2
    exit 1
  fi
  if ! digest="$(jq -er '
    .manifest.digest
    | strings
    | select(test("^sha256:[0-9a-f]{64}$"))
  ' <<< "$metadata")"; then
    echo "$image_tag inspection has no valid .manifest.digest." >&2
    exit 1
  fi
  [[ "$digest" == "$expected_root_digest" ]] || {
    echo "$image_tag resolves to $digest, expected $expected_root_digest" >&2
    exit 1
  }
  [[ -n "$root_metadata" ]] || root_metadata=$metadata
done

if ! media_type="$(jq -er '.manifest.mediaType | strings | select(length > 0)' <<< "$root_metadata")"; then
  echo "Root inspection has no .manifest.mediaType." >&2
  exit 1
fi

case "$media_type" in
  application/vnd.oci.image.index.v1+json|application/vnd.docker.distribution.manifest.list.v2+json)
    if ! jq -e '
      .manifest.manifests
      | type == "array"
        and length > 0
        and all(.[];
          (.digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
          and (.platform | type == "object"
            and (.os | type == "string" and length > 0)
            and (.architecture | type == "string" and length > 0))
          and (.annotations == null or (.annotations | type == "object"))
        )
    ' <<< "$root_metadata" >/dev/null; then
      echo "Index inspection has malformed .manifest.manifests descriptors." >&2
      exit 1
    fi

    if ! actual_platforms="$(jq -er '
      [
        .manifest.manifests[]
        | select(((.annotations // {})["vnd.docker.reference.type"] // "") != "attestation-manifest")
        | select(.platform.os != "unknown" and .platform.architecture != "unknown")
        | "\(.platform.os)/\(.platform.architecture)"
      ]
      | if length > 0 then unique | sort | join(",")
        else error("index has no platform manifests")
        end
    ' <<< "$root_metadata")"; then
      echo "Could not read platform descriptors from .manifest.manifests." >&2
      exit 1
    fi

    while IFS= read -r child_digest; do
      [[ -n "$child_digest" ]] || {
        echo "A non-attestation descriptor has no digest." >&2
        exit 1
      }
      child_ref="${repository}@${child_digest}"
      if ! child_metadata="$(docker buildx imagetools inspect --format '{{json .}}' "$child_ref")"; then
        echo "Child manifest $child_ref is not resolvable." >&2
        exit 1
      fi
      if ! resolved_child_digest="$(jq -er '
        .manifest.digest
        | strings
        | select(test("^sha256:[0-9a-f]{64}$"))
      ' <<< "$child_metadata")"; then
        echo "Child manifest $child_ref inspection has no valid .manifest.digest." >&2
        exit 1
      fi
      [[ "$resolved_child_digest" == "$child_digest" ]] || {
        echo "$child_ref resolved to $resolved_child_digest, expected $child_digest" >&2
        exit 1
      }
    done < <(
      jq -r '
        .manifest.manifests[]
        | select(((.annotations // {})["vnd.docker.reference.type"] // "") != "attestation-manifest")
        | .digest
      ' <<< "$root_metadata"
    )
    ;;
  *)
    if ! actual_platforms="$(jq -er '
      if (.image | type) == "object"
         and (.image.os | type) == "string" and (.image.os | length) > 0
         and (.image.architecture | type) == "string" and (.image.architecture | length) > 0
      then "\(.image.os)/\(.image.architecture)"
      else error("single manifest has no platform in .image")
      end
    ' <<< "$root_metadata")"; then
      echo "Could not read single-manifest platform from .image." >&2
      exit 1
    fi
    ;;
esac

[[ "$actual_platforms" == "$expected_platforms" ]] || {
  echo "Published platforms $actual_platforms do not match $expected_platforms" >&2
  exit 1
}

echo "Verified ${repository}@${expected_root_digest} ($actual_platforms)"
