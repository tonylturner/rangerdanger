// Historical generated-policy oracle for curriculum compatibility tests only.
// Student controls never submit this advisory policy to the running firewall.
import { curriculumForPlan, type DynamicExercisePlan } from "./remediation-to-rules";
import type { CurriculumPolicyRule } from "./api";

export function buildContaindConfig(plan: DynamicExercisePlan): object {
  const curriculum = curriculumForPlan(plan);
  const selected = new Set(plan.selectedActions);
  const rules: CurriculumPolicyRule[] = [];
  for (const entry of curriculum.policy) {
    if (entry.when && !selected.has(entry.when) || entry.unless && selected.has(entry.unless)) continue;
    if (entry.when && curriculum.groups[entry.when]?.effect === "semantic" && curriculum.icsRules === "forbidden") continue;
    const rule = structuredClone(entry.rule);
    const predicate = entry.semantic && selected.has(entry.semantic) ? entry.predicate : rule.ics;
    if (entry.semantic) delete rule.ics;
    if (entry.logging && selected.has(curriculum.flags.logging)) rule.log = true;
    if (entry.semantic && curriculum.icsRules !== "forbidden") rule.ics = predicate;
    if (curriculum.icsRules === "forbidden") delete rule.ics;
    rules.push(rule);
  }
  return {
    schema_version: "0.1.0",
    system: {
      hostname: "containd",
      mgmt: {
        listenAddr: ":8080",
        enableHTTP: true,
        enableHTTPS: true,
        httpListenAddr: ":8080",
        httpsListenAddr: ":8443",
        tlsCertFile: "/data/tls/server.crt",
        tlsKeyFile: "/data/tls/server.key",
      },
      ssh: {},
    },
    interfaces: curriculum.interfaces,
    zones: curriculum.zones,
    routing: {},
    dataplane: {},
    pcap: { filter: {} },
    firewall: { defaultAction: "DENY", rules, nat: { enabled: false } },
    ids: { enabled: false },
    services: {
      syslog: { forwarders: [], format: "rfc5424", batchSize: 500, flushEvery: 2 },
      dns: { enabled: false },
      ntp: { enabled: false },
      proxy: { forward: { enabled: false }, reverse: { enabled: false } },
      dhcp: { enabled: false },
      vpn: { wireguard: { enabled: false }, openvpn: { enabled: false } },
      av: { enabled: false, icap: {}, clamav: {} },
    },
  };
}
