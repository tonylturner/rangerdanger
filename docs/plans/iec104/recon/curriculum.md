# IEC104 recon: curriculum, validation, frontend and state

Snapshot: `iec104`, HEAD `b65374380cd550793bd214016ab732c4095b66ea` (v0.1.34).
Read-only source inspection; only this report written. Paths below are repository-relative.
**V** = verified from source, **I** = inference/recommendation, **Q** = unresolved decision.
No live stack, browser, smoke gate, unit suite, or build was run; execution counts below are static inventories, not passing tests.

## Ranked findings / claims 5–9

1. **P0 / claim 9 CONFIRMED — curriculum ownership is not package-safe.**
   **V:** `backend/internal/labs/loader.go:53–67` loads one shared `scenarios/*.yml` list and feeds it to every top-level template.
   `loader.go:123–132,155–171` imports inline and directory scenarios and upserts by scenario ID alone, assigning the current template ID.
   `backend/internal/models/models.go:51–63` makes `Scenario.ID` the sole primary key; `LabTemplateID` is only a field.
   **I:** if two valid templates share these IDs, the later import transfers the rows rather than creating two owned copies; differing nodes can instead fail validation before import (`backend/internal/labs/validate.go:99–117`). A second top-level YAML is insufficient.
2. **P0 / claim 6 CONFIRMED — all scenario validation depends on a single RTAC, including unknown IDs.**
   **V:** `backend/internal/server/scenario_validate.go:33–75` fetches RTAC state and audit before a seven-ID switch; unknown IDs receive electrical fallback validation without a scenario DB lookup.
   `backend/internal/server/substation.go:21–24` always resolves `http://rtac-sim:8080`; no selected package/instance argument exists.
   **I:** replacing only DNP3 commands in YAML cannot make a non-substation package validate, and namespaced IDs would silently select the wrong fallback unless dispatch changes.
3. **P0 / claim 8 CONFIRMED — “evidence” and verification are another hardcoded deployment.**
   **V:** `backend/internal/server/validation_report.go:39–58` embeds 19 container/IP/port/expected-result rows; `:139–145,240–244,349–350` embeds field capture hosts, subnet interpretation, storage path, and RTAC/GPS approved sources.
   `backend/internal/server/pcap.go:574–597,612–615` embeds HTTP/Modbus/DNP3 generator commands; `backend/internal/server/reset.go:24–40,67–81` embeds four-device reset recipes and shared capture deletion.
   **I:** these need package-owned fixtures/capabilities, not just new exercise text.
4. **P1 / claim 7 PARTLY — storage primitives accept arbitrary IDs, but their namespace and the business mappings assume one workshop.**
   **V:** `frontend/lib/decision-storage.ts:18–25`, `scenario-runner-storage.ts:2–7`, `remediation-plan.ts:5–33`, `use-firewall-track.ts:23–27` use scenario-only decisions/progress plus global plan/track keys.
   `frontend/lib/requirement-coverage.ts:39–89`, `remediation-to-rules.ts:26–216,402–645` embed US requirements, actions, addresses, protocol predicates, zones and appliance interfaces.
   **I:** namespacing scenario IDs alone does not isolate the global plan or track.
5. **P1 / claim 5 PARTLY — frontend explicitly selects the shipped template; YAML is extensible content, not a package-neutral execution schema.**
   **V:** frontend literal selectors: `components/exercise-list.tsx:39–40`, `components/scenario-list.tsx:8–9`, `app/exercises/[id]/page.tsx:20`, `lib/pdf-download.tsx:32,47` (seven occurrences including query keys).
   `backend/internal/labs/types.go:34–114` allows Markdown, structured actions, decisions and TCP probes, but `validate.go:14–30,110–111,149–177` fixes device/check vocabulary and policy names.
   **I:** content rendering can be reused; package routing, validation, rule generation and progress ownership cannot yet.

## Curriculum schema, seeding and persistence

