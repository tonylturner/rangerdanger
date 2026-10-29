import { defineConfig } from "vitest/config";

// Keep logic tests in Node; component tests opt into jsdom when they need
// React Testing Library's DOM renderer. The JSX runtime matches Next.js.
export default defineConfig({
  oxc: { jsx: { runtime: "automatic" } },
  test: {
    include: ["lib/**/*.test.ts", "components/**/*.test.tsx"],
    environment: "node",
    setupFiles: ["./vitest.setup.ts"],
  },
});
