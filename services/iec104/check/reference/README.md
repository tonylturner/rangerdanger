<!-- SPDX-License-Identifier: Apache-2.0 -->

# Sealed C test reference

`build.py` extracts only the first-party build inputs from the revision in
`baseline.json`, then builds its **unchanged** production recipe. It never
reads dependency sources or headers. All compiler/dependency output is
discarded; build failure is reported without exposing logs.

```sh
python3 services/iec104/check/reference/build.py --out build/iec104-reference
```

The tracked manifest pins provenance; the materialized
`build/iec104-reference/baseline.json` additionally records the local image ID.
Image IDs are platform/build-specific, so they cannot be committed as a
portable constant. `roles.json` is input to the differential runner.

The image retains the existing GPL licenses and corresponding source payload.
It is local and test-only: never publish it, add it to release inventory,
Compose, or SSD bundles. The caller owns removing the reference tag after
testing. At product cutover, archive the first-party reference inputs here
before removing their production paths; Git remains the present authority.
