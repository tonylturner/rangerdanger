Labs 1.3 and 1.4 record requirements and an approved change request. Lab 2.2 teaches manual rule authoring. Optional catch-up **Apply Your Plan** sends eligible action IDs, package ID and revision to compile and apply a policy. A saved selection alone is not running state.

### Readiness and labor budget

Lab 1.3 readiness reshapes the Lab 1.4 labor model. OT engineering `STRETCHED` reduces its cap from 120h to 60h; `UNAVAILABLE` reduces it to 0h and blocks OT-touching actions. Firewall skills `VENDOR_LED` sets an 80h cap and adds 20h per DPI action; `GAP` sets a 40h cap and blocks DPI actions. A `NEGOTIATING` vendor window multiplies the vendor restriction effort by 1.5; `NOT_DISCUSSED` blocks vendor-touching actions. `UNCLEAR` risk authority means signoff pending; `DISPUTED` blocks high-impact deny actions.

The total envelope remains 500h. The planner shows original → adjusted values and the answer behind each change. **Use default budget** restores default caps, effort and eligibility for comparison; the setting is saved per package and revision. Readiness-blocked saved choices stay visible but are excluded from draft totals, coverage and Apply Your Plan, even if you change readiness without reopening Lab 1.4. Changing readiness back restores those saved choices. An already-applied plan keeps its recorded provenance and proof expectations. Budget or role overrun alone remains a warning, not an apply limit.

### Action kinds and deferred exposure

Enforceable actions change L4 reachability. These are `restrict-enterprise-to-vendor`, `block-enterprise-to-ot`, `block-enterprise-to-field`, `restrict-vendor-to-ot`, `block-vendor-to-field` and `pin-rtac-to-field`.

Detection-only actions are `modbus-dpi`, `dnp3-dpi` and `improve-logging`. They change no TCP verdict. **The L4 source pin enforces; DPI detects and logs.** All other actions are planning-only. They emit no firewall change and deploy no sensor or broker.

An empty, planning-only or partial selection can be saved. With no effective selection, a manual-policy check uses the full Hardened reference; it does not approve leaving Weak in place. Budget overrun is a warning only. A deferred enforceable action retains its Weak exposure. Required RTAC, HMI and NTP operations remain in every plan. The full enforceable and detection selection equals Hardened.

### From selections to running state

`:::decision` records an answer. `:::findings-panel` displays earlier answers. `:::plan-coverage` compares selections with requirements; it does not probe the firewall. The implementation rule table and tests come from the backend compiler, not the coverage panel.

Apply Your Plan imports the compiled configuration and records package revision, action IDs and policy hash. Download **Export compiled config** and its companion **Export plan record**, which contains the package ID, revision, label, action IDs and policy hash. Check `GET /api/firewall/active` to confirm what is running. A named-policy apply or running-hash change clears plan provenance. Checks use the applied plan first, otherwise the saved change request, otherwise the full Hardened reference. An empty effective selection sends no request. Weak-identical manual policies still fail.

Lab 2.3 compares the attempt with the approved scope. Deferring `block-enterprise-to-field` leaves the Kali command path open. In the bonus lab, `restrict-enterprise-to-vendor` owns RDP/VNC and `block-vendor-to-field` owns the field hop. Test each link independently, then restore normal operations for every plan.

### Hand test record

Lab 2.4 starts with the approved requirements and change request. Write source-specific positive and negative tests, collect interface-attributed boundary packets and fresh operator readings, then sign the test record. Current-system cross-checks do not grade that record. A partial scope leaves named exposures for the risk owner; a successful TCP test does not establish command authorization.

### Reset boundaries

Student records are scoped to package and revision. Reset Progress clears the current exercise's completed steps, notes and command log, not other exercises' decisions or the saved plan. Reset Lab restores simulator and Weak firewall state, not browser records. After reset, restore the intended policy with one-level `rollback` if no other commit followed the reset; otherwise import the saved Lab 2.2 backup and commit it. Apply Your Plan is the catch-up route. Then collect a new evidence window.
