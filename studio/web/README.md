# Crew Studio — web

React frontend for Crew Studio: design, run and inspect CrewAI crews (agents, tasks, crews, LLMs,
pricing, settings, runs, history) without writing Python. It talks only to the Go API described in
[`../docs/api.md`](../docs/api.md).

Stack: Vite, React 19, TypeScript (strict), Tailwind CSS v4, React Router 7, React Flow
(`@xyflow/react`) with dagre layout, react-markdown + remark-gfm, lucide-react, sonner, Vitest.

## Install

```bash
cd studio/web
npm install
```

Requires Node 20+ (developed on Node 24).

## Run (development)

```bash
npm run dev
```

Opens on <http://127.0.0.1:5174>. Requests to `/api` are proxied to the Go service at
`http://127.0.0.1:8088` (SSE run streams go through the same proxy). Every page shows an error
state with a Retry button if the backend is not running.

## Build

```bash
npm run build      # tsc -b && vite build → dist/
npm run preview    # serve the production build locally
```

## Test

```bash
npm test           # vitest run (unit tests)
npm run test:watch
```

Tests cover the API client (envelope unwrapping, `ApiError`, 204 handling), placeholder detection,
the crew → graph conversion, price matching, SSE event parsing and the timeline reducer.

## Layout

```
src/
  api/          types mirroring api.md, fetch wrapper (client.ts), endpoint functions
  components/   shared UI: buttons, fields, modal, tabs, markdown, graph canvas/nodes/layout
  layout/       app shell and sidebar navigation
  lib/          helpers: placeholders, pricing match, formatting, hooks (useAsync, forms, theme)
  features/
    overview/ llms/ pricing/ settings/ agents/ tasks/ crews/ runs/ history/
```

Model output is rendered only through react-markdown (raw HTML is skipped; remote images are
shown as links).
