# containd: protected lab control plane and engine API exposure

This is a handoff spec for agents working on the containd `iec104` branch. It is self-contained.
It states two things RangerDanger needs from containd. RangerDanger does not change containd itself.

- **Owner:** Tony Turner. Agents never push, never merge to containd `main` and never publish images.
- **References:** `C:` paths are in containd `iec104` at **4eb73bf1**. `C0:` paths are in containd
  `main` at **8ebc4229** (release 0.1.31). `RD:` paths are in RangerDanger `inc4-integ`. Recheck the
  line numbers before you edit.
- **Evidence:** **V** means read from source or observed live. **P** means proposed.
- **Rule (Tony, 2026-10-15):** "rangerdanger must always have access to containd no matter what
  policy is in place."

## 1. P0: the engine's internal API is open to every zone without authentication

- **V** The engine listens on `CONTAIND_ENGINE_ADDR`, default `:8081`, on all interfaces
  (`C:pkg/app/engine/engine.go:64-67`; `C0:pkg/app/engine/engine.go:64`).
- **V** The INPUT chain accepts `tcp dport 8081` for every interface, before any policy rule
  (`C:pkg/dp/enforce/enforce.go:132`; `C0:pkg/dp/enforce/enforce.go:123`).
- **V** The mux has no authentication. It serves mutation endpoints, including
  `/internal/apply_rules`, `/internal/config`, `/internal/interfaces`, `/internal/routing`,
  `/internal/blocks/host|flow`, `/internal/services` and `/internal/pcap/*`
  (`C:pkg/app/engine/engine.go:142-174`).
- **V (live, EU range, iec104 build):** from Kali on the enterprise zone,
  `GET http://10.10.10.2:8081/internal/ruleset_status` returns 200 with the full nft ruleset.
  POST was not tested.
- **Impact:** any host in any zone can read the firewall state and can probably rewrite it, whatever
  the policy says. Release 0.1.31 has the same code, so this affects RangerDanger ranges that run
  published `:latest`.
- **RD mitigation (iec104 branch only):** RangerDanger sets `CONTAIND_ENGINE_ADDR=127.0.0.1:8081` in
  combined mode (`CONTAIND_MODE=all`). mgmt already uses `CONTAIND_ENGINE_URL=http://127.0.0.1:8081`.
  This is configuration, not a containd fix.

**P (containd):**
1. In combined mode, bind the engine to loopback by default. Split mode keeps an explicit address.
2. Replace the unconditional `tcp dport 8081 accept` with an accept limited to loopback and the
   configured mgmt peer (split mode).
3. Authenticate the engine API in split mode (shared secret or mTLS), or confine it to a private
   control link.
4. Tests:
   - compiler output for each mode;
   - a Linux namespace test where a zone host cannot connect to 8081;
   - mgmt → engine still works in combined and split modes.

## 2. A protected lab control plane that no policy can disable

### What RangerDanger needs (V)
- The backend `10.99.99.10` and the proxy `10.99.99.3` reach containd at `10.99.99.2:8080` (HTTP)
  over the mgmt network. containd binds that network as logical `lan3` by subnet
  (`CONTAIND_AUTO_LAN3_SUBNET=10.99.99.0/24`). The `ethN` index is not stable.
- Uses: policy apply (candidate + commit), config/rules reads, events, flows, conntrack, pcap,
  health, and the portal's `/containd/` UI. HTTPS 8443 and SSH 2222 are manual surfaces that the
  host also publishes.
- RangerDanger never uses a zone address to manage containd.

### Why policy can lock RangerDanger out today (V)
- Access flags are honoured in two places: the INPUT compile (`C:pkg/cp/compile/compile.go:325-353`,
  where nil means allowed and `mgmt=false` gates HTTP and HTTPS but not SSH) and the per-request
  HTTP middleware (`C:pkg/app/mgmt/http_support.go:155-208`, keyed on the socket's local IP).
- INPUT is default-drop. Local management accepts match only the ingress interface and port, with
  no protected source (`C:pkg/dp/enforce/enforce.go:124-145`).
- Nothing rejects a config that disables the interface the request arrived on:
  - candidate/commit (`C:api/http/server.go:451-545`);
  - interface PATCH (`C:api/http/interface_routing_handlers.go:278-331`);
  - full config POST and import (`C:api/http/server.go:403-417`, `config_backup_handlers.go:62-88`);
  - CLI import (`C:pkg/cli/cli.go:436-449`).
- Lab mode changes authentication only, not reachability (`C:pkg/app/mgmt/mgmt.go:123-137`).

RangerDanger already rejects unsafe policies that it applies itself. It cannot see edits made in
the containd UI, the API or the CLI. Students make such edits on purpose in the labs.

### P (containd): an operator-owned protected management contract
1. **Activation:** runtime environment set by the deployment, outside editable config. For example:
   - `CONTAIND_PROTECTED_MGMT_SUBNET=10.99.99.0/24`
   - `CONTAIND_PROTECTED_MGMT_SOURCES=10.99.99.10/32,10.99.99.3/32`
   - `CONTAIND_PROTECTED_MGMT_PORTS=8080,8443,2222`

   Unset means today's behaviour, unchanged. Do not hardcode RangerDanger addresses in containd.
2. **Kernel:** the compiler emits an INPUT accept for (ingress device holding the protected subnet,
   protected sources, protected ports) before the policy-derived local rules. It survives every
   ruleset apply, raw snapshot and dynamic block. It must not touch FORWARD or widen any zone access.
3. **Application:** HTTP and SSH access middleware skips the interface-flag denial only for a
   protected tuple (socket source plus local address). Authentication is still required. Never trust
   forwarded headers.
4. **Config safety:** validation rejects any change that would defeat the contract:
   - deleting or rebinding the interface that holds the protected subnet;
   - removing its address;
   - disabling or moving the HTTP listener;
   - NAT or redirects on protected ports;
   - routes that break the replies.

   This applies on every path: save, candidate/commit, commit-confirmed, rollback, import, interface
   and routing endpoints, CLI, factory reset and boot. Reject before persisting, so there is no
   commit-then-repair window. Return a clear error.
5. **Non-goals:** root shell commands, killing processes, removing Docker networks.
6. **Tests:**
   - unit tests: compiler order, middleware tuples, validation on each mutation path;
   - a Linux nft/namespace integration test: set lan3 access false through the UI API, the CLI and an
     import, and prove the protected sources still connect and every other source is refused.

### RangerDanger acceptance after containd ships this
- RD sets the three variables on the firewall service in both packages.
- A live EU test tries to cut off lan3 through the embedded UI, the API and the CLI. Each attempt
  is either rejected or has no effect. Backend apply, read, reset and the portal `/containd/` keep
  working.
- Zone probes still follow the policy flags. Under EU improved, every zone address is closed on
  8080, 8443, 2222 and 8081.