- **V:** `backend/internal/labs/types.go:3–32`: lab template = ID/name/description/firewall config/networks/nodes/inline scenarios. Nodes expose ID/type/name/networks/primary IP/container, not capabilities, protocol point maps, UI metadata or per-interface addresses.
- **V:** `types.go:34–62`: scenario = ID/name/summary/description/order/nodes/tags/steps/estimated minutes; step = title/description/expected_config/action/node. No stable step ID, prerequisite graph, completion criteria or validator declaration.
- **V:** `types.go:64–114`: actions are command/sequence/check/firewall/decision/probe; decision payload carries budget, role capacity and action effort/roles/tags. TCP probes support per-target source override, IPv4 host/port/note/outcome.
- **V:** `validate.go:85–119,180–209,213–273` validates IDs/name/order/node references, action types, TCP target bounds, device command names, and decision budgets/roles/actions. It does not parse narrative decision references or validate runtime protocol capabilities. `yaml.Unmarshal` is non-strict (`loader.go:39,81`).
- **V:** required-description/steps assertions in `docs/lab-authoring.md` are stronger than actual `validate.go:85–119`; empty descriptions/step arrays and filename/ID equality are not checked there. `expected_config` actually accepts only absent/weak/hardened (`:110–111`), although authoring docs also advertise improved.
- **V:** inline scenarios alone populate `DefaultScenarios` and topology's scenarios (`loader.go:93–107`), whereas standalone files also become DB rows (`:128–132`). Shipped topology has `scenarios: []` (`lab-definitions/substation-segmentation.yml:159–168`), so its stored default list is empty despite seven listable exercises.
- **V:** every template records literal `ComposeFile: "docker-compose.yml"` (`loader.go:109–117`). Startup seeds with warning-only failure (`backend/cmd/server/main.go:28–31`); no transaction covers all templates (`loader.go:64–68,119–147`).
- **V:** stale scenarios are deleted per template, only if valid IDs are nonempty (`loader.go:134–149`). Thus an empty curriculum cannot clear its last rows. Removed template rows are not pruned by this loader. These matter when packages change or seeding partially fails.
- **V:** SQLite auto-migrates `lab_templates`, `lab_instances`, `node_definitions`, `scenarios`, `scenario_runs`, `telemetry_points` via GORM models (`backend/internal/db/db.go:22–36`; `models/models.go:8–84`). String primary keys exist on each. Templates/scenarios hold JSON as strings, not normalized steps/decisions.
- **V:** `models.go:67–74` stores run ID/scenario ID/lab instance ID/status/events. Browser decisions, plans, notes and completed step indexes are separate from this run record; `frontend/components/scenario-runner.tsx:72–90,148–159,343–347` stores/marks them locally.
- **V:** list supports `lab_template_id` filtering (`backend/internal/server/scenarios.go:12–22`); get/execute use global ID (`scenarios.go:41–48`; `scenario_execute.go:49–59`). Starting a run only checks that scenario exists and instance ID is nonempty, not that instance belongs to its template (`scenarios.go:51–81`). Create uses `Save` with the request model, not `ValidateLab` (`:25–38`).
- **V:** `BaselineGridState` is declared (`types.go:48–52`) but omitted from persisted Scenario and loader assignment (`models.go:51–63`; `loader.go:159–170`); source search found no execution/reader implementation. It is not a working baseline mechanism.

## All validators and RTAC state interfaces

All references in this table are to `backend/internal/server/scenario_validate.go`.

| Validator / lines | Actual checks and coupling |
|---|---|
| baseline-assessment / 268–342 | PCAP existence; relay/recloser/regulator comms; breaker/recloser closed; reclose enabled (warn if off); critical/general loads energized; 114–126 V ANSI range. Capbank comms not required. |
| segmentation-requirements / 346–374 | Breaker closed; weak restored (warn otherwise); same three comms. Does not evaluate design decisions. |
| remediation-planning / 428–447 | Breaker/recloser/critical load normal; always appends “Plan saved” pass. Server cannot verify browser plan. |
| firewall-implementation / 451–509 | Config is anything other than weak; three comms required, capbank mentioned if healthy; protection/critical load preserved; ANSI voltage range. No actual deny/allow probes or policy predicates. |
| hardening-configurations / 518–580 | Tap -3…3; critical voltage 114–126 if positive; recloser closed/reclose enabled; improved/custom; warns on executed non-RTAC set_tap or enterprise crob_reclose/disable_reclose audit entries. Missing/zero voltage contributes no voltage check. |
| vendor-rdp-compromise / 99–138 | Recloser/reclose; both loads energized; improved/custom else warn. Does not prove RDP/VNC blocked or vendor conduit closed. |
| validation-evidence / 378–424 | improved/custom; breaker/recloser/critical load; three comms; reclose enabled. Does not inspect exported policy, report result, fresh PCAP or browser reflection. |
| generic / 584–603 | Breaker/critical load plus always-pass config display; still electrical/RTAC-specific, not a generic validator. |

