The customer-service tile on the lab's Feeder HMI shows "ALL CUSTOMERS WITHOUT POWER" or "N kW serving M customers" depending on the feeder state. This is not just flavor. It is the lab's representation of the metrics utilities actually report to their regulators - and the metrics a cyber attack against a substation would actually move.

### The Standard Reliability Metrics

Utilities report distribution-system reliability using two widely-used indices:

- **SAIDI** (System Average Interruption Duration Index) - the *average* total outage time per customer per year. If a utility serves a million customers and the total outage-customer-minutes across the year was 100 million, SAIDI = 100 minutes per customer.
- **SAIFI** (System Average Interruption Frequency Index) - the *average* number of outage events per customer per year. SAIFI = 1.5 means the average customer experiences 1.5 outage events per year.

A third commonly-tracked metric is **CAIDI** (Customer Average Interruption Duration Index) - the average outage duration per outage event = SAIDI / SAIFI. It is what your average outage feels like to a customer, in minutes.

These are reported to **state-level public utility commissions** in the US (each state has its own) and equivalent regulators in other countries. They are public information. They are tracked over time. They feed rate-case decisions, performance-based ratemaking, and utility executive bonuses. They are not abstract.

### Typical Values

For a North American distribution utility:

- SAIDI around **90 to 150 minutes per year** is normal in good weather years.
- Major-event days (hurricanes, ice storms) push SAIDI higher; sometimes the regulator excludes them from the official reporting figure to avoid penalizing for weather.
- The best-performing utilities in fair-weather climates run SAIDI under 60 minutes. Long-rural utilities in harsh climates can be 200+.
- SAIFI typically runs **0.7 to 1.5 events per year per customer**.

A single substation event that takes 1,000 customers offline for 60 minutes contributes 60,000 customer-minutes - visible in the SAIDI calculation. A whole feeder event taking 10,000 customers offline for 4 hours is 2.4 million customer-minutes - the kind of event that triggers an executive after-action review.

### How Outage Costs Get Quantified

Beyond the regulatory reporting, utilities calculate the economic cost of outages for cost-benefit analysis of grid investments. The standard reference is the **DOE Interruption Cost Estimator (ICE) Calculator** (Berkeley Lab). It uses customer-survey-derived willingness-to-pay-to-avoid-outage data to produce per-event cost estimates:

- A 1-hour outage for a typical small commercial customer: roughly **\$200 to \$500**
- A 1-hour outage for a medium industrial customer: **\$5,000 to \$30,000** depending on production sensitivity
- A 1-hour outage for a hospital or fire station: **incalculable in the direct dollar sense**, which is why the lab calls these out separately on the customer-service tile

For a 1,000-customer-hour outage event across a typical mix, the all-in economic cost is in the **\$200,000 to \$1,000,000** range. This is before regulatory penalties.

### Cyber Attacks vs. Weather as Outage Causes

For context: about **70% of customer-minutes of outage** at a typical distribution utility comes from weather (storms, ice, lightning, vegetation contact). The next-largest categories are equipment failure, animal contact, and human error. Cyber attack is, today, a tiny fraction of the actual outage tally.

But the cyber-attack risk profile is different from the weather risk profile in two important ways:

1. **Concurrency.** A targeted cyber attack can take down many substations *simultaneously*, in a way that weather events at this scale (regional ice storm, hurricane) usually require luck or a multi-day weather pattern. A coordinated Industroyer-class attack could in principle affect tens of substations in a window of minutes.
2. **Cascading.** A weather event is bounded by the geography of the weather. A cyber event is bounded by the attacker's reach - which, if the IT-OT boundary is porous, can be region-wide or larger.

These two characteristics are why utility executives and regulators care about cyber attacks against distribution far in excess of the historical-outage-share argument. The risk is not what cyber attacks *have* done; it is what cyber attacks *could* do under a coordinated campaign.

### How the Customer-Service Tile Maps to All This

The customer-service tile is the lab's representation of the operator's awareness of outage impact. When the tile flips from "120 kW serving ~200 customers" to "ALL CUSTOMERS WITHOUT POWER," that is the operational visibility a real control-room operator has. The lab's "hospital and fire station without power" annotation is the *critical-load* category - the loads whose outage triggers an immediate emergency response.

When you tell a workshop attendee "this single packet caused a complete feeder outage," you can put a dollar figure on it: 200 customers × 1 hour at typical mix ≈ **\$50,000 to \$200,000** of economic impact, plus the regulatory reporting visibility, plus the (incalculable) critical-services consequences. The cyber-attack-against-substation threat is not theoretical. The lab does not simulate the economics directly, but the customer-service tile is the bridge to that framing.