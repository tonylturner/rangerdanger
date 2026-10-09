import { defineConfig } from "vitest/config";

// Vitest config for the frontend. Logic tests live in lib/; component
// tests in components/ render to static markup with react-dom/server,
// so everything runs in the node environment without a DOM library.
// tsconfig keeps "jsx": "preserve" for Next, so the test transform is
// told to emit the automatic runtime itself.
export default defineConfig({
  oxc: { jsx: { runtime: "automatic" } },
  test: {
    include: ["lib/**/*.test.ts", "components/**/*.test.tsx"],
    environment: "node",
  },
});
