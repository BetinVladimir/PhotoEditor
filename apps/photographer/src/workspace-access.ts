import {photographerRoutes,type PhotographerRoute} from './routes';
export type WorkspaceRole='owner'|'admin'|'editor'|'viewer';
export const scopeUUID=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
const rank:Record<WorkspaceRole,number>={viewer:0,editor:1,admin:2,owner:3};
export function roleAllows(actual:unknown,required:WorkspaceRole){return typeof actual==='string'&&Object.hasOwn(rank,actual)&&rank[actual as WorkspaceRole]>=rank[required]}
export const globalRoutes=new Set<PhotographerRoute>(['/','/login','/register','/workspaces','/settings/profile']);
/** Read access remains Viewer for mixed browsing screens. Their editing controls
 * consume the current role; each server action still repeats project/session ACL. */
const editorScreens=new Set<PhotographerRoute>(['/reviews','/reviews/[id]','/imports/new','/collections/[id]/apply','/collections/[id]/multi','/collections/[id]/lens','/styles/[id]/training','/editor/[version]','/exports/sign','/deliveries/new','/multi-replacements/[original]','/perspective-replacements/[original]','/interchange/new','/interchange/format','/interchange/prepare','/interchange/prepared/[id]','/interchange/import-format','/interchange/upload','/interchange/masks/[id]']);
export const routeRolePolicies=Object.fromEntries(photographerRoutes.map(route=>[route,{tenant:!globalRoutes.has(route),read:editorScreens.has(route)?'editor':'viewer'}])) as Record<PhotographerRoute,{tenant:boolean;read:WorkspaceRole}>;
export function scopedRoute(segments:readonly string[]):PhotographerRoute{
 const normalized=[...segments];if(normalized.at(-1)==='index')normalized.pop();
 const route=('/'+normalized.join('/')) as PhotographerRoute;
 if(!Object.hasOwn(routeRolePolicies,route))throw new Error('WORKSPACE_ROUTE_INVALID');return route;
}
export function readWorkspaceAuthority(value:unknown,tenant:string){
 if(!scopeUUID(tenant)||!value||typeof value!=='object'||Array.isArray(value))throw new Error('WORKSPACE_AUTHORITY_INVALID');
 const row=value as Record<string,unknown>;
 if(row.id!==tenant||!roleAllows(row.role,'viewer')||typeof row.revision!=='string'||!/^[1-9][0-9]{0,18}$/.test(row.revision)||BigInt(row.revision)>9223372036854775807n)throw new Error('WORKSPACE_AUTHORITY_INVALID');
 return {tenant,role:row.role as WorkspaceRole,revision:row.revision};
}
export function workspaceScope(route:PhotographerRoute,params:Record<string,unknown>){
 if(!routeRolePolicies[route].tenant)return null;
 if(!scopeUUID(params.tenant))throw new Error('WORKSPACE_SCOPE_INVALID');
 // Every dynamic path segment is required, independently of optional query scope.
 for(const segment of route.split('/')){const match=/^\[([a-zA-Z]+)\]$/.exec(segment);if(match&&!scopeUUID(params[match[1]]))throw new Error('WORKSPACE_SCOPE_INVALID');}
 // Search parameters are untrusted, including repeated query keys/arrays.
 for(const key of ['project','id','asset','version','original','collection','item','analysis','export','session','snapshot','style','review','semanticWorkflow','relightWorkflow','relightAsset','relightVersion','localPortraitWorkflow','deliveryWorkflow','deliveryProject'])if(params[key]!==undefined&&!scopeUUID(params[key]))throw new Error('WORKSPACE_SCOPE_INVALID');
 const project=route==='/projects/[id]'?params.id:params.project;
 if(route==='/editor/[version]'&&!scopeUUID(params.asset))throw new Error('WORKSPACE_SCOPE_INVALID');
 return {tenant:params.tenant,project:project as string|undefined,asset:route==='/editor/[version]'?params.asset as string:undefined};
}
