import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));

/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  // Tests use repository fixtures absent from the frontend-only image context.
  // Keep their type coverage in the full tsconfig via `npm run typecheck`.
  typescript: {
    tsconfigPath: "tsconfig.build.json"
  },
  // Next.js infers the workspace root by walking up for lockfiles. A stray
  // package-lock.json anywhere above this directory (e.g. in $HOME) wins that
  // search and silently changes the file-tracing root, so pin it to the
  // frontend directory to keep builds identical across machines and CI.
  outputFileTracingRoot: __dirname
};

export default nextConfig;
