import type { Section } from "../knowledge-types";
import { substationEquipment } from "./substation-equipment";
import { segmentationConcepts } from "./segmentation-concepts";
import { protocols } from "./protocols";
import { toolReferences } from "./tool-references";
import { labInternals } from "./lab-internals";
import { threatsAndPractice } from "./threats-and-practice";

export const sections: Section[] = [
  substationEquipment,
  segmentationConcepts,
  protocols,
  toolReferences,
  labInternals,
  threatsAndPractice,
];
