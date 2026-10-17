The EU plan-coverage pipeline carries a student's observations, design verdicts and funded actions across several authored labs. It reuses browser-saved choices to show context and tailor later instructions; it is not itself firewall state or proof that a control worked.

### Start with observations, then record design

Lab 1.2, `eu-baseline-assessment`, records passive findings and a separate decision about latent reachability. Its `enterprise-to-field`, `enterprise-to-station`, `vendor-to-station`, `non-rtu-to-field` and `unauth-control` answers describe what was observed in the exercise window. An unobserved flow is not thereby denied.

Lab 1.3, `eu-segmentation-requirements`, displays those values in read-only findings panels, then asks for new design verdicts using several of the same decision IDs. The answers remain distinct: storage keys include both the scenario ID and decision ID, so an observation in Lab 1.2 does not overwrite a requirement chosen in Lab 1.3. The separate `latent-exposure-confirmed` answer also carries active probe context forward. Keep observation, intent and measured enforcement separate; the [policy comparison](#eu-policy-comparison) explains why reachability and allowed-port policy do not establish command authority.

### Fund the EU action catalog

`package.yml` binds the baseline, requirements, planning, implementation, hardening, vendor and validation roles to their EU scenario IDs. Lab 1.4, `eu-remediation-planning`, reads the Lab 1.3 verdicts and the four readiness answers (`ot-eng-availability`, `fw-admin-skills`, `vendor-change-window` and `risk-acceptance-authority`). Its catalog and mappings come from this package's `curriculum.yml`.

The catalog distinguishes changes to the reference boundary—such as `block-enterprise-to-field`, `block-enterprise-to-station`, `restrict-vendor-to-station` and `pin-rtu-to-field`—from logging, conceptual reviews and future work. The [change plan](#eu-change-resource-plan) is the companion for roles, rollback and acceptance; a funded action or green coverage summary is not acceptance evidence.

The catalog also names `modbus-dpi` and `iec104-dpi` for planning. They are not live controls: the EU package sets `ics_rules: forbidden`, the improved reference policy has `dpiEnabled: false`, and the planner labels semantic actions “planned, not enforceable in this range yet.” The current boundary is L4 source/port filtering; it does not inspect Modbus function codes or IEC 104 commands, ASDUs or IOAs. Do not interpret a selected semantic action as enabled inspection.

### See intent in later labs

The implementation lab, `eu-firewall-implementation`, shows the baseline findings and design verdicts side by side. Its package binding maps the `implement-rules`, `test-positive` and `test-negative` steps to plan-aware content. The plan may shape eligible source/port rules and the validation instructions, but the applied policy and the actual positive and negative tests remain separate evidence.

`:::plan-coverage` panels in planning, hardening (`eu-hardening-configurations`) and validation (`eu-validation-evidence`) compare saved actions with the saved design verdicts. The renderer reports mapped work as covered, partial, a gap or not applicable. “Not applicable” can mean no verdict, an `ALLOW` design choice, or a selected semantic action that this range cannot enforce. A panel describes the authored mapping and learner selections; it does not probe the firewall or certify the running configuration. For proof and retention boundaries, use the [evidence lifecycle](#eu-evidence-lifecycle) and [reset acceptance](#eu-reset-acceptance) guidance.

### Persistence and reset boundaries

Curriculum state is stored in the student's browser under `rd:<package-id>:<revision>:<name>`. For the checked-in EU package, the current revision is 3. A decision is keyed by scenario and decision ID; the remediation plan is stored separately from each lab's completed steps, notes and command log. A later package revision uses a different storage scope, so old answers are not silently treated as answers to the new curriculum.

Reset Lab changes simulator state and restores the package firewall baseline; it does not clear browser-saved decisions or the remediation plan. Reset Progress clears only the current lab's completed steps, notes and command log. Neither should be mistaken for a curriculum-wide reset. Browser storage may also be unavailable, in which case answers may not persist across views or reloads.

Implementation: package `package.yml` and `curriculum.yml`; frontend `lib/curriculum-storage.ts`, `lib/remediation-plan.ts`, `lib/requirement-coverage.ts` and `components/scenario-description-blocks.tsx`.
