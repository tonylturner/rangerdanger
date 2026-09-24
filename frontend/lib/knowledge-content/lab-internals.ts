import type { Section } from "../knowledge-types";

export const labInternals: Section =
  /* ====== Lab-Specific Context ====== */
  {
    heading: "Lab Internals",
    description:
      "How the lab is wired together: architecture, weak vs hardened policy, the plan-coverage pipeline, the Live DPI Events strip, and how containd enforces policy at the kernel.",
    icon: "layers",
    accent: "slate",
    articles: [
      {
        id: "lab-architecture",
        title: "The RangerDanger Lab Architecture",
        body: `The RangerDanger lab models a distribution substation with four network zones separated by the containd Next-Generation Firewall (NGFW), plus an unfirewalled physics simulation network.

### Network Zones

| Zone | Subnet | Key Hosts |
|------|--------|-----------|
| Enterprise | 10.10.10.0/24 | Corporate workstation (10.10.10.10), Kali attacker (10.10.10.50) |
| Vendor | 10.20.20.0/24 | Jump box (10.20.20.10), Engineering workstation (10.20.20.20) |
| OT Operations | 10.30.30.0/24 | HMI/FUXA (10.30.30.10), RTAC (10.30.30.20), OpenPLC (10.30.30.30) |
| Field Devices | 10.40.40.0/24 | Relay (10.40.40.20), Recloser (10.40.40.21), Regulator (10.40.40.22) |
| Physics | 10.50.50.0/24 | OpenDSS engine (10.50.50.20), not firewalled |

All inter-zone traffic flows through containd, which acts as the default gateway for every zone.

### Three Protocol Interfaces

The field device simulators are custom Go services that implement three protocol interfaces simultaneously on the same shared state:

1. **HTTP REST** on port 8080: \`GET /api/state\`, \`POST /api/command\`, \`GET /api/audit\`, \`GET /api/health\`
2. **Modbus TCP** on port 502: function codes FC01/FC03/FC04 for reads, FC05/FC06 for writes
3. **DNP3 TCP** on port 20000: Read (FC01), Direct Operate (FC05), Select-Before-Operate (FC03/FC04)

All three interfaces operate on the same device state. A Modbus write to the relay breaker coil is immediately reflected in HTTP and DNP3 responses. Use whichever protocol suits the exercise.

### The Simulation Loop

The RTAC polls all field devices and the physics engine at regular intervals, aggregating their state into a unified view. The HMI displays this on a one-line diagram showing feeder topology, breaker positions, voltage levels, and device health. The physics engine calculates electrical quantities (voltage, energization) based on current device states, creating a **closed-loop simulation** where opening a breaker actually de-energizes the downstream feeder section.

### Background Traffic

The RTAC polling cycle generates baseline autonomous traffic on the OT and field networks. This is realistic background traffic that you can observe and analyze. When you execute attack exercises, your malicious traffic is superimposed on this baseline. You can use the containd event feed and the HMI to observe both the network-level and process-level effects of your actions.`,
      },
      {
        id: "weak-vs-hardened",
        title: "Weak vs Hardened Firewall Policy",
        body: `The lab provides two firewall configurations representing opposite ends of the segmentation spectrum. Both use the exact same network topology. The only difference is the containd firewall rules.

### Weak Baseline (substation-weak.json)

The weak policy is intentionally permissive, reflecting the reality of many substations where firewalls exist but are configured with broad allow rules. Under this policy:

- The enterprise zone can communicate freely with OT operations and field devices
- The Kali attacker at 10.10.10.50 can directly reach the relay at 10.40.40.20 on Modbus (port 502) and DNP3 (port 20000)
- The vendor zone has similarly broad access
- There is **no protocol-level filtering**, so Modbus writes and DNP3 Direct Operate commands pass through unchallenged

This configuration enables all attack exercises to succeed.

### Hardened Policy (substation-improved.json)

The hardened policy represents the target state after a segmentation improvement project:

- Enterprise zone **cannot reach** the field device zone at all
- Vendor zone has limited access to OT operations for engineering purposes, but **no direct field device access**
- Only the RTAC (10.30.30.20) can send Modbus write commands (FC05, FC06) to field devices
- Only the RTAC can send DNP3 Direct Operate commands to field devices
- Other OT hosts (HMI, PLC) can read from field devices but cannot write

Even if an attacker compromises the HMI or an enterprise host, they cannot directly manipulate field devices.

### The Progressive Learning Model

The exercises follow a deliberate sequence:

1. Run attacks against the **weak baseline** and successfully compromise field devices from the enterprise zone
2. Observe the impact on the simulated power system via the HMI
3. Switch to the **hardened policy**
4. Attempt the same attacks again and watch them fail
5. Inspect the firewall logs to see the blocked connection attempts

This direct comparison builds intuition for why segmentation matters and exactly which rules provide the protection.`,
      },
      {
        id: "traffic-matrix",
        title: "Understanding the Traffic Matrix",
        body: `A traffic matrix documents all expected communication flows between zones and devices in an OT network. For each flow, you record the source, destination, protocol, port, direction, frequency, and purpose. Creating an accurate traffic matrix is the essential first step in designing firewall rules, because you cannot write effective allow rules without knowing what legitimate traffic looks like.

### Baseline vs. Exercise Traffic

The lab distinguishes between two categories:

**Baseline traffic** is the normal operational communication that runs continuously:
- RTAC polling field devices via HTTP
- HMI fetching aggregated state from the RTAC
- Physics engine exchanging data with the RTAC

This represents the legitimate communication that the firewall must always permit.

**Exercise traffic** is the additional communication generated during attack steps:
- Modbus writes from the Kali box to field devices
- DNP3 commands from unauthorized sources
- Port scans from the enterprise zone into field device zone

### Using the Matrix for Anomaly Detection

When reviewing containd events or packet captures, compare observed traffic against the matrix. For example, if the matrix shows that only the RTAC (10.30.30.20) should send Modbus traffic to the relay (10.40.40.20), then any Modbus traffic from 10.10.10.50 (Kali) to 10.40.40.20 is immediately suspicious.

### From Matrix to Firewall Rules

The traffic matrix is the specification for the hardened firewall policy. Each row representing a legitimate, necessary flow becomes an explicit allow rule. Everything not in the matrix is denied by default. This approach, called a positive security model or whitelisting, is the recommended practice per IEC 62443 and NERC CIP-005.

You can use the traffic matrix as a reference when reviewing the hardened firewall rules to verify that each rule corresponds to a documented legitimate flow.

### In a Real Engagement

In a real substation, building the traffic matrix is often the most time-consuming step. It requires interviewing operations engineers, reviewing relay settings, understanding SCADA polling configurations, and capturing baseline traffic for analysis. The lab provides the matrix as a given, but in practice, developing it is a significant portion of the segmentation assessment work.`,
      },
      {
        id: "plan-coverage-pipeline",
        title: "The Plan Coverage Pipeline (1.3 → 1.4 → 2.2 → 2.3 → 2.4)",
        body: `The seven labs are not a flat sequence of independent exercises. The choices you make in Labs 1.3 and 1.4 actually rewrite the content you see in Labs 2.2, 2.3, 2.3-bonus, and 2.4. This article explains the data pipeline behind that, because it is not obvious from the surface and a lot of the lab's pedagogical payoff comes from noticing it.

### The Three Inline Markdown Fences

Lab YAML descriptions support three custom fence blocks that the exercise runner parses out:

**\`:::decision\`** captures a multiple-choice answer from the student into browser localStorage. Lab 1.3 uses these to record design verdicts (BLOCK / RESTRICT / ALLOW per requirement). Lab 1.4 uses them to record selected remediation actions and per-role labor budgets. Each fence has an \`id\` so later labs can read the recorded answer.

\`\`\`
:::decision id=enterprise-to-field options=BLOCK,RESTRICT,ALLOW correct=BLOCK
What is your design verdict for Enterprise → Field traffic?
:::
\`\`\`

**\`:::findings-panel\`** displays what the student recorded in an earlier lab as a structured panel. Lab 2.4 uses these to surface the design verdicts from Lab 1.3 and the observations from Lab 1.2 inside the validation step, so the student can compare current evidence against original intent without flipping back through tabs.

**\`:::plan-coverage\`** is the runtime engine. It reads the student's Lab 1.4 selections from localStorage and renders a live "this requirement is fully addressed / partial / deferred / not applicable" matrix. Lab 2.3 uses it to show which of the three attack defenses the student's plan actually closes. Lab 2.4 uses it for the final reflection.

### The Closed Loop

Concretely, here is what happens when a student makes a choice in Lab 1.4:

1. The student clicks a remediation action in Lab 1.4 - say \`pin-rtac-to-field\`.
2. \`scenario-runner.tsx\` writes the selection to localStorage under \`decision:remediation-planning:pin-rtac-to-field\` with the value \`SELECTED\`.
3. Subsequent labs read the same key when their description is rendered. The \`injectDynamicContent()\` helper in \`scenario-runner.tsx\` looks for \`:::plan-coverage\` fences and computes coverage live based on what is in localStorage.
4. Lab 2.2's Phase 3 text adapts based on whether the student selected DPI actions or not. Lab 2.3's "Apply the hardened policy" plan-coverage panel shows which attacks the student's plan actually closes. Lab 2.4's final reflection shows the full coverage matrix.

### Why This Matters Pedagogically

A lab that says "build a hardened firewall policy" without forcing the student to commit to specific design verdicts and remediation choices first lets the student skip the thinking. The plan-coverage pipeline makes the thinking visible: by Lab 2.4 the student can see, "I committed to BLOCK on enterprise-to-field in Lab 1.3, I deferred adding Modbus DPI in Lab 1.4, and now Lab 2.4's panel tells me my plan does close the enterprise-to-field attack but leaves the eng-ws-to-RTAC-on-DNP3 surface open."

The same workshop can be run two different ways - straight through, treating the labs as independent exercises, or with explicit attention to the pipeline. Both work. The pipeline is the deeper read.

### Resetting the State

Student progress lives in localStorage on each student's browser, scoped by exercise ID. To reset a single lab's recorded decisions during a workshop, open the browser console on the lab page and run \`localStorage.clear()\` (clears everything) or \`Object.keys(localStorage).filter(k => k.startsWith("decision:remediation-planning")).forEach(k => localStorage.removeItem(k))\` to reset just one lab. The instructor-facing \`/api/workshop/reset\` endpoint resets simulator state (substation devices, firewall config) but does not touch student-side localStorage.`,
      },
      {
        id: "live-dpi-events-strip",
        title: "Reading the Live DPI Events Strip",
        body: `The Segmentation drawer on the [Network Map](/console) has a **Live DPI Events** strip that surfaces firewall events in real time - every accept, every deny, every DPI verdict. This article is the operator's manual for reading it.

### Where the Events Come From

containd emits two kinds of events that feed the strip:

1. **L4 events** - every packet that matches a rule with logging enabled. These come from the kernel's **nflog** facility on the \`nflogGroup\` configured in the active policy (group 100 in the lab's hardened config). A userspace consumer inside containd reads them and forwards them via Server-Sent Events to the backend, which republishes them on \`/api/substation/network-events\`.
2. **DPI events** - when a rule has \`dpiMode: enforce\` set, the packet is also queued to userspace via **NFQUEUE** (group 101 in the lab). The DPI engine parses the payload, applies the predicate, and emits a structured event with the verdict and the protocol-specific reason.

### Reading a Row

Each row in the strip looks roughly like:

\`\`\`
12:34:56.789  category: l4    verdict: DENY   src: 10.10.10.50  dst: 10.40.40.20  port: 502   rule: enterprise-deny
\`\`\`

The fields you actually care about for lab evidence:

- **timestamp** - when the packet arrived. Useful for correlating with your probe commands.
- **category** - either \`l4\` (decided by the kernel-side rule) or \`ics\` (decided by the userspace DPI verdict). The distinction is the lab's whole "L4 + DPI defense in depth" lesson.
- **verdict** - \`ACCEPT\` or \`DENY\`. (Plus \`BlockFlowTemp\` for the case where DPI temporarily blocks a flow to throttle abuse.)
- **src / dst / port** - the L4 5-tuple.
- **rule** - the rule ID that matched. Cross-reference against the active policy's JSON to see what the rule actually allows.
- **attributes** - for DPI events, the protocol-specific reason: \`functionCode: 5\` for a Modbus FC5 reject, \`crobOpCode: trip\` for a DNP3 trip reject, etc.

### Common Patterns in the Lab

**An L4 deny on a hardened policy.** You ran an enterprise → field probe. The kernel dropped the packet before DPI was reached. You see:

\`\`\`
category: l4   verdict: DENY   src: 10.10.10.50   dst: 10.40.40.20   port: 502   rule: enterprise-deny
\`\`\`

That is the L4 source-pin in action.

**An ICS DPI deny on a partially-allowed flow.** You ran a probe from a source the L4 rule allows but the DPI rule does not (e.g., an eng-ws-to-RTAC Modbus write). You see:

\`\`\`
category: l4   verdict: ACCEPT   src: 10.20.20.20   dst: 10.30.30.20   port: 502   rule: eng-ws-to-rtac
category: ics  verdict: DENY    src: 10.20.20.20   dst: 10.30.30.20   port: 502   rule: rtac-dpi  attributes: { functionCode: 5 }
\`\`\`

Both rows appear because the packet passed L4 and then got rejected by DPI. This is the *exact* condition the lab's "DPI matters even when L4 is loose" hint block in Lab 2.3 is trying to make visible.

**Lab 2.4 evidence assembly.** When you assemble the evidence package, run your negative-test probes one at a time and screenshot or copy out the matching event rows. The change board sees both the policy intent (your \`student-policy.json\` export) and the policy reality (these event rows). Two-source attestation.

### When the Strip Stays Empty

If you fire a probe and no row appears, the usual causes are:

- The probe never actually hit containd (kernel did not see it - check your source). If you probed from the wrong container, the packet may have stayed intra-zone.
- The matching rule has logging disabled. Lab policies enable logging on the deny-class rules; if you have applied your own custom policy without logging, your rules may match without emitting events.
- The \`nflogGroup\` is not set on the active profile. The lab's improved policy sets \`dataplane.nflogGroup: 100\`; if you exported and re-imported a policy that lost that key, the L4 events never reach containd's consumer.
- The frontend strip is filtered. The drawer has a category-and-verdict filter at the top; make sure it is not filtering out the rows you want to see.

The strip is the closest thing the lab has to a real-time firewall watch. Use it.`,
      },
      {
        id: "how-containd-enforces-policy",
        title: "How containd Enforces Policy (Kernel-Level View)",
        body: `For students curious about how the firewall actually works under the hood - what happens between "click Apply Hardened" in the lab UI and a TCP packet getting dropped - this article is the tour. It is not strictly necessary for working through the labs, but it pays off when you are reading containd events, debugging policy edge cases, or evaluating other ICS firewalls against containd.

### The Layered Stack

containd is built on **nftables** (the Linux kernel's modern packet-filtering framework, the successor to iptables). On top of nftables, containd adds:

- A **policy compiler** that translates the JSON policy file (zones, rules, ICS predicates) into a set of nftables rules
- A **DPI userspace engine** that handles per-protocol packet inspection
- An **events pipeline** that emits decisions back up to user space

The interesting layers are the kernel-userspace boundary and how packets actually move between them.

### Compilation: From JSON to nftables

When you POST a policy to \`/api/v1/policy/import\` (or use the web UI), containd parses the JSON and generates the corresponding nftables ruleset. A \`Allow enterprise → vendor SSH\` rule becomes something like \`tcp dport 22 ip saddr 10.10.10.0/24 ip daddr 10.20.20.0/24 accept\` in nftables syntax. Rules are organized into chains by zone (input from wan, forward wan → dmz, etc.) and applied with \`nft replace ruleset\` so the swap is atomic.

You can see the compiled ruleset by running \`nft list ruleset\` inside the \`fw-1\` container (Lab 2.4 evidence-package guidance has examples). It looks like a normal nftables output, with extra named sets for the things containd needs to look up dynamically (\`block_flows\` for DPI-blocked source/dest pairs, \`learn_flows\` for learn-mode observations).

### The L4 Path (Fast)

A packet arrives at containd's network interface. The kernel runs it through the compiled nftables rules:

1. Match the source/destination IP and port against the chain for that direction.
2. If a rule matches:
   - \`accept\` → packet is forwarded out the egress interface
   - \`drop\` → packet is silently discarded
   - \`reject\` → packet is dropped and a TCP RST or ICMP unreachable is sent back
3. If the rule has logging enabled, copy the packet header to **nflog group N** (the kernel's userspace-logging facility). containd's userspace consumer reads from nflog and emits an event.

This whole path runs in kernel space at line rate. Latency is sub-microsecond per packet. Almost all packets in the lab take this fast path.

### The DPI Path (Slower, More Powerful)

For rules with \`dpiMode: enforce\`, the rule's action is instead **\`queue num N\`** - the kernel hands the packet to userspace via the **NFQUEUE** netfilter facility on queue number N.

1. The kernel queues the packet and stalls the flow waiting for a verdict.
2. containd's DPI engine reads the packet from the queue (\`AF_NETLINK\` socket, \`NFQUEUE\` protocol).
3. The engine identifies the protocol (Modbus on 502, DNP3 on 20000), parses the payload, and applies the rule's ICS predicate (\`functionCode in [1,3,4,5,6]\`, \`crobOpCode != trip\`, etc.).
4. The engine writes a verdict back to the kernel: \`NF_ACCEPT\` lets the packet through, \`NF_DROP\` drops it, \`NF_QUEUE\` re-queues it for further inspection.
5. The DPI engine also emits an event for the operator-visible record.

DPI latency is microseconds-to-milliseconds per packet - fine for SCADA polling cycles (which are seconds-class), nowhere near fast enough for IEC 61850 GOOSE (which is microsecond-class).

### Events: nflog and DPI Both Feed One Stream

Both the kernel-side nflog and the userspace DPI engine emit events to containd's internal event store. The store deduplicates and timestamps them, then publishes them via Server-Sent Events on the containd REST API. RangerDanger's backend subscribes to that stream and re-publishes via \`/api/substation/network-events\`. The frontend reads from there and renders the Live DPI Events strip. This is the chain from \`packet hit on the kernel\` to \`row appears in the strip\` - roughly 100–500 ms end to end depending on event backlog.

### Why This Layered Approach

The fast L4 path lets containd handle line-rate traffic for the 99% of cases where the source/destination/port pair already decides the verdict. The DPI path handles the remaining 1% where the protocol payload matters. Pure-userspace firewalls (Snort, Suricata in inline mode) have lower throughput because every packet pays the user-kernel crossing cost. Pure-kernel firewalls (basic iptables) cannot inspect protocol payloads at all. The hybrid model is why containd can do ICS DPI on production traffic without becoming the bottleneck.`,
      },
    ],
  }
;
