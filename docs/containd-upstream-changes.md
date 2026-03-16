# containd Upstream Changes

Changes in the local containd repo (`/Users/tturner/Documents/GitHub/containd`, branch `merge-cont`) that should be upstreamed before publishing a generic containd image to DockerHub.

These are all general-purpose improvements — none are RangerDanger-specific.

---

## A. CORS and Frame-Embedding Support (committed)

**Commit:** `24bcd771` — "Add CORS and frame-embedding support for external applications"
**File:** `pkg/app/mgmt/mgmt.go` (+88 lines)
**Status:** Committed on `merge-cont`, not yet pushed to origin

### What it does
Adds middleware to allow the containd UI to be embedded in iframes and accessed cross-origin by external applications.

### Functions added
- **`corsHandler(allowedOrigins []string)`** — Sets `Access-Control-Allow-Origin`, `Allow-Credentials`, `Allow-Methods`, `Allow-Headers`, and `Max-Age`. Handles OPTIONS preflight.
- **`frameOptionsHandler(allowedOrigins []string)`** — Sets `Content-Security-Policy: frame-ancestors 'self' <origins>` for iframe embedding.
- **`getAllowedOrigins()`** — Reads `CONTAIND_ALLOWED_ORIGINS` env var (comma-separated list of allowed origins).

### Configuration
```
CONTAIND_ALLOWED_ORIGINS=http://localhost:8088,https://rangerdanger.example.com
```

---

## B. Docker Interface Compatibility Fix (uncommitted)

**File:** `pkg/dp/netcfg/netcfg_linux.go`
**Status:** Uncommitted working tree change

### What it does
Skips the `setLinkUp` netlink call if the interface already has `FlagUp` set. In Docker environments, interfaces are externally managed and already up — the netlink call can conflict.

### Diff
```diff
@@ -405,6 +405,10 @@ func setLinkUp(name string) error {
 	if err != nil {
 		return err
 	}
+	// Skip if interface is already up (common in Docker where interfaces are externally managed)
+	if nic.Flags&net.FlagUp != 0 {
+		return nil
+	}
 	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_ROUTE)
```

---

## C. NFT Ruleset Preview UI (uncommitted)

**Files:** `ui/app/firewall/page.tsx` (+55 lines), `ui/lib/api.ts` (+11 lines)
**Status:** Uncommitted working tree changes

### What it does
Adds a panel to the firewall page that previews the compiled nftables ruleset before applying. Admin-only feature.

### API client addition (`ui/lib/api.ts`)
```typescript
export type RulesetPreview = {
  ruleset: string;
  snapshot?: unknown;
  engineStatus?: unknown;
  engineStatusError?: string;
};

export async function getRulesetPreview(): Promise<RulesetPreview | null> {
  return await getJSON<RulesetPreview>("/api/v1/dataplane/ruleset");
}
```

### UI addition (`ui/app/firewall/page.tsx`)
- "nftables ruleset preview" card with a "Preview" button
- Calls `GET /api/v1/dataplane/ruleset`
- Displays compiled ruleset in a scrollable `<pre>` block
- Shows engine status errors if present
- Admin-only (view-only notice for non-admin users)

---

## D. Task Doc Updates (uncommitted)

**File:** `docs/tasks.md`
**Status:** Uncommitted, marks NFT preview items as complete

---

## Upstream Workflow

1. Push commit `24bcd771` to `origin/merge-cont`
2. Commit the three uncommitted changes (B, C, D) on `merge-cont`
3. Open PR from `merge-cont` to `main` in the containd repo
4. After merge, tag and publish: `ghcr.io/tonylturner/containd:latest`
5. RangerDanger's `docker-compose.yml` already pulls `ghcr.io/tonylturner/containd:latest` with `pull_policy: always`