- **V:** state/audit fetches (`:142–172`) use 5 s HTTP clients, JSON maps, no HTTP status check or state freshness check; audit fetch failure becomes nil (`:43–47`). Helpers default missing/wrongly typed values to false/0 (`:174–232`).
- **V:** audit non-RTAC classification uses source string substrings rtac/reset/operator (`:682–697`) and fixed enterprise zone strings (`:253–263,568–570`). **I:** nil audit can masquerade as “no unauthorized writes”; historical audit can warn about pre-hardening attacks. Neither is scoped by package/run/time window here.
- **V:** `scenario_execute.go:261–350` fetches RTAC even for a policy-only check; fixed checks are breaker/load/recloser/reclose/voltage/firewall_config. It treats improved as also custom, but does not implement a step-level expected_config check.
- **V:** `scenario_execute.go:138–169,205–225` implements HTTP control via RTAC and preemptively denies unauthorized sources only for activeConfig=improved, using fixed RTAC IP and zone-prefix labels. This is not proof that the firewall dropped an on-wire attack; custom policies bypass that precheck.
- **V:** TCP probe action is comparatively reusable (`scenario_probe.go:13–84`): source from node, literal IPv4/port, reachable vs blocked/error; fast refusal counts as reachable. It still resolves nodes through fixed workshop context and has a rangerdanger container-name fallback (`:44–52`).
- **V:** frontend runner polls state/firewall/audit every 3 s for **every** exercise (`frontend/components/scenario-runner.tsx:266–285`), even planning labs; process panel similarly reads fixed device/electrical fields (`components/substation-panel.tsx:25–50`).
- **I:** shared substation state can survive DNP3→IEC104 transport replacement if its process model remains identical. Regional process settings are a separate concern; the currently fixed ANSI thresholds are not IEC104 semantics.

## Evidence, capture, probes, reset and automatic tests

- **V:** `validation_report.go:39–58` = 8 authorized + 11 unauthorized TCP probes, expected against hardened policy regardless of student's selected remediations. `:76–111` correctly fails empty/skipped matrices; no source container means not a pass.
- **V:** report includes timestamp, active config/source and truncated firewall hash, matrix, PCAP path/summary (`:127–145,187–206,286–356`). It has no package/revision/scenario/run identity. PCAP source listing is informative Markdown, not a DPI command test or an additional pass/fail criterion (`:176–190,240–250,349–350`).
- **V:** report capture filter lists relay/recloser/regulator/capbank IPs and host path `data/firewall/captures`; forwarded source interpretation assumes 10.40.40 and RTAC/GPS 10.30.30 addresses (`:139–145,240–244,349–350`). Standard capture endpoint is more generic: receives duration/name/filter, intentionally leaves containd interfaces empty to fall back to `tcpdump -i any` (`backend/internal/server/pcap.go:22–64,314–340`). It is still tied to one firewall and `/tmp/capture.pcap` (`:441–455`).
- **V:** baseline success accepts nonempty baseline.pcap, any containd API capture, or in-memory FileReady (`scenario_validate.go:642–679`). Another capture can satisfy it; freshness/contents/run ownership are not established. The alternative disk-only helper at `:608–639` is not used by the baseline validator.
- **V:** narrative export instructions live in `lab-definitions/scenarios/firewall-implementation.yml:556–613` and `validation-evidence.yml:72–208`; UI validation panel displays response Markdown only (`frontend/components/validation-report-panel.tsx:18–24,66–84`). Curriculum PDF/workbook export is separate and explicitly selects shipped template (`lib/pdf-download.tsx:27–52`), not a persisted student-evidence bundle.
- **V:** workshop reset applies weak, sends common device commands plus tap=0, deletes shared PCAPs and wipes appliance user/session DBs (`reset.go:45–115`). It does not clear browser keys. Capture exec-start errors are ignored by UI reset (`:83–90`); test-runner reset tracks start failures (`test_runner.go:337–350`), but neither awaits deletion completion/exit status there.
- **V:** `/api/workshop/test-suite` queries **all scenarios**, not a template (`test_runner.go:174–195`); resets before/after, evaluates structured actions and applies hardened preconditions (`:199–239,274–298`). Plain description commands are not executed here. No-action/decision auto-pass is explicit (`test_runner_steps.go:18` onward; `test_runner_test.go:23–33`). A command returning the synthetic HTTP-control denial string “BLOCKED by containd” is counted as a passed step (`test_runner_steps.go:28–35`), not measured wire enforcement.
- **V:** policy reconciliation uses hash polling plus hardcoded Kali→RTAC:502 canary (`test_runner.go:56–123`). Sleep heuristics key off inject_fault/disable_reclose/set_tap (`:242–270`). This suite cannot be treated as seven end-to-end student lab executions.

## Frontend coupling inventory

### Package selectors, exercise IDs and browser state

