The seven labs are not a flat sequence of independent exercises. The choices you make in Labs 1.3 and 1.4 actually rewrite the content you see in Labs 2.2, 2.3, 2.3-bonus, and 2.4. This article explains the data pipeline behind that, because it is not obvious from the surface and a lot of the lab's pedagogical payoff comes from noticing it.

### The Three Inline Markdown Fences

Lab YAML descriptions support three custom fence blocks that the exercise runner parses out:

**`:::decision`** captures a multiple-choice answer from the student into browser localStorage. Lab 1.3 uses these to record design verdicts (BLOCK / RESTRICT / ALLOW per requirement). Lab 1.4 uses them to record selected remediation actions and per-role labor budgets. Each fence has an `id` so later labs can read the recorded answer.

```
:::decision id=enterprise-to-field options=BLOCK,RESTRICT,ALLOW correct=BLOCK
What is your design verdict for Enterprise → Field traffic?
:::
```

**`:::findings-panel`** displays what the student recorded in an earlier lab as a structured panel. Lab 2.4 uses these to surface the design verdicts from Lab 1.3 and the observations from Lab 1.2 inside the validation step, so the student can compare current evidence against original intent without flipping back through tabs.

**`:::plan-coverage`** is the runtime engine. It reads the student's Lab 1.4 selections from localStorage and renders a live "this requirement is fully addressed / partial / deferred / not applicable" matrix. Lab 2.3 uses it to show which of the three attack defenses the student's plan actually closes. Lab 2.4 uses it for the final reflection.

### The Closed Loop

Concretely, here is what happens when a student makes a choice in Lab 1.4:

1. The student clicks a remediation action in Lab 1.4 - say `pin-rtac-to-field`.
2. `decision-panel.tsx` saves the selected actions to localStorage as the student's remediation plan, under `rd:<package>:<revision>:remediation-plan`.
3. Subsequent labs read the same key when their description is rendered. Each `:::plan-coverage` fence computes coverage live from the saved plan and the Lab 1.3 verdicts, and the `injectDynamicContent()` helper in `scenario-runner-logic.ts` rewrites Lab 2.2's rule steps from the plan.
4. Lab 2.2's Phase 3 text adapts based on whether the student selected DPI actions or not. Lab 2.3's "Apply the hardened policy" plan-coverage panel shows which attacks the student's plan actually closes. Lab 2.4's final reflection shows the full coverage matrix.

### Why This Matters Pedagogically

A lab that says "build a hardened firewall policy" without forcing the student to commit to specific design verdicts and remediation choices first lets the student skip the thinking. The plan-coverage pipeline makes the thinking visible: by Lab 2.4 the student can see, "I committed to BLOCK on enterprise-to-field in Lab 1.3, I deferred adding Modbus DPI in Lab 1.4, and now Lab 2.4's panel tells me my plan does close the enterprise-to-field attack but leaves the eng-ws-to-RTAC-on-DNP3 surface open."

The same workshop can be run two different ways - straight through, treating the labs as independent exercises, or with explicit attention to the pipeline. Both work. The pipeline is the deeper read.

### Resetting the State

Student progress lives in localStorage on each student's browser. Every key is `rd:<package>:<revision>:<name>`, where the package and its curriculum revision come from the backend: `exercise:<lab-id>` holds completed steps, notes and the command log, `decision:<lab-id>:<decision-id>` holds one recorded answer, and `remediation-plan` and `firewall-track` hold the Lab 1.4 plan and the Lab 2.2 track choice. To reset a single lab's recorded decisions during a workshop, open the browser console on the lab page and run `localStorage.clear()` (clears everything) or `Object.keys(localStorage).filter(k => k.startsWith("rd:") && k.includes(":decision:segmentation-requirements:")).forEach(k => localStorage.removeItem(k))` to reset just one lab. The instructor-facing `/api/workshop/reset` endpoint resets simulator state (substation devices, firewall config) but does not touch student-side localStorage.