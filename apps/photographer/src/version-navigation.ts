import type {OwnerAPI} from './owner-api';
import {scopeUUID} from './workspace-access';
export async function versionEditorTarget(api:Pick<OwnerAPI,'get'|'me'>,tenant:string,project:string,version:string,expectedActor:string,signal:AbortSignal,current:()=>boolean){
 const check=()=>{signal.throwIfAborted();if(!current())throw new DOMException('Navigation scope changed','AbortError');};
 check();if(![tenant,project,version,expectedActor].every(scopeUUID))throw new Error('VERSION_NAVIGATION_INVALID');
 const actor=await api.me(signal);check();if(!scopeUUID(actor.id)||actor.id!==expectedActor)throw new Error('VERSION_NAVIGATION_ACTOR_INVALID');
 const value=await api.get<unknown>(`/api/v1/tenants/{t}/projects/${encodeURIComponent(project)}/versions/${encodeURIComponent(version)}/navigation`,signal);check();
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('VERSION_NAVIGATION_INVALID');
 const row=value as Record<string,unknown>,keys=['tenant_id','project_id','asset_id','asset_version_id'];
 if(Object.keys(row).length!==keys.length||!keys.every(key=>Object.hasOwn(row,key))||row.tenant_id!==tenant||row.project_id!==project||row.asset_version_id!==version||!scopeUUID(row.asset_id))throw new Error('VERSION_NAVIGATION_INVALID');
 const fresh=await api.me(signal);check();if(fresh.id!==actor.id)throw new Error('VERSION_NAVIGATION_ACTOR_CHANGED');
 return {pathname:'/editor/[version]' as const,params:{tenant,project,asset:row.asset_id,version}};
}