- **V:** all production literal `substation-segmentation` query selectors are enumerated in finding 5. API `listScenarios(templateId?)` itself is reusable (`frontend/lib/api.ts:197–200`). Backend workshop graph/status is independently pinned (`backend/internal/server/workshop.go:15–21,136,156`), so fixing selectors alone is insufficient.
- **V:** `frontend/lib/exercise-nodes.ts:10–16` fixes all seven exercise IDs; `:19–42` fixes node labels and iframe URLs; `:45–60` prefers scenario-provided nodes but otherwise falls back to fixed IDs and IP/prose inference.
- **V:** policy and validation-button allowlists hardcode four lab IDs (`lib/scenario-runner-logic.ts:18–36`); title fragments choose dynamic phases/buttons (`:8–11,39–58,172–186`). Rename/translate titles and behavior can change.
- **V:** runner branches on firewall-implementation and step index 0 (`components/scenario-runner.tsx:542,569`); description renderer also branches on that ID (`components/scenario-description-blocks.tsx:708`); banner excludes four specific IDs (`components/remediation-plan-banner.tsx:29–32`).
- **V:** links back to planning/requirements are fixed (`lib/scenario-runner-logic.ts:58`; `components/scenario-description-blocks.tsx:352,432`; `components/decision-panel.tsx:137`). Upstream scenario IDs also live in YAML findings/default-from fences, not just TS.
- **V:** browser keys: `decision:<scenarioId>:<decisionId>` (`lib/decision-storage.ts:18–20`); `rd-exercise-<scenarioId>` completed numeric indexes/notes/log (`scenario-runner-storage.ts:2–7`); `rd-remediation-plan` JSON exerciseId/selectedActionIds/savedAt (`remediation-plan.ts:5–23`); `rangerdanger.firewall-track` (`use-firewall-track.ts:23–27`). Exercise cards separately read the progress key (`components/exercise-list.tsx:20`). None has package/revision/session scope.
- **V:** requirement/readiness readers always use segmentation-requirements (`lib/requirement-coverage.ts:82–89,180–218`); plan action→defended exercise mapping uses fixed IDs (`lib/remediation-plan.ts:49–92`). Browser progress reset clears only progress/notes/log, not decisions/global plan/track (`components/scenario-runner.tsx:152–159`).

### Rules, addresses, protocol names, commands

- **V:** `lib/remediation-to-rules.ts:26–63,66–216` fixes RTAC/GPS/zone addresses, baseline Modbus/DNP3/HTTP/NTP rules and remediation catalog. `:339–397` emits mbpoll/dnp3poll/curl/ssh command recipes with concrete IPs. `:402–645` separately builds appliance JSON with wan/dmz/lan1/lan2, eth0–eth3, port allowlists, DPI fields, system/TLS paths and DENY baseline.
- **V:** displayed DNP3 DPI map says all application functions and generated ICS only declares `protocol: dnp3` (`:172–188,521–535`); unlike Modbus it supplies no functionCode allowlist. **I:** do not mechanically translate this to IEC104 “command filtering”; packet/capability tests must establish the intended behavior. Generated student policy and canned improved policy are distinct contracts.
- **V:** complete production code address/protocol inventory outside knowledge prose (comments included where relevant):
  - `app/labs/page.tsx:45`; `components/activity-feed.tsx:31`; `network-console.tsx:288`; `scenario-description-blocks.tsx:610`; `scenario-runner-widgets.tsx:24,37,49`; `scenario-runner.tsx:225`.
  - `components/segmentation-view.tsx:327,464`; `substation-commands.tsx:37,54,64,75,84,94`; `substation-electrical.tsx:225,239,253,268`; `substation-one-line.tsx:166`; `topology-nodes.tsx:382`; `topology-preview.tsx:35–38,74`; `traffic-matrix-view.tsx:60–61`.
  - `lib/api.ts:358,371` protocol-specific event comments; `exercise-nodes.ts:56–60`; `network-console-data.ts:15–28,51–80,178–208`; `network-graph.ts:119–124,429–441`; `observed-flows.ts:36–80,148–161`; `remediation-plan.ts:52–76`; `remediation-to-rules.ts:26–216,339–397,429–590`; `requirement-coverage.ts:41,55,69–73,259–282`; `scenario-runner-logic.ts:92,108–114,165–167`.
- **V:** full knowledge sections are package-specific curriculum too: `lib/knowledge-content/protocols.ts:46–106,162–170`; `tool-references.ts:15–142,154–255,274–277`; `substation-equipment.ts:32–76,139–173`; `segmentation-concepts.ts:175–178,213–216,318–379`; `threats-and-practice.ts:25–45,76–94`; `lab-internals.ts:21–37,104–110,148–160,179–205,240–263`. They contain more fixed examples than the scenario files alone.
- **V:** runnable tool lists duplicated in `lib/scenario-description.ts:13`, `exercise-pdf-text.ts:55`, `scripts/lab-commands-smoke.sh:67–71`, `backend/internal/server/exec.go:12–16`. IEC104 CLI prefix must be understood by all four plus installed in intended source containers. The UI command-inference and smoke parser also duplicate source-host heuristics.

