Reliability indices make customer interruptions comparable across a reporting period, but they are not measures of electrical state, cyber attribution or economic damage. This article introduces the common indices and shows how to keep them separate from the EU range's modeled process evidence.

### Three reliability indices

- **SAIDI** (System Average Interruption Duration Index) is the total interruption duration experienced by customers in the reporting scope divided by the number of customers served in that scope. It is usually expressed as time per customer for a stated reporting period.
- **SAIFI** (System Average Interruption Frequency Index) is the number of customer interruptions divided by the number of customers served. It is expressed as interruptions per customer for the period.
- **CAIDI** (Customer Average Interruption Duration Index) is the average duration per customer interruption: total customer interruption duration divided by the number of customer interruptions. It equals SAIDI divided by SAIFI when both use the same period, scope and interruption population, and SAIFI is nonzero.

One feeder event can interrupt many customers, so it can contribute many customer interruptions. If supply is restored and then interrupted again, the reporting methodology may count another interruption. The precise rules matter: national and sector methods can differ in interruption-duration thresholds, treatment of planned interruptions and exceptional events, customer scope and reporting period. Check the applicable DSO and national methodology before comparing published figures. No single benchmark in this article should be treated as an EU-wide target.

### A transparent arithmetic example

Suppose, purely for illustration, that a reporting area serves 20,000 customers and one qualifying event interrupts 500 of them for two hours. If there are no other interruptions in the example period:

- Customer interruption duration is 500 × 2 = 1,000 customer-hours.
- The event contributes 1,000 ÷ 20,000 = 0.05 hours, or 3 minutes, to SAIDI.
- It contributes 500 ÷ 20,000 = 0.025 interruptions per customer to SAIFI.
- Its average interruption duration is 1,000 customer-hours ÷ 500 customer interruptions = 2 hours (CAIDI).

These are arithmetic examples, not EU range telemetry, actual customer counts or a forecast for a real feeder. A proper annual index aggregates all qualifying interruptions under the chosen reporting rules and denominator.

### Reliability is not the same as outage cost

SAIDI and SAIFI describe interruption duration and frequency; neither assigns a monetary value to an event. Economic impact depends on which customers are affected, how long supply is lost, when it happens, the activity interrupted, available backup and the valuation method. Critical services also have safety and continuity consequences that cannot be reduced to one universal price.

An assessment may distinguish customer interruption impacts from the DSO's restoration and operational costs. Use local, current assumptions and disclose the affected customer groups and method. Do not transfer US agency tools, dollar estimates or regulatory incentives into an EU case without independent validation. Do not infer a common EU reliability metric or interruption-cost rate from NIS2 or the electricity Network Code on Cybersecurity; see the [DSO regulatory context](#eu-dso-regulatory-context) for their scope and jurisdiction caveats.

### Connect a network event to the physical outcome carefully

An outage calculation needs evidence of an interruption, its affected customers and its duration. A denied connection or firewall event alone proves none of those. Distinguish loss of supply from loss of operator visibility, interrupted automatic control and a gap in the archive. In the EU model, for example, IEC 104 observation can become stale while local protection changes breaker state; the [modeled outage and voltage consequences](#eu-outage-voltage-consequences) explain why topology, fault location and observation quality must be considered separately.

The range's feeder cases illustrate process behavior under stated assumptions; they are not a full utility study, actual customer-impact forecast or reported SAIDI/SAIFI result. Do not infer a customer count, outage duration or avoided cost from a simulated switch position or a packet-level test. For cyber-risk review, state the observed process consequence and its evidence separately from the reliability metric that a DSO might calculate from validated customer and event records.

When documenting a test, pair the claim with the operating baseline, event window, relevant device/process evidence and observation quality. The [security objectives](#eu-security-objectives) separate supply, supervision and regulation; the [acceptance and handover guidance](#eu-acceptance-handover) describes how to retain evidence and state residual uncertainty. Neither a successful segmentation test nor a green validator is itself proof of reliability improvement or regulatory compliance.
