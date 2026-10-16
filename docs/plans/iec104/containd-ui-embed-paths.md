# containd: route every UI request and redirect through the embed base

This is a handoff spec for agents working on the containd `iec104` branch. It is self-contained.
RangerDanger does not change containd itself.

- **Owner:** Tony Turner. Agents never push, never merge to containd `main` and never publish images.
- **References:** `C:` paths are in containd `iec104` at **4eb73bf1**. Recheck line numbers before you edit.
- **Evidence:** **V** means read from source or observed live. **P** means proposed.

## Background (V)
- Since v0.1.24 (commit a9cb4958) the UI detects that it is served under `/containd/` and prefixes API
  calls with `/containd` (`C:ui/lib/api-core.ts:9-17`). `fetchWithSession` and `apiURL` use that base
  (`C:ui/lib/api-core.ts:143`, `:178`).
- RangerDanger embeds the UI at `/containd/` behind nginx. Its proxy also rewrote `"/api/` to
  `"/containd/api/` in the JS, so calls became `/containd/containd/api/...` and the UI showed
  "Unable to verify session (API unavailable)". RangerDanger now rewrites only the two literals below.

## The two paths that skip the API base (V)
1. `C:ui/lib/api-dataplane.ts:103-105` `downloadPcapURL` returns `/api/v1/pcap/download/<name>` without
   `API_BASE`. It is used as a link (`C:ui/app/dataplane/pcap-library.tsx:192`).
2. `C:ui/components/Console.tsx:30-36` builds the CLI websocket as
   `${proto}://${window.location.host}/api/v1/cli/ws` without `API_BASE`.

3. `C:ui/app/login/page.tsx:73` and `:149` send the user to `/` after login when there is no `next`
   parameter. Embedded, that is the embedder's root page, not the containd dashboard. (V, live: opening
   `/containd/login` directly and logging in lands on the RangerDanger portal. Opening `/containd/` first
   works, because the redirect carries `next=/containd/`.)

Embedded under `/containd/`, paths 1 and 2 go to the embedder's root `/api/...` and fail.

## P (containd)
1. Build paths 1 and 2 through the same base as every other call (`apiURL(...)`, or an exported `API_BASE`
   for the websocket path after the host). For path 3, fall back to the UI's own root (the embed base plus
   `/`), not the origin root.
2. Add a UI test that loads with `location.pathname = "/containd/..."` and asserts:
   - the pcap download href is `/containd/api/v1/pcap/download/<name>`;
   - the websocket URL is `ws(s)://<host>/containd/api/v1/cli/ws`.
3. Add a check (lint or test) that no UI module builds an `/api/` URL without the base.

## RangerDanger acceptance after containd ships this
- RD deletes its two remaining rewrites (`"/api/v1/pcap/download/` and `"/api/v1/cli/ws`) from both
  packages' `nginx.routes.conf`.
- Through the portal at `/containd/`: login, dashboard, pcap download and the CLI console all work, and the
  proxy log shows no `/containd/containd/` and no root `/api/v1/cli/ws` request.
