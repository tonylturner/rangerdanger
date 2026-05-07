import { defineConfig } from "vitest/config";

// Vitest config for pure-TypeScript module tests in frontend/lib/.
// Component tests (React/JSX) need a richer setup (jsdom + plugin-
// react + Testing Library) — those would go in a separate config or
// be added here when the component test surface arrives. For now,
// scope is the deterministic logic in lib/ that the lab UI hinges
// on.
export default defineConfig({
  test: {
    include: ["lib/**/*.test.ts"],
    environment: "node",
  },
});