### Process and topology assumptions

- **V:** state contract is explicitly `SubstationState` (`lib/api.ts:250–282`), not a process capability schema. `components/substation-panel.tsx:46–50` pulls four named devices. Device commands/IPs/ANSI numbers fixed in widgets and command view above.
- **V:** `components/substation-electrical.tsx:24–26,55` sets 12.47 kV / 120 V / 600 A and regulator fallback 120; `substation-one-line.tsx:28–29,213–215` and `metrics-overview.tsx:58–65,98–100,123–142` encode 114–126/120 V. Runner status repeats these limits (`scenario-runner.tsx:801–802`). Load presets are in UI code (`components/load-simulator.tsx:24` onward), not scenario-owned baseline data.
- **V:** graph renderer recognizes one firewall, filters hosts to a fixed zone allowlist, and synthesizes firewall-to-zone conduits (`lib/network-graph.ts:41–70,591–617`). Purdue/SL/subnet assignments are hardcoded (`:103–124`); default labels/subnets also fixed (`:413–441`). Backend workshop graph likewise fixes four-zone order and primary network grouping (`backend/internal/server/workshop.go:35–64`).
- **V:** port labels, zone aliases/interface metadata, health mapping fixed in `lib/network-console-data.ts:5–28,109–125,167–208,310–334`; unknown 2404 currently renders tcp/2404 (`:31–40`), not IEC104. `components/topology-preview.tsx:33–78` is an entirely static US preview.
- **V:** traffic edges/matrix are configured flow fixtures, not discovered packets (`lib/observed-flows.ts:1–19,36–80`); node zone/IP maps are static (`:119–161`); device_comms boolean powers both Modbus and DNP3 liveness (`:89–101`). **I:** IEC104 must not be labeled healthy merely because HTTP/device comms are healthy. Physical truth, operator view, transport health and observed packet evidence need distinct provenance.

## Gates and validation ownership

**All NOT RUN.** These mutate a live range; read-only recon must not apply policies, trip devices, delete captures, pause physics or reset the workshop.
CI currently runs all five sequentially on its disposable stack (`.github/workflows/smoke.yml:137–190`). Do not fork five whole scripts per package: keep generic runners, package-owned fixtures and protocol/process-specific capability checks.

| Gate | Reusable mechanics | Shipped coupling / owner of IEC104 variant |
|---|---|---|
| `scripts/test-suite-smoke.sh` | Health, response schema, failed/reset failure accounting | `:51–58` pins 18 auto-pass steps and exactly 5 executed firewall-implementation steps; backend queries all scenarios and resets US process. Curriculum owner supplies package inventory/counts; backend owner scopes run/reset and keeps auto-pass visibly distinct. |
| `scripts/lab-commands-smoke.sh` | Extract narrative commands, exec, policy reconciliation/error reporting | `:64–71,97–141` fixed scenario/topology paths/tools/source inference; `:219–274` fixed Kali→RTAC:502 canary and weak/improved. Curriculum owner validates every package command, including generated TS descriptions (currently static YAML only). Runtime owner supplies canary/container/tool metadata. |
| `scripts/firewall-smoke.sh` | TCP/UDP probe verdicts, policy settle, gateway/listener checks | `:160–193,201–202,285–314,368–377` all matrix addresses/services/gateways/listeners hardcoded; zones and weak/improved pinned. Networking owner supplies separate expected package matrix preserving the US one; IEC104 2404 tests must include permitted telemetry AND forbidden commands where advertised. |
| `scripts/events-smoke.sh` | Firewall event API/JSON, polling and template normalization | `:135–159` fixed Modbus deny tuple; `:242–244` modbus-read-only template; `:282–311` Modbus FC8 bytes; `:332–366` DNP3 FC5 tool/block_flows/restore. Protocol/firewall owner supplies IEC104 wire/event/assertions. `:269–279,334–337` conditional DPI skips mean PASS is not proof those checks ran. |
| `scripts/substation-smoke.sh` | State freshness/convergence, simulator pause/recovery patterns | `:30,75–91,108–149,155–173` OpenDSS container, four devices, capbank power factor and regulator AVR. Process owner reuses for same feeder with resolved names; a non-electric package needs its own capability fixture, not an always-pass substitute. |

