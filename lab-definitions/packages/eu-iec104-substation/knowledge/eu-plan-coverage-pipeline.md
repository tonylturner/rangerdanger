The EU plan-coverage pipeline carries a student's observations, design verdicts and funded actions across several authored labs. It reuses browser-saved choices to show context and tailor later instructions; it is not itself firewall state or proof that a control worked.

### Readiness and labor budget

Lab 1.3 readiness reshapes the Lab 1.4 labor model. OT engineering `STRETCHED` reduces its cap from 120h to 60h; `UNAVAILABLE` reduces it to 0h and blocks OT-touching actions. Firewall skills `VENDOR_LED` sets an 80h cap and adds 20h per DPI action; `GAP` sets a 40h cap and blocks DPI actions. A `NEGOTIATING` vendor window multiplies the vendor restriction effort by 1.5; `NOT_DISCUSSED` blocks vendor-touching actions. `UNCLEAR` risk authority means signoff pending; `DISPUTED` blocks high-impact deny actions.

The total envelope remains 500h. The planner shows original → adjusted values and the answer behind each change. **Use default budget** restores default caps, effort and eligibility for comparison; the setting is saved per package and revision. Readiness-blocked saved choices stay visible but are excluded from draft totals, coverage and Apply Your Plan, even if you change readiness without reopening Lab 1.4. Changing readiness back restores those saved choices. An already-applied plan keeps its recorded provenance and proof expectations. Budget or role overrun alone remains a warning, not an apply limit.

### Start with observations, then record design

Lab 1.2, `eu-baseline-assessment`, records passive findings and a separate decision about latent reachability. Its `enterprise-to-field`, `enterprise-to-station`, `vendor-to-station`, `non-rtu-to-field` and `unauth-control` answers describe what was observed in the exercise window. An unobserved flow is not thereby denied.

Lab 1.3, `eu-segmentation-requirements`, displays those values in read-only findings panels, then asks for new design verdicts using several of the same decision IDs. The answers remain distinct: storage keys include both the scenario ID and decision ID, so an observation in Lab 1.2 does not overwrite a requirement chosen in Lab 1.3. The separate `latent-exposure-confirmed` answer also carries active probe context forward. Keep observation, intent and measured enforcement separate; the [policy comparison](#eu-policy-comparison) explains why reachability and allowed-port policy do not establish command authority.

### Fund the EU action catalog

`package.yml` binds the baseline, requirements, planning, implementation, hardening, vendor and validation roles to their EU scenario IDs. Lab 1.4, `eu-remediation-planning`, reads the Lab 1.3 verdicts and the four readiness answers (`ot-eng-availability`, `fw-admin-skills`, `vendor-change-window` and `risk-acceptance-authority`). Its catalog and mappings come from this package's `curriculum/`.

### Compile and apply one plan

Enforceable actions are `restrict-enterprise-to-vendor`, `block-enterprise-to-field`, `block-enterprise-to-station`, `restrict-vendor-to-station`, `block-vendor-to-field`, `pin-rtu-to-field` and `tighten-firewall-objects`. Each removes its deferred Weak allow when selected. EU selected slots emit no extra rule. Six required allows and four logged diagnostic denies remain in every plan.

EU has no detection-only action. `modbus-dpi`, `iec104-dpi`, `improve-logging` and all organizational actions are planning-only: they emit no firewall change. Required denies always log. The EU policy forbids ICS keys and disables DPI. **The L4 source pin enforces; DPI detects and logs.** EU does not demonstrate or promise semantic enforcement.

An empty, planning-only or partial plan is valid. Budget overrun is a warning only. Deferring an enforceable action retains its Weak exposure; required operational flows cannot be deferred.

Apply Your Plan sends package ID, revision and action IDs to the backend. One compiled result supplies the configuration, ordered rule table and expected TCP matrix. The implementation templates retain CR-22 and the operational checks; the fresh-session reconnect remains in Phase 4. Save **Export plan record** beside **Export compiled config**; the record contains package ID, revision, label, action IDs and policy hash. Check `GET /api/firewall/active` to confirm what is running. Named-policy applies and running-hash divergence clear applied-plan provenance.

### Follow the applied target in later labs

Lab 2.3 judges an applied plan against its expected results, with Hardened as the comparison. Deferring `block-enterprise-to-station` leaves Kali→RTU open; the breaker command still lands. In the bonus lab, `restrict-enterprise-to-vendor` owns RDP/VNC and `block-vendor-to-field` owns the field hop. Test both independently. Restore normal device state, GI, supply and operator visibility for every plan.

Coverage panels compare saved selections with design requirements. They do not probe the running firewall. Prevention mappings use enforceable actions only; required logged denies need no logging selection. A LOG-only requirement has no selectable live implementation. Commands on an allowed CC session and same-subnet paths remain residual risks.

Lab 2.4 has two results across 37 TCP tuples. Plan proof checks this plan with zero skipped and a stable recorded plan hash. Hardened acceptance requires 10/10 authorized and 27/27 unauthorized denied, zero skipped, capture and stable hash. Confirm a healthy HMI separately. A partial plan can pass plan proof and show prevention gaps against Hardened. Manual and named policies have no applicable plan proof and use Hardened expectations. See [evidence lifecycle](#eu-evidence-lifecycle) and [acceptance handover](#eu-acceptance-handover).

### Persistence and reset boundaries

Curriculum state is stored in the student's browser under `rd:<package-id>:<revision>:<name>`. Use the active package revision rather than a copied version number. A decision is keyed by scenario and decision ID; the remediation plan is stored separately from each lab's completed steps, notes and command log. A later package revision uses a different storage scope, so old answers are not silently treated as answers to the new curriculum.

Reset Lab changes simulator state and restores the package firewall baseline; it does not clear browser-saved decisions or the remediation plan. Reset Progress clears only the current lab's completed steps, notes and command log. Neither should be mistaken for a curriculum-wide reset. Browser storage may also be unavailable, in which case answers may not persist across views or reloads.

Implementation: package `package.yml` and `curriculum/`; frontend `lib/curriculum-storage.ts`, `lib/remediation-plan.ts`, `lib/requirement-coverage.ts` and `components/scenario-description-blocks.tsx`.
