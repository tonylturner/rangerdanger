import { defineConfig } from "vitest/config";

// Vitest config for the frontend. Logic tests live in lib/; component
// tests in components/ render to static markup with react-dom/server,
// so everything runs in the node environment without a DOM library.
// The test transform uses the same automatic JSX runtime as Next.js.
export default defineConfig({
  oxc: { jsx: { runtime: "automatic" } },
  test: {
    include: ["lib/**/*.test.ts", "components/**/*.test.tsx"],
    environment: "node",
    setupFiles: ["./vitest.setup.ts"],
  },
});