Unit-test split (CI entrypoints remain Go -race/-count=1 and frontend Vitest):
- **V, reusable core:** backend config/DB/models CRUD, containd HTTP client/seed schema and orchestrator fake lifecycle mechanics (`backend/internal/config/config_test.go`; `db/db_test.go:51–55`; `models/models_test.go:22–47`; `containd/client_test.go`; `orchestrator/lifecycle_test.go:30`). Fixtures may mention US names without asserting package semantics.
- **V, reusable kernels with new package contracts:** loader refresh/stale/validation tests (`labs/loader_test.go:146,191,230`), structural validation/probe tests (`labs/validate_test.go:256`), TCP result classification (`server/scenario_probe_test.go`), report tally (`server/validation_report_test.go:14–83`), test-step/reset aggregation (`server/test_runner_test.go:10,171`), PCAP filename safety (`server/pcap_test.go:27,80,103`), policy-observer/hash/transport error handling (`server/policy_observer_test.go`, `test_runner_wait_test.go`). Add cross-package duplicate local ID, empty/reseed, ownership mismatch, unknown/missing capability and missing evidence tests.
- **V, package/process/protocol fixtures:** `server/scenario_validate_test.go:97–222` US checks; `reset_test.go:22` matches reset commands against sim source; `labs/validate_test.go:304` loads shipped tree; `containd/firewall_config_test.go:25–30` and `server/firewall_compare_test.go:34,74–78` read exact US policy paths; `server/firewall_apply_test.go:79,123–176` pins canned definitions; `exec_test.go:136` checks tool list. Keep US regression plus IEC104 fixtures, not replacement of US assertions.
- **V, frontend reusable kernels:** Markdown/decision/PDF text parsing and generic storage helpers (`lib/scenario-description.test.ts:22–538`, `decision-storage.test.ts`, `exercise-pdf-text.test.ts`), knowledge search. Extend runnable tools/key scoping once, then test manifests.
- **V, frontend package fixtures:** `exercise-nodes.test.ts:8–27,60–74`, `scenario-runner-logic.test.ts:23–51`, `network-graph.test.ts`, `network-console-data.test.ts`, `remediation-to-rules.test.ts:29–181`. Refactor tests to consume package maps without weakening shipped expected values.
- **V, curriculum contracts:** `scenario-decision-graph.test.ts:29–48,127–170` loads one fixed directory and checks >=7 scenarios plus narrative references; `firewall-lab-consistency.test.ts:22–76` pins US rule IDs/IPs/ports and prose; `apply-your-plan-combinations.test.ts:17–45,73–87` enumerates 256 combinations of eight US actions with protocol predicates. Run these per package; generic shape invariants can remain shared. Combination tests prove serialization/schema shape, not dataplane enforcement.
- **V:** `frontend/vitest.config.ts:3–14` tests `lib/**/*.test.ts` in Node only: no React component/DOM tests. Therefore package selection/query-cache/progress isolation and process/topology rendering need additional test coverage or explicit manual acceptance, not a claim that existing Vitest covers screens.

## Rough curriculum reuse count

**V, reproducible static census:** seven YAMLs contain 2,589 physical lines; excluding blanks/comments: 2,047 lines / ~17,029 whitespace words. Regex hits on these lines: DNP3/20000/CROB/Direct Operate/outstation/FC03–06 = 142 (6.9%); any DNP3 or Modbus vocabulary/ports/function-code phrase = 242 (11.8%); concrete 10.x addresses = 158 (7.7%); ANSI/voltage/frequency examples = 7. Union = 331 (16.2%). Overlapping categories must not be added.

A second census parsed YAML and counted **summary, scenario/step descriptions and decision action title/why**, excluding structural/action payload fields: 1,486 nonblank narrative lines / ~15,835 whitespace words; DNP3 hits 128 (8.6%), all protocol hits 214 (14.4%), addresses 134 (9.0%), union including regional hits 292 (19.7%). These are keyword lower bounds, not semantic rewrite percentages. Wrapped surrounding explanation and attack consequences need editing too.

