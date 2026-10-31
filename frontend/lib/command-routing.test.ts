import { readdirSync, readFileSync } from "node:fs";
import { createRequire } from "node:module";
import { resolve } from "node:path";
import { describe, expect, it } from "vitest";
import type { Scenario } from "./api";
import { fixtureCurriculum } from "./curriculum-storage.test-util";
import { getExerciseNodes, inferNodeFromDescription } from "./exercise-nodes";
import { EU_PLANS, US_PLANS } from "./plan-fixtures.test-util";
import { injectDynamicContent } from "./scenario-runner-logic";
import { splitDescription, type Segment } from "./scenario-description";

const { load } = createRequire(import.meta.url)("js-yaml") as { load: (text: string) => unknown };
function commands(text: string, inherited?: string): Extract<Segment, { type: "cmd" }>[] {
  return splitDescription(text, inherited).flatMap((seg) => {
    if (seg.type === "cmd") return seg.cli || (seg.copyOnly && seg.sourceNode !== "host") ? [] : [seg];
    if (seg.type === "hint") return commands(seg.value, seg.sourceNode);
    if (seg.type === "trackOnly") return commands(seg.body, seg.sourceNode);
    return [];
  });
}

describe("authored command routing inventory", () => {
  it("pins every EU and US source override, including compiled descriptions", () => {
    const inventory: Record<string, string[]> = {};
    for (const id of ["us-dnp3-substation", "eu-iec104-substation"]) {
      const root = resolve(__dirname, "../..");
      const binding = fixtureCurriculum(`lab-definitions/packages/${id}`);
      const dir = resolve(root, id.startsWith("us") ? "lab-definitions/scenarios" : `lab-definitions/packages/${id}/scenarios`);
      const changes: string[] = [];
      for (const file of readdirSync(dir).filter((f) => f.endsWith(".yml")).sort()) {
        const scenario = load(readFileSync(resolve(dir, file), "utf8")) as Scenario;
        for (const step of scenario.steps) {
          const before = step.node || inferNodeFromDescription(step.description, binding) || binding.nodeMap[scenario.id]?.primary || getExerciseNodes(scenario.id, scenario.nodes, binding)[0];
          for (const dynamic of [false, true]) {
            const text = dynamic ? injectDynamicContent(step.description, step.id, id.startsWith("us") ? US_PLANS[1] : EU_PLANS[1], binding) : step.description;
            const fallback = step.node || inferNodeFromDescription(text, binding) || binding.nodeMap[scenario.id]?.primary || getExerciseNodes(scenario.id, scenario.nodes, binding)[0];
            for (const cmd of commands(text)) {
              const after = cmd.sourceNode || fallback;
              if (after !== before) changes.push(`${file}/${step.id}: ${cmd.value} [${before} → ${after}]`);
            }
          }
        }
      }
      inventory[id] = [...new Set(changes)];
    }
    expect(inventory).toMatchInlineSnapshot(`
      {
        "eu-iec104-substation": [
          "eu-firewall-implementation.yml/phase-4-enable-logging-and-visibility: curl -fsS -X POST http://localhost:8088/api/operator/reconnect [rtu-1 → host]",
          "eu-firewall-implementation.yml/phase-4-enable-logging-and-visibility: wget -qO- --post-data '' http://10.99.99.21:8080/api/reconnect [rtu-1 → cc-1]",
          "eu-firewall-implementation.yml/test-positive: curl -fsS --max-time 3 http://10.70.70.40:8080/api/points [rtu-1 → kali-1]",
          "eu-firewall-implementation.yml/test-positive: curl -fsS --max-time 3 http://10.30.30.30:8080/login [rtu-1 → eng-ws-1]",
          "eu-vendor-rdp-compromise.yml/re-attempt-the-pivot: mbpoll -m tcp -a 1 -t 4 -0 -r 0 -1 10.40.40.21 1 [kali-1 → vendor-jump-1]",
        ],
        "us-dnp3-substation": [
          "firewall-implementation.yml/phase-1-verify-current-access-under-the-weak-baseline: mbpoll -a 1 -r 1 -c 2 -1 -t 4 10.40.40.22 [kali-1 → eng-ws-1]",
          "firewall-implementation.yml/phase-1-verify-current-access-under-the-weak-baseline: mbpoll -a 1 -r 1 -c 4 -1 -t 0 10.40.40.23 [kali-1 → eng-ws-1]",
          "firewall-implementation.yml/phase-6-validate-blocked-traffic: mbpoll -a 1 -r 1 -c 4 -1 -t 0 -o 3 10.40.40.23 [kali-1 → eng-ws-1]",
          "firewall-implementation.yml/phase-6-validate-blocked-traffic: dnp3poll -a 1 -t 3s 10.40.40.20:20000 [kali-1 → vendor-jump-1]",
          "firewall-implementation.yml/phase-6-validate-blocked-traffic: mbpoll -a 1 -r 1 -c 4 -1 -t 0 -o 3 10.40.40.20 [kali-1 → vendor-jump-1]",
          "hardening-configurations.yml/attack-dnp3-direct-operate-injection-against-the-recloser: mbpoll -a 1 -t 4 -r 1 -c 5 -1 10.30.30.20 [kali-1 → rtac-1]",
          "hardening-configurations.yml/attack-dnp3-direct-operate-injection-against-the-recloser: mbpoll -a 1 -t 3 -r 1 -c 6 -1 10.30.30.20 [kali-1 → rtac-1]",
          "hardening-configurations.yml/re-test-the-attack-under-the-hardened-policy: mbpoll -a 1 -t 4 -r 1 -c 5 -1 10.30.30.20 [kali-1 → rtac-1]",
          "validation-evidence.yml/generate-the-validation-report: mbpoll -a 1 -r 1 -c 4 -1 -t 0 10.40.40.20 [kali-1 → rtac-1]",
          "validation-evidence.yml/generate-the-validation-report: dnp3poll -a 1 -t 3s 10.40.40.20:20000 [kali-1 → rtac-1]",
          "validation-evidence.yml/generate-the-validation-report: dnp3poll -a 2 -t 3s 10.40.40.21:20000 [kali-1 → rtac-1]",
          "validation-evidence.yml/generate-the-validation-report: dnp3poll -a 3 -t 3s 10.40.40.22:20000 [kali-1 → rtac-1]",
          "validation-evidence.yml/generate-the-validation-report: curl -s http://10.30.30.20:8080/api/health [kali-1 → rtac-1]",
          "validation-evidence.yml/generate-the-validation-report: curl -s http://10.30.30.20:8080/api/state | python3 -m json.tool [kali-1 → rtac-1]",
          "validation-evidence.yml/generate-the-validation-report: ssh -o StrictHostKeyChecking=no rangerdanger@10.30.30.20 'hostname' [kali-1 → vendor-jump-1]",
          "validation-evidence.yml/generate-the-validation-report: curl -ks https://10.30.30.20/ -o /dev/null -w '%{http_code}\\n' [kali-1 → vendor-jump-1]",
          "validation-evidence.yml/generate-the-validation-report: nmap -Pn -p 20000 10.40.40.21 --max-retries 1 -T4 [kali-1 → vendor-jump-1]",
          "validation-evidence.yml/generate-the-validation-report: dnp3poll -a 2 -t 3s 10.40.40.21:20000 [kali-1 → vendor-jump-1]",
          "validation-evidence.yml/generate-the-validation-report: nmap -Pn -p 20000 10.40.40.22 --max-retries 1 -T4 [kali-1 → historian-1]",
          "validation-evidence.yml/generate-the-validation-report: tcpdump -i lan2 -nn 'tcp port 502 or tcp port 20000' -w /data/captures/validation.pcap [kali-1 → fw-1]",
          "validation-evidence.yml/generate-the-validation-report: tcpdump -r /data/captures/validation.pcap -nn | awk '{ s = $3; d = $5; sub(/\\.[0-9]+$/, "", s); sub(/\\.[0-9]+:$/, "", d); print s, "->", d }' | sort | uniq -c | sort -rn [kali-1 → fw-1]",
          "vendor-rdp-compromise.yml/re-attempt-the-pivot: mbpoll -a 1 -t 0 -r 2 10.40.40.21 0 [kali-1 → vendor-jump-1]",
        ],
      }
    `);
  });
});
