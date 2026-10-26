---
id: PT-2a
title: Portal security headers in next.config.ts (+ cache split)
area: portal
size: S
depends: []
edit:
  - apps/portal/next.config.ts
  - apps/portal/src/next-config.test.ts (new)
context:
  - apps/portal/next.config.ts
verify:
  - cd apps/portal && npx vitest run src/next-config.test.ts
---
## Goal
Defence in depth even when the portal is reached without Caddy, and stop sending `Cache-Control: no-store` for public static content (landing, icons) while keeping it for private share pages and API calls.

## Spec
Rewrite `headers()` to return two entries (keep `output`, `poweredByHeader`, `reactStrictMode`, `experimental` untouched):
1. `source: "/:path*"` with: `Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`, `Permissions-Policy: camera=(), microphone=(), geolocation=()`, `X-Frame-Options: DENY`, `Cross-Origin-Opener-Policy: same-origin`, `Content-Security-Policy: frame-ancestors 'none'; base-uri 'none'; object-src 'none'; form-action 'self'`.
2. `source: "/:locale/s/:share_id"` with `Cache-Control: no-store`.
Do not add HSTS here (it is set by Caddy/Cloudflare). Do not set `Cache-Control` for other paths (Next manages `_next/static`).

## Test
`import config from "../next.config"`; `const rules = await config.headers!()`; assert: some rule with source `/:path*` contains X-Frame-Options DENY and a CSP containing `frame-ancestors 'none'`; the `/:locale/s/:share_id` rule has `Cache-Control: no-store`; NO rule sets `Cache-Control` on `/:path*`.