| File under `lab-definitions/scenarios/` | Steps | Narrative lines | DNP3 / all protocol / union lines | I: semantic reuse for same feeder, DNP3 replaced, Modbus retained |
|---|---:|---:|---:|---|
| baseline-assessment.yml (`:107–157,218–292,328–349`) | 8 | 285 | 23 / 31 / 61 | ~70–80%; topology/capture reasoning reusable, protocol mix/tools/expected observations replaced |
| segmentation-requirements.yml (`:19–150,152–200`) | 3 | 135 | 12 / 17 / 20 | ~85–90%; resource readiness/design method survives, protocol labels/decision values updated |
| remediation-planning.yml (`:79–279`) | 4 | 90 | 10 / 15 / 15 | ~80–90%; budget/roles/coverage reusable, action rationale/DPI options adapted |
| firewall-implementation.yml (`:114–158,256–369,440–555`) | 9 | 396 | 22 / 48 / 64 | ~70–80%; rule-authoring/default deny/logging/export reusable; rules/probes/DPI examples adapted |
| hardening-configurations.yml (`:4–8,71–191,293–412`) | 6 | 239 | 41 / 64 / 72 | ~40–55%; central DNP3 attack + CROBs/FC05/FC06/No-Ack/SBO semantics require real IEC104 replacement, not text substitution |
| vendor-rdp-compromise.yml (`:50–193,216–305`) | 7 | 176 | 1 / 15 / 28 | ~80–90%; RDP/VNC and vendor access chain reusable; field attack is mainly Modbus already |
| validation-evidence.yml (`:72–208,209–256`) | 3 | 165 | 19 / 24 / 32 | ~75–85%; evidence/reflection survives; probe fixtures/expected policy/export details change |

**I:** overall ~70–80% of learning structure/prose is reusable for an IEC104 adaptation of the same substation; budget ~20–30% semantic reauthoring/revalidation, higher in Lab 2.3. This excludes extra knowledge base and dynamic TS-generated prose. A genuinely European electrical process/localization change increases that work independently of protocol choice. 40 total steps, 85 stated guided minutes (73 without optional bonus); order labels are deck labels, not “Lab 1…7” (`substation-segmentation.yml:159–167`; each scenario `:2,5`).

## Smallest package ownership model (proposal, not implementation)

**I:** do not add a workshop-builder database or duplicate renderer. Treat existing LabTemplate as the package identity for the first cut: one package, one topology template, one owned curriculum. Keep one selected runtime package, switched only by full restart as the brief proposes.

```text
lab-definitions/packages/<package-id>/
  package.yml              # id, schema version, topology/policy/compose references, capabilities
  topology.yml
  scenarios/<local-slug>.yml
  firewall/<variant>.json
  verification.yml         # probe/event/capture/reset/canary fixtures + curriculum feature maps
```

- **I:** enumerate package manifests rather than all top-level *.yml; load only its scenarios; reject inline-plus-file duplicates; validate all before transactional upsert/prune. Prune scenarios only within package, including empty curriculum. Do not transplant the frozen US topology/policy values while extracting ownership.
- **I:** minimum DB change: `LabTemplate.ID = package-id`; `Scenario.ID = <package-id>--<local-slug>` (URL-safe, works with existing global GET routes); `Scenario.LabTemplateID = package-id`; add local slug plus unique `(lab_template_id, slug)`. Reuse existing run ScenarioID/instance fields, enforcing matching package on run/create/get/execute/validate. No separate Package table needed until a package truly contains multiple topology templates.
- **I:** YAML local IDs and findings/default-from references stay package-local; resolver qualifies once at API/storage boundary. Avoid colon-separated qualified IDs because current decision inheritance uses `scenario:decision` and tests use `[A-Za-z0-9_-]+` (`scenario-decision-graph.test.ts:81–98`). Do not globally share seven identical scenario rows across packages.
- **I:** browser progress/decision/plan/track namespace must include package plus curriculum revision; stable step IDs replace array indexes before authored revisions are expected. Selected package/revision also belongs in React Query keys and evidence headers. Minimal baseline may reset old browser progress explicitly at cutover rather than maintain aliases; migration of existing saved work is an owner decision.
- **I:** move scenario-feature flags, terminal defaults, requirement/readiness→action mappings, defense mappings and dynamic phase keys into owned metadata. No title-based dispatch. Rules and verification should reference node/zone/service roles resolved to concrete addresses, while rendered commands still show concrete copy/paste targets. Curriculum and runtime validators must consume the same resolved package identity, not independently infer it.

## Validator interface selected by capability (proposal)

```go
// Conceptual interface; no source change in this recon.
type Validator interface {
    Requirements(spec CheckSpec) []CapabilityRef
    Validate(ctx context.Context, spec CheckSpec, inputs ValidationInputs) ([]ValidationCheck, error)
}
// CheckSpec identifies a registered validator kind and typed parameters.
// ValidationInputs provides package/run identity, fresh state, policy, audit and artifacts.
```

