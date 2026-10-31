Labs 1.3 and 1.4 record design verdicts and action selections. **Apply Your Plan** sends those action IDs, the package ID and revision to the backend. The backend compiles one policy, rule table and expected TCP matrix. A saved selection alone is not running state.

### Readiness and labor budget

Lab 1.3 readiness reshapes the Lab 1.4 labor model. OT engineering `STRETCHED` reduces its cap from 120h to 60h; `UNAVAILABLE` reduces it to 0h and blocks OT-touching actions. Firewall skills `VENDOR_LED` sets an 80h cap and adds 20h per DPI action; `GAP` sets a 40h cap and blocks DPI actions. A `NEGOTIATING` vendor window multiplies the vendor restriction effort by 1.5; `NOT_DISCUSSED` blocks vendor-touching actions. `UNCLEAR` risk authority means signoff pending; `DISPUTED` blocks high-impact deny actions.

The total envelope remains 500h. The planner shows original → adjusted values and the answer behind each change. **Use default budget** restores default caps, effort and eligibility for comparison; the setting is saved per package and revision. Readiness-blocked saved choices stay visible but are excluded from draft totals, coverage and Apply Your Plan, even if you change readiness without reopening Lab 1.4. Changing readiness back restores those saved choices. An already-applied plan keeps its recorded provenance and proof expectations. Budget or role overrun alone remains a warning, not an apply limit.

### Action kinds and deferred exposure

Enforceable actions change L4 reachability. These are `restrict-enterprise-to-vendor`, `block-enterprise-to-ot`, `block-enterprise-to-field`, `restrict-vendor-to-ot`, `block-vendor-to-field` and `pin-rtac-to-field`.

Detection-only actions are `modbus-dpi`, `dnp3-dpi` and `improve-logging`. They change no TCP verdict. **The L4 source pin enforces; DPI detects and logs.** All other actions are planning-only. They emit no firewall change and deploy no sensor or broker.

An empty, planning-only or partial plan is valid. Budget overrun is a warning only. A deferred enforceable action retains its Weak exposure. Required RTAC, HMI and NTP operations remain in every plan. The full enforceable and detection selection equals Hardened.

### From selections to running state

`:::decision` records an answer. `:::findings-panel` displays earlier answers. `:::plan-coverage` compares selections with requirements; it does not probe the firewall. The implementation rule table and tests come from the backend compiler, not the coverage panel.

Apply Your Plan imports the compiled configuration and records package revision, action IDs and policy hash. Download **Export compiled config** and its companion **Export plan record**, which contains the package ID, revision, label, action IDs and policy hash. Check `GET /api/firewall/active` to confirm what is running. A named-policy apply or running-hash change clears plan provenance. Manual and named policies are judged against Hardened.

Lab 2.3 uses the applied plan's expected results. Deferring `block-enterprise-to-field` leaves the Kali command path open. In the bonus lab, `restrict-enterprise-to-vendor` owns RDP/VNC and `block-vendor-to-field` owns the field hop. Test each link independently, then restore normal operations for every plan.

### Two report results

Lab 2.4 tests all 21 TCP tuples. Plan proof checks this plan with 0 skipped and a stable plan hash. Hardened acceptance requires authorized 8/8 and unauthorized denied 13/13, 0 skipped, capture and stable hash. Confirm a healthy HMI separately. A partial plan can pass plan proof while showing prevention gaps against Hardened. Neither result proves command authorization on an allowed session.

### Reset boundaries

Student records are scoped to package and revision. Reset Progress clears the current exercise's completed steps, notes and command log, not other exercises' decisions or the saved plan. Reset Lab restores simulator and Weak firewall state, not browser records. Reapply the intended plan and collect a new evidence window after reset.
