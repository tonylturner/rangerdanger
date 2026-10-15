For students curious about how the firewall actually works under the hood - what happens between "click Apply Hardened" in the lab UI and a TCP packet getting dropped - this article is the tour. It is not strictly necessary for working through the labs, but it pays off when you are reading containd events, debugging policy edge cases, or evaluating other ICS firewalls against containd.

### The Layered Stack

containd is built on **nftables** (the Linux kernel's modern packet-filtering framework, the successor to iptables). On top of nftables, containd adds:

- A **policy compiler** that translates the JSON policy file (zones, rules, ICS predicates) into a set of nftables rules
- A **DPI userspace engine** that handles per-protocol packet inspection
- An **events pipeline** that emits decisions back up to user space

The interesting layers are the kernel-userspace boundary and how packets actually move between them.

### Compilation: From JSON to nftables

When you POST a policy to `/api/v1/policy/import` (or use the web UI), containd parses the JSON and generates the corresponding nftables ruleset. A `Allow enterprise → vendor SSH` rule becomes something like `tcp dport 22 ip saddr 10.10.10.0/24 ip daddr 10.20.20.0/24 accept` in nftables syntax. Rules are organized into chains by zone (input from wan, forward wan → dmz, etc.) and applied with `nft replace ruleset` so the swap is atomic.

You can see the compiled ruleset by running `nft list ruleset` inside the `fw-1` container (Lab 2.4 evidence-package guidance has examples). It looks like a normal nftables output, with extra named sets for the things containd needs to look up dynamically (`block_flows` for DPI-blocked source/dest pairs, `learn_flows` for learn-mode observations).

### The L4 Path (Fast)

A packet arrives at containd's network interface. The kernel runs it through the compiled nftables rules:

1. Match the source/destination IP and port against the chain for that direction.
2. If a rule matches:
   - `accept` → packet is forwarded out the egress interface
   - `drop` → packet is silently discarded
   - `reject` → packet is dropped and a TCP RST or ICMP unreachable is sent back
3. If the rule has logging enabled, copy the packet header to **nflog group N** (the kernel's userspace-logging facility). containd's userspace consumer reads from nflog and emits an event.

This whole path runs in kernel space at line rate. Latency is sub-microsecond per packet. Almost all packets in the lab take this fast path.

### The DPI Path (Slower, More Powerful)

For rules with `dpiMode: enforce`, the rule's action is instead **`queue num N`** - the kernel hands the packet to userspace via the **NFQUEUE** netfilter facility on queue number N.

1. The kernel queues the packet and stalls the flow waiting for a verdict.
2. containd's DPI engine reads the packet from the queue (`AF_NETLINK` socket, `NFQUEUE` protocol).
3. The engine identifies the protocol (Modbus on 502, DNP3 on 20000), parses the payload, and applies the rule's ICS predicate (`functionCode in [1,3,4,5,6]`, `crobOpCode != trip`, etc.).
4. The engine writes a verdict back to the kernel: `NF_ACCEPT` lets the packet through, `NF_DROP` drops it, `NF_QUEUE` re-queues it for further inspection.
5. The DPI engine also emits an event for the operator-visible record.

DPI latency is microseconds-to-milliseconds per packet - fine for SCADA polling cycles (which are seconds-class), nowhere near fast enough for IEC 61850 GOOSE (which is microsecond-class).

### Events: nflog and DPI Both Feed One Stream

Both the kernel-side nflog and the userspace DPI engine emit events to containd's internal event store. The store deduplicates and timestamps them, then publishes them via Server-Sent Events on the containd REST API. RangerDanger's backend subscribes to that stream and re-publishes via `/api/substation/network-events`. The frontend reads from there and renders the Live DPI Events strip. This is the chain from `packet hit on the kernel` to `row appears in the strip` - roughly 100–500 ms end to end depending on event backlog.

### Why This Layered Approach

The fast L4 path lets containd handle line-rate traffic for the 99% of cases where the source/destination/port pair already decides the verdict. The DPI path handles the remaining 1% where the protocol payload matters. Pure-userspace firewalls (Snort, Suricata in inline mode) have lower throughput because every packet pays the user-kernel crossing cost. Pure-kernel firewalls (basic iptables) cannot inspect protocol payloads at all. The hybrid model is why containd can do ICS DPI on production traffic without becoming the bottleneck.