# Served Turbopack excerpts

Trimmed from containd 0.1.36 responses through `/containd/`, not from
containd source:

- `runtime.js`: base-path declaration, string chunk loader and URL builder
  from `turbopack-3-ry682tkyiqk.js`. Unrelated runtime functions are omitted.
- `page.js`: lazy import registration from `2kzg625-w6g98.js`. Its dependency
  array loads a stylesheet and `3lhospwwj0xgp.js`.
- `console.js`: two lazy import registrations from `3lhospwwj0xgp.js`,
  retrieved with a read-only portal GET. This proves imports are transitive,
  not just the references in the initial page scripts.

These are excerpts, not executable bundles. Identifiers and dependency
filenames are preserved so tests exercise the observed emitted syntax.
