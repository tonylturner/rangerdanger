// ── LocalStorage persistence helpers ──────────────────────────────
export function storageKey(scenarioId: string) { return `rd-exercise-${scenarioId}`; }

export type SavedState = {
  completedSteps: number[];
  notes: string;
  cmdLog: string[];
};

export function loadSaved(scenarioId: string): SavedState {
  try {
    const raw = localStorage.getItem(storageKey(scenarioId));
    if (raw) {
      const parsed = JSON.parse(raw);
      // Migrate from per-step notes (Record<number, string>) to single shared note
      if (parsed.notes && typeof parsed.notes === "object" && !Array.isArray(parsed.notes)) {
        const merged = Object.values(parsed.notes as Record<string, string>).filter(Boolean).join("\n\n");
        return { ...parsed, notes: merged };
      }
      return parsed;
    }
  } catch { /* ignore */ }
  return { completedSteps: [], notes: "", cmdLog: [] };
}

export function saveToDisk(scenarioId: string, state: SavedState) {
  try { localStorage.setItem(storageKey(scenarioId), JSON.stringify(state)); } catch { /* ignore */ }
}
