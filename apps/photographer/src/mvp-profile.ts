/**
 * MVP release profile (P-1). Routes stay in the codebase and in
 * photographerRoutes; they are only disabled at runtime so nothing is deleted.
 * Set EXPO_PUBLIC_MVP_PROFILE=full to enable every route (development/QA).
 */
export function mvpProfileEnabled(env: string | undefined = process.env.EXPO_PUBLIC_MVP_PROFILE): boolean {
  return env !== 'full';
}

/** `pathname` may carry the `/{locale}` prefix used by the URL router. */
export function isDeferredInMvp(pathname: string, enabled = mvpProfileEnabled()): boolean {
  if (!enabled) return false;
  const path = pathname.replace(/^\/(en|ru|bg)(?=\/|$)/, '') || '/';
  if (path === '/interchange' || path.startsWith('/interchange/')) return true;
  if (path === '/exports/sign' || path.startsWith('/exports/sign/')) return true;
  return /^\/styles\/[^/]+\/training(\/|$)/.test(path);
}
