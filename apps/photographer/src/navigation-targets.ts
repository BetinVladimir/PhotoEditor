import {scopeUUID} from './workspace-access';
export const profileTarget={pathname:'/settings/profile' as const};
export function workspaceSettingsTargets(tenant:string){
 if(!scopeUUID(tenant))throw new Error('NAVIGATION_SCOPE_INVALID');
 return [
  {labelKey:'nav_members' as const,href:{pathname:'/settings/members' as const,params:{tenant}}},
  {labelKey:'nav_quotas' as const,href:{pathname:'/settings/quotas' as const,params:{tenant}}},
  {labelKey:'nav_retention' as const,href:{pathname:'/settings/retention' as const,params:{tenant}}},
 ];
}
export function sessionAnalysisTarget(tenant:string,id:string,project:string){
 if(![tenant,id,project].every(scopeUUID))throw new Error('NAVIGATION_SCOPE_INVALID');
 return {pathname:'/sessions/[id]' as const,params:{tenant,id,project}};
}
