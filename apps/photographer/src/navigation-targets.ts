import {scopeUUID,roleAllows,type WorkspaceRole} from './workspace-access';
export const profileTarget={pathname:'/settings/profile' as const};
export function workspaceSettingsTargets(tenant:string,role:WorkspaceRole='viewer'){
 if(!scopeUUID(tenant))throw new Error('NAVIGATION_SCOPE_INVALID');
 return [
  ...(roleAllows(role,'owner')?[
   {labelKey:'nav_billing' as const,href:{pathname:'/settings/billing' as const,params:{tenant}}},
   {labelKey:'nav_billing_payment' as const,href:{pathname:'/settings/billing-payment-details' as const,params:{tenant}}},
  ]:[]),
  ...(roleAllows(role,'admin')?[
   {labelKey:'nav_usage' as const,href:{pathname:'/settings/usage' as const,params:{tenant}}},
  ]:[]),
  {labelKey:'nav_members' as const,href:{pathname:'/settings/members' as const,params:{tenant}}},
  {labelKey:'nav_quotas' as const,href:{pathname:'/settings/quotas' as const,params:{tenant}}},
  {labelKey:'nav_retention' as const,href:{pathname:'/settings/retention' as const,params:{tenant}}},
  {labelKey:'nav_privacy' as const,href:{pathname:'/settings/privacy' as const,params:{tenant}}},
 ];
}
export function sessionAnalysisTarget(tenant:string,id:string,project:string){
 if(![tenant,id,project].every(scopeUUID))throw new Error('NAVIGATION_SCOPE_INVALID');
 return {pathname:'/sessions/[id]' as const,params:{tenant,id,project}};
}
