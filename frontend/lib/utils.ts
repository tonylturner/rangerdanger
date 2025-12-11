export function cn(...inputs: Array<string | undefined | null | false>) {
  return inputs.filter(Boolean).join(" ");
}

export const API_BASE_URL = process.env.NEXT_PUBLIC_API_URL ?? "/api";
