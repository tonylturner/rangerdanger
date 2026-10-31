**HMI** (Human-Machine Interface) is the operator's view of an industrial process. In a substation control room the HMI is the screen that shows breakers as open / closed circles, voltages as numeric tiles, and alarms as colored banners. It is what the operator looks at all day.

**SCADA** (Supervisory Control and Data Acquisition) is the larger system: the data-acquisition infrastructure that polls field devices, normalizes their values, stores them in a historian, and forwards selected operator commands back down to the equipment. SCADA is the *plumbing*; the HMI is the *screen*.

**DCS** (Distributed Control System) is the related architecture for plant-wide automation (refineries, chemical plants) where the control logic itself is distributed among many controllers. In substations the architecture is usually called SCADA rather than DCS, but the device-and-protocol surface looks similar.

:::tip The lab's primary HMI is at /substation
Throughout the workshop, every reference to "the Feeder HMI" or "the operational consequence at the HMI" means the **`/substation` panel** built into the RangerDanger web app - not FUXA. Open it at [Feeder HMI](/substation). The Lab 2.3 alarm chain ("RECLOSER OPEN - downstream loads lost") and the customer-service tile both live there.
:::

### What a Real Substation HMI Looks Like

A typical utility HMI shows:

- A **one-line diagram** of the substation with breakers, transformers, and feeders drawn schematically
- **Status indicators** colored by state (closed = green, open = red, tripped = flashing)
- **Analog measurements** for bus voltage, feeder current, MW/MVAR flow
- **Alarm summary** banner showing active alarms and acknowledged-but-unresolved alarms
- **Trend graphs** for recent history
- **Switching menus** the operator uses to dispatch commands (open this breaker, raise this regulator)

Commercial HMIs include Survalent, Wonderware, GE iFIX, ABB PCM600, and many vendor-specific systems. They are licensed software running on Windows or RHEL servers, polling field devices over DNP3, IEC 61850, or Modbus and storing data in a historian.

### The Lab's Primary HMI: `/substation`

The `/substation` panel is **a custom HMI built into the lab's web app**. It talks to the [RTAC](#rtac)'s REST API (`GET /api/state`) and renders the feeder one-line, the customer-service tile, and the alarm chain directly.

We built it custom because we needed tight control over the alarm logic - the `LOW VOLTAGE` and `RECLOSER OPEN - downstream loads lost` banners fire on the exact lab conditions we wanted to teach - and we wanted a clean kinetic-feedback view that updates within seconds of an attack. The lab's [Distribution Feeder Physics](#substation-physics) closed loop ends here: an attack mutates a [recloser](#reclosers) state, the RTAC polls it, OpenDSS recomputes the feeder, the panel re-renders the alarm chain. That is the operational consequence the labs keep referring to.

Two tabs to know:

- **Feeder One-Line** - the operator-facing summary view with alarm banners, breaker / recloser symbols, customer-service tile. This is what every Lab 2.3 / 2.3-bonus / 2.4 "Operational consequence at the HMI" callout references.
- **Electrical Detail** - the engineering-precision view with per-bus voltage, feeder current, kW / kVAR. Useful when you want to *quantify* an attack rather than read its alarm-level summary; the same view becomes evidence material in Lab 2.4.

### The FUXA Sidecar (Context, Not Load-Bearing)

The lab also runs [FUXA](https://github.com/frangoteam/FUXA) - an open-source HMI/SCADA platform - at [FUXA UI](/apps/fuxa-hmi/). Fresh US installs include a read-only RTAC Modbus monitor; existing FUXA projects are preserved. The separate `hmi_poller` also generates baseline traffic. It is the closest thing in the lab to "the HMI you'd encounter at a small utility or in a vendor demo."

:::note FUXA is contextual, not part of the exercises
None of the exercises depend on FUXA being open or correctly configured. The lab's alarm chain, decision questions, and validation chips all read from the `/substation` panel. FUXA is included so students who want to see what the open-source HMI ecosystem looks like - and how a traditional Modbus-polled HMI compares to the lab's purpose-built React panel - can spend a few minutes poking around. If FUXA looks empty or misbehaves, ignore it and use `/substation`.
:::

### Why a Custom Panel vs. Configuring FUXA

We picked the React-based panel over building the labs around a FUXA project for three reasons:

1. **Alarm-rule control.** The `/substation` panel computes alarms ("voltage out of ANSI Range A → red banner") with a few lines of TypeScript. FUXA would require building an equivalent alarm spec inside FUXA's configuration model, which is opaque to students reading the lab source.
2. **Source-controlled reproducibility.** Every byte of the `/substation` UI lives in `frontend/components/substation-panel*.tsx` and can be diff-reviewed in PRs. FUXA's project state lives in a SQLite database; changes are awkward to review.
3. **Reaction time.** The `/substation` panel polls the RTAC every couple of seconds and re-renders within ~100 ms. It is responsive to attacks in a way that feels live. FUXA's Modbus polling cycle adds a second or two of additional latency; the labs are tighter without it.

The trade-off is that the `/substation` panel is *not* a representative example of what a real OT HMI looks like architecturally - it is a single-page React app, not a config-driven HMI runtime. FUXA is closer to that reality, which is why it stays around as context.

### See Also

- [Distribution Feeder Physics for Tech Workers](#substation-physics) - what the numbers on the HMI actually mean and where they come from
- [RTAC (Real-Time Automation Controller)](#rtac) - the data source the HMI polls
- [What Outages Cost (SAIDI, SAIFI, and the Customer-Service Tile)](#outage-costs-saidi-saifi) - what the customer-service tile maps to in real-utility metrics