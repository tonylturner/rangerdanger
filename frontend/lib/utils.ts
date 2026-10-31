export function cn(...inputs: Array<string | undefined | null | false>) {
  return inputs.filter(Boolean).join(" ");
}

// The text of a thrown value. Template-interpolating an Error prints
// its name too ("Error: ..."), which doubled up behind our own labels.
export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "/api";
