Labs 1.3 and 1.4 record design verdicts and action selections. **Apply Your Plan** sends those action IDs, the package ID and revision to the backend. The backend compiles one policy, rule table and expected TCP matrix. A saved selection alone is not running state.

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
