---
id: PT-1t
title: Tests - no production domain in LocaleLinks/metadata, share page has no alternates
area: portal
size: XS
depends: []
edit:
  - apps/portal/src/components/LocaleLinks.test.tsx
  - apps/portal/src/site-metadata.test.ts (new)
context:
  - apps/portal/src/components/LocaleLinks.tsx
  - apps/portal/src/app/(localized)/[locale]/s/[share_id]/page.tsx
verify:
  - cd apps/portal && npx vitest run src/components/LocaleLinks.test.tsx src/site-metadata.test.ts
---
## Goal
Lock in PT-1: no hard-coded `https://framevela.com` leaks into server-rendered links; private share pages publish no canonical/alternates.

## Spec
1. `LocaleLinks.test.tsx`: add `it('does not embed the production origin in server-rendered links', ...)` using `renderToStaticMarkup(<LocaleLinks locale="en" path="/en/s/xyz" label="L"/>)` and assert the markup does not contain `framevela.com` and every `href` starts with `/`.
2. New `src/site-metadata.test.ts`: import `generateMetadata` from `./app/(localized)/[locale]/s/[share_id]/page`; call with `{params: Promise.resolve({locale:'en', share_id:'48cf689a-c8c0-4f70-83a7-982fd95fd946'})}`; assert `robots` is `{index:false, follow:false}`, `referrer` is `'no-referrer'` and `alternates` is `undefined`. Also call with an invalid share id and assert it rejects (Next's `notFound()` throws).
3. Landing metadata: import `generateMetadata` from `./app/(localized)/[locale]/page`; with `process.env.NEXT_PUBLIC_SITE_ORIGIN='https://example.test'` (set/restore in the test) assert `alternates.canonical === 'https://example.test/en'`.
