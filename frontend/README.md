# RangerDanger frontend

Next.js 14 + TypeScript UI for the RangerDanger lab. Provides the
exercise runner, network console, FUXA proxy, and substation views.

## Local development

```sh
npm ci
npm run dev    # next dev on :3000
```

The dev server runs standalone, but most of the lab functionality
(exercise validators, terminal sessions, firewall control, traffic
generation) requires the backend and the running compose stack. For
end-to-end work:

```sh
# from repo root
docker compose up -d --build
```

…then open <http://localhost:8088> (the nginx proxy serves both the
backend API and the frontend through the same origin).

## Layout

| Path | Purpose |
|------|---------|
| `app/` | Next.js routes (one directory per page) |
| `components/` | React components - exercise runner, terminals, network map, decision panel, etc. |
| `lib/` | API client, type definitions, exercise/node mappings, utility helpers |
| `public/` | Static assets - logos, icons, favicon |

Notable routes (`app/`):

- `/exercises` - exercise library + per-exercise runner
- `/console` - Cobalt Strike-style network console with React Flow
- `/substation` - substation control panel
- `/knowledge` - reference material accompanying the exercises (e.g. Purdue Model levels)
- `/labs` and `/labs/[id]` - lab template and instance management
- `/scenarios` - legacy redirect to `/exercises`

## API and proxy

The frontend never calls services directly. All HTTP requests go to
the same origin (the nginx proxy at `:8088`), which routes:

- `/api/*` → backend (Go + Gin) at `:8080`
- `/apps/fuxa-hmi/*` → FUXA HMI
- `/apps/openplc/*` → OpenPLC web UI
- `/containd/*` → containd web UI (proxied directly by nginx for same-origin iframe embedding)

The backend additionally proxies `/api/containd/*` → containd's
REST API server-side, so the frontend can hit containd's `/api/v1/*`
through the same `/api/` base it uses for everything else.

`NEXT_PUBLIC_API_URL` is injected as `/api` by `docker-compose.yml`
so `fetch` calls land on the proxy, not on `:3000`. The fallback in
`lib/utils.ts` is also `/api`, so the env var is mostly belt-and-
suspenders.

## Lint and build

```sh
npm run lint    # next lint, ESLint config in .eslintrc.json
npm run build   # next build (also runs in CI on every PR)
```

CI fails on any ESLint error. The repo convention favors no-`any`
and no unused-variable suppressions (not currently enforced as a
lint rule — `frontend/.eslintrc.json` only extends
`next/core-web-vitals` — but new code should follow it); if you find
yourself reaching for one, push back on the design first.

## Adding a page

```sh
mkdir app/foo
echo 'export default function Foo() { return <div>Foo</div>; }' > app/foo/page.tsx
```

Wire navigation in `components/nav-sidebar.tsx`. If the page calls
backend APIs that don't exist yet, add the handlers in
`backend/internal/server/server.go` first and update
`docs/api-spec.md`.

## Conventions

- TypeScript strict - avoid `any`. Cast at the boundary, narrow inside.
- Tailwind for styling. shadcn-style primitives in `components/ui/`.
- Server Components for most pages; reach for `"use client"` only
  when you need state, effects, or browser APIs.
- Don't import directly from `lib/api.ts` inside server components -
  build server actions instead so types stay aligned.