- **I:** scenario declares a list of checks by validator kind, not scenario ID. Registry provides TCP reachability, policy predicates, process-state predicates, artifact availability and protocol command-enforcement validators. Requirements select providers only when needed: `process.electrical`, `audit.device-control`, `capture.firewall`, `probe.tcp`, `protocol.iec104.command-enforcement`, etc.
- **I:** capability must mean supported executable behavior, not “IEC104 named somewhere in UI.” Unsupported validator/provider fails authoring/startup; missing runtime evidence is fail/error, not successful generic fallback. Pure planning needs no RTAC; decision completion is client-state evidence or deliberately manual, never server “Plan saved” pass.
- **I:** providers resolve RTAC/other supervisory endpoint from package, return typed fields with units/ranges, freshness and provenance, and scope audit/artifacts to run/time. Normal state checks can be shared across DNP3/IEC104 if process is shared. DPI checks cannot: IEC104 must test its own command type/address/selection/execution semantics using wire traffic and actual deny evidence.
- **I:** structured command-via-control-plane vs adversarial wire action must be distinguishable in results. TCP connect alone proves L4 reachability, not read success or command safety. Keep report outcome kernel and truthful skipped/error handling; do not replace unsupported checks with generic electrical health.

## Future no-code workshop builder: data prerequisites only

**I:** builder would need a versioned, strictly validated authoring schema with package ownership; stable step/decision/action identities; explicit prerequisite/inheritance references; typed targets/services/point maps and capability inventory; reusable supported command/check/reset definitions; separate prose from executable recipe/expected evidence; budget/role/requirement/coverage mappings as data; explicit guided/technical track flags; units/process bounds and UI node metadata; package-scoped immutable revision/evidence identities; validation diagnostics before publish. Existing Markdown directives and action schema are useful foundations, but prose regexes, hardcoded TS business catalogs and arbitrary `Save` of JSON-string model fields are not sufficient (`types.go:55–114`; `scenario-description.ts:13`; `scenarios.go:25–38`). No builder UI or workflow designed here.

## Missed couplings, debt and questions / corrections to brief

- **V:** proposal misses backend fixed workshop graph/node resolution; unscoped test-suite query; create/run ownership gaps; four separate tool allowlists; title/index dispatch; frontend policy JSON generator being a second policy source; query-cache isolation; PDF workbook selection; static knowledge base; package-less evidence/capture/reset scope. Evidence is in sections above.
- **V:** adjacent correctness debt to preserve/fix when extracting: stale/empty reseed and warning-only partial imports (`loader.go:64–68,134–149`; `main.go:29–31`); ignored BaselineGridState (`loader.go:159–170`); authoring expected_config mismatch (`validate.go:110–111`); unknown-ID RTAC fallback (`scenario_validate.go:71–74`); plan auto-pass (`:444`); nil audit interpreted as clean (`:43–47,568–573`); zero-voltage check omitted (`:535–542`); reset exec-start not observed (`reset.go:83–90`); liveness inferred per device, not protocol (`observed-flows.ts:89–101`).
- **V:** generated negative-test prose says refusal proves blocked (`frontend/lib/scenario-runner-logic.ts:136`) while runtime TCP probes treat fast refusal as reachable (`scenario_probe.go:69–72`). This can teach the wrong segmentation conclusion and should be reconciled, not copied to IEC104.
- **V:** brief's “flexible YAML” understates current runtime vocabulary/schema checks and frontend specialization. Claim 6 is true for validation requests, not proof every scenario execution always fetches RTAC (probe/firewall/manual do not). Claim 7 overstates ID hardcoding in generic storage helpers but understates globally shared plan/track. Claim 8 must include duplicated TS-generated command/rule/evidence fixtures. Current loader already validates actions and removes stale nonempty entries; it is not merely a naïve importer.
- **V:** root AGENTS.md frontend lint description says next/core-web-vitals; project instructions correctly describe ESLint 9 flat invocation. Root guide's “three smoke gates” summary omits current CI events/physics/test-suite stages (`.github/workflows/smoke.yml:160–190`). Follow actual current scripts/workflows.
- **Q:** retain Modbus alongside IEC104? retain four field roles/topology/IPs? IEC104-only regional workshop or European electrical model too? Which point/address mappings and blocked command semantics are accepted? What exact decoder evidence exists is a separate containd lane question, not verified here.
- **Q:** existing user progress preservation at US extraction; whether package ID can equal template ID long-term; whether evidence outcome should require capture freshness and affirmative DPI proof; how to declare constrained-host DPI checks unavailable without calling the workshop fully validated.
- **I:** resolve these before estimating full package delivery. The smallest vertical slice must exercise listing/owned IDs, state, wire attack, policy deny/read preservation, truthful report and storage isolation—not just substitute DNP3 vocabulary and boot another YAML.

## Inspection limits / validation

Static file/line audit and two Python read-only counts executed successfully. Tests executed: **0**. Full backend/frontend/service tests, lint/build/typecheck, Compose/vulnerability gates, all five smoke scripts and live rendering are **NOT RUN** (planning-only, no source changes; destructive live checks inappropriate). No source modified, no commits, no push/deploy. Repository began clean; only permitted ignored report output created.
