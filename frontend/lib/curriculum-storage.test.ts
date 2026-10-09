import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  FIREWALL_TRACK,
  REMEDIATION_PLAN,
  curriculumKey,
  decisionName,
  exerciseName,
  readCurriculum,
  removeCurriculum,
  writeCurriculum,
  type CurriculumScope,
} from "./curriculum-storage";
import { memoryStorage } from "./memory-storage.test-util";

const US: CurriculumScope = { packageId: "us-dnp3-substation", revision: 1 };

describe("curriculumKey", () => {
  it("scopes every name as rd:<package>:<revision>:<name>", () => {
    expect(curriculumKey(US, decisionName("baseline-assessment", "enterprise-to-field")))
      .toBe("rd:us-dnp3-substation:1:decision:baseline-assessment:enterprise-to-field");
    expect(curriculumKey(US, exerciseName("baseline-assessment")))
      .toBe("rd:us-dnp3-substation:1:exercise:baseline-assessment");
    expect(curriculumKey(US, REMEDIATION_PLAN)).toBe("rd:us-dnp3-substation:1:remediation-plan");
    expect(curriculumKey(US, FIREWALL_TRACK)).toBe("rd:us-dnp3-substation:1:firewall-track");
  });

  it("keeps decision ids verbatim so the YAML contract test catches typos", () => {
    expect(decisionName("baseline-assessment", "enterprise_to_field"))
      .toBe("decision:baseline-assessment:enterprise_to_field");
  });
});

describe("curriculum storage", () => {
  let storage: Storage;
  beforeEach(() => {
    storage = memoryStorage();
    vi.stubGlobal("window", { localStorage: storage });
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("round-trips a value under the scoped key", () => {
    writeCurriculum(US, FIREWALL_TRACK, "technical");
    expect(storage.getItem("rd:us-dnp3-substation:1:firewall-track")).toBe("technical");
    expect(readCurriculum(US, FIREWALL_TRACK)).toBe("technical");
    removeCurriculum(US, FIREWALL_TRACK);
    expect(readCurriculum(US, FIREWALL_TRACK)).toBeNull();
  });

  it("isolates packages and curriculum revisions", () => {
    writeCurriculum(US, REMEDIATION_PLAN, "us-plan");
    expect(readCurriculum({ packageId: "eu-iec104-substation", revision: 1 }, REMEDIATION_PLAN)).toBeNull();
    expect(readCurriculum({ ...US, revision: 2 }, REMEDIATION_PLAN)).toBeNull();
  });

  it("ignores keys written before scoping", () => {
    storage.setItem("decision:segmentation-requirements:enterprise-to-field", "BLOCK");
    storage.setItem("rd-remediation-plan", "{}");
    storage.setItem("rangerdanger.firewall-track", "technical");
    expect(readCurriculum(US, decisionName("segmentation-requirements", "enterprise-to-field"))).toBeNull();
    expect(readCurriculum(US, REMEDIATION_PLAN)).toBeNull();
    expect(readCurriculum(US, FIREWALL_TRACK)).toBeNull();
  });

  it("degrades to no persistence when the browser blocks storage", () => {
    const blocked = {
      get localStorage(): Storage {
        throw new Error("SecurityError");
      },
    };
    vi.stubGlobal("window", blocked);
    expect(() => writeCurriculum(US, FIREWALL_TRACK, "guided")).not.toThrow();
    expect(readCurriculum(US, FIREWALL_TRACK)).toBeNull();
  });

  it("reads nothing during server rendering", () => {
    vi.unstubAllGlobals();
    expect(readCurriculum(US, FIREWALL_TRACK)).toBeNull();
  });
});
