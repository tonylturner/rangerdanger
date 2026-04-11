// Workshop courseware module numbers for each exercise. These are
// shared by both the web UI exercise cards and the PDF export so the
// printed workbook ties cleanly back to the slide-deck modules used
// in instructor-led delivery.
export const WORKBOOK_SECTION: Record<string, string> = {
  "baseline-assessment": "1.2",
  "remediation-planning": "1.4",
  "segmentation-requirements": "1.3",
  "vendor-rdp-compromise": "2.3",
  "modbus-override": "2.3",
  "dnp3-command-injection": "2.3",
  "validation-evidence": "2.4",
};

export function workbookSection(scenarioId: string): string | undefined {
  return WORKBOOK_SECTION[scenarioId];
}
