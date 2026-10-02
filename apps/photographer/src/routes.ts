export const photographerRoutes = [
  "/login", "/workspaces", "/projects", "/projects/[id]", "/collections/[id]", "/imports/new",
  "/capture", "/capture/[id]",
  "/styles", "/styles/[id]", "/styles/[id]/training", "/collections/[id]/apply", "/editor/[version]",
  "/reviews", "/reviews/[id]", "/runs/[id]", "/exports", "/settings/profile", "/settings/members",
  "/settings/quotas", "/settings/retention",
] as const;
export type PhotographerRoute = typeof photographerRoutes[number];

export type RouteCapability = { route: PhotographerRoute; api: readonly string[]; native: boolean; web: boolean };
export const routeCapabilities: readonly RouteCapability[] = [
  {route:"/login",api:["POST /api/v1/auth/otp/request","POST /api/v1/auth/otp/verify"],native:true,web:true},
  {route:"/workspaces",api:["GET /api/v1/tenants","POST /api/v1/tenants"],native:true,web:true},
  {route:"/projects",api:["GET /api/v1/tenants/{t}/projects","POST /api/v1/tenants/{t}/projects"],native:true,web:true},
  {route:"/projects/[id]",api:["GET /api/v1/tenants/{t}/projects/{id}","PUT /api/v1/tenants/{t}/projects/{id}/acl/{user}"],native:true,web:true},
  {route:"/collections/[id]",api:["GET /api/v1/tenants/{t}/collections/{id}","GET /api/v1/tenants/{t}/collections/{id}/items","POST /api/v1/tenants/{t}/collections/{id}/publication"],native:true,web:true},
  {route:"/imports/new",api:["POST /api/v1/tenants/{t}/uploads","POST /api/v1/tenants/{t}/uploads/{id}/parts","POST /api/v1/tenants/{t}/uploads/{id}/complete"],native:true,web:true},
  {route:"/capture",api:["GET /api/v1/tenants/{t}/shoot-sessions","GET /api/v1/tenants/{t}/shoot-sessions/{id}","GET /api/v1/tenants/{t}/collections","POST /api/v1/tenants/{t}/shoot-sessions"],native:true,web:true},
  {route:"/capture/[id]",api:["PATCH /api/v1/tenants/{t}/shoot-sessions/{id}","GET /api/v1/tenants/{t}/shoot-sessions/{id}","GET /api/v1/tenants/{t}/shoot-sessions/{id}/shot-list","PUT /api/v1/tenants/{t}/shoot-sessions/{id}/shot-list","GET /api/v1/tenants/{t}/shoot-sessions/{id}/alerts","GET /api/v1/tenants/{t}/projects/{id}/persons","POST /api/v1/tenants/{t}/capture-alerts/{id}/ack","POST /api/v1/tenants/{t}/capture-receipts/{id}/confirm"],native:true,web:true},
  {route:"/styles",api:["GET /api/v1/tenants/{t}/styles","POST /api/v1/tenants/{t}/styles"],native:true,web:true},
  {route:"/styles/[id]",api:["GET /api/v1/tenants/{t}/styles/{id}","GET /api/v1/tenants/{t}/styles/{id}/versions"],native:true,web:true},
  {route:"/styles/[id]/training",api:["POST /api/v1/tenants/{t}/styles/{id}/training-sets","POST /api/v1/tenants/{t}/training-sets/{id}/pairs","POST /api/v1/tenants/{t}/styles/{id}/analyses"],native:true,web:true},
  {route:"/collections/[id]/apply",api:["POST /api/v1/tenants/{t}/collections/{id}/apply-profile"],native:true,web:true},
  {route:"/editor/[version]",api:["GET /api/v1/tenants/{t}/assets/{id}/editor-draft","PUT /api/v1/tenants/{t}/assets/{id}/editor-draft","POST /api/v1/tenants/{t}/overrides","POST /api/v1/tenants/{t}/previews","GET /api/v1/tenants/{t}/runs/{id}/outputs","POST /api/v1/tenants/{t}/collections/{id}/save-edits"],native:true,web:true},
  {route:"/reviews",api:["GET /api/v1/tenants/{t}/reviews"],native:true,web:true},
  {route:"/reviews/[id]",api:["GET /api/v1/tenants/{t}/reviews/{id}","POST /api/v1/tenants/{t}/reviews/{id}/decision"],native:true,web:true},
  {route:"/runs/[id]",api:["GET /api/v1/tenants/{t}/runs/{id}","POST /api/v1/tenants/{t}/runs/{id}/cancel","POST /api/v1/tenants/{t}/runs/{id}/retry"],native:true,web:true},
  {route:"/exports",api:["GET /api/v1/tenants/{t}/export-presets","POST /api/v1/tenants/{t}/export-presets","POST /api/v1/tenants/{t}/download-jobs","GET /api/v1/tenants/{t}/download-jobs/{id}","POST /api/v1/tenants/{t}/download-jobs/{id}/download-intent"],native:true,web:true},
  {route:"/settings/profile",api:["GET /api/v1/me","PATCH /api/v1/me"],native:true,web:true},
  {route:"/settings/members",api:["GET /api/v1/tenants/{t}/members","POST /api/v1/tenants/{t}/invites"],native:true,web:true},
  {route:"/settings/quotas",api:["GET /api/v1/tenants/{t}","GET /api/v1/tenants/{t}/capabilities"],native:true,web:true},
  {route:"/settings/retention",api:["GET /api/v1/tenants/{t}","PATCH /api/v1/tenants/{t}"],native:true,web:true},
];
