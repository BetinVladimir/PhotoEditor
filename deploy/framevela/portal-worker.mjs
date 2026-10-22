import application from 'vinext/server/fetch-handler';

// Preserve portal response protections when running outside Next's Node server.
export default {
  async fetch(request, env, ctx) {
    const url = new URL(request.url);
    if (url.protocol === 'http:') {
      url.protocol = 'https:';
      return Response.redirect(url.href, 308);
    }
    const result = await application.fetch(request, env, ctx);
    const headers = new Headers(result.headers);
    headers.set('Referrer-Policy', 'no-referrer');
    headers.set('X-Content-Type-Options', 'nosniff');
    headers.set('Permissions-Policy', 'camera=(), microphone=(), geolocation=()');
    headers.set('Cache-Control', 'no-store');
    return new Response(result.body, { status: result.status, statusText: result.statusText, headers });
  },
};
