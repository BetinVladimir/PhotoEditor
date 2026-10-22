import {Text} from 'react-native-paper';
import {createContext,useCallback,useContext,useMemo,useState,type ReactNode} from 'react';
import {useFocusEffect} from './localized-router';
import {SafeAreaView,View} from 'react-native';
import {NavigationReturnLinks} from './navigation-return-links';
import {useAuth} from './auth-context';
import {OwnerAPI} from './owner-api';
import {useReadResource} from './use-read-resource';
import {ReadResourceState} from './read-resource-state';
import {useLocale} from './locale-context';
import {readWorkspaceAuthority,roleAllows,routeRolePolicies,scopedRoute,scopeUUID,workspaceScope,type WorkspaceRole} from './workspace-access';
export type WorkspaceAccess={tenant:string;actor:string;role:WorkspaceRole;canEdit:boolean;canAdmin:boolean};
export const WorkspaceAccessContext=createContext<WorkspaceAccess|null>(null);
export function useWorkspaceAccess(tenant:string){const value=useContext(WorkspaceAccessContext);return value?.tenant===tenant?value:null}
export function WorkspaceAccessBoundary({children,routeName,params={}}:{children:ReactNode;routeName:string;params?:Record<string,unknown>}){
 const {t}=useLocale();
 let route,scope;try{route=scopedRoute(routeName.split('/').filter(Boolean));scope=workspaceScope(route,params)}catch{return <SafeAreaView><Text accessibilityRole="alert">{t('workspace_scope_invalid')}</Text></SafeAreaView>}
 if(!scope)return <WorkspaceAccessContext.Provider value={null}>{route==='/settings/profile'?<View style={{flex:1}}><NavigationReturnLinks/><View style={{flex:1}}>{children}</View></View>:children}</WorkspaceAccessContext.Provider>;
 return <Scoped key={JSON.stringify([route,scope.tenant,scope.project,scope.asset])} route={route} tenant={scope.tenant} project={scope.project} asset={scope.asset}>{children}</Scoped>;
}
function Scoped({route,tenant,project,asset,children}:{route:keyof typeof routeRolePolicies;tenant:string;project?:string;asset?:string;children:ReactNode}){
 const [focus,setFocus]=useState({active:false,generation:0});
 useFocusEffect(useCallback(()=>{setFocus(value=>({active:true,generation:value.generation+1}));return()=>setFocus(value=>({...value,active:false}))},[]));
 const auth=useAuth(),{t}=useLocale(),client=useMemo(()=>auth.client(),[auth]),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const resource=useReadResource(JSON.stringify([auth.origin,tenant,project,asset,route,focus.active,focus.generation]),api,async signal=>{
  if(!focus.active)throw new Error('WORKSPACE_SCREEN_INACTIVE');
  const [actor,settings]=await Promise.all([api.me(signal),api.tenantSettings(signal)]);signal.throwIfAborted();
  if(!scopeUUID(actor.id))throw new Error('WORKSPACE_ACTOR_INVALID');
  const authority=readWorkspaceAuthority(settings,tenant);
  const derived=asset?await api.assetProject(asset,signal):undefined;signal.throwIfAborted();
  if(derived&&project&&derived!==project)throw new Error('WORKSPACE_PROJECT_MISMATCH');
  const resolved=derived??project;
  const access=resolved?await api.projectAccess(resolved,signal):null;signal.throwIfAborted();
  return {tenant,actor:actor.id,role:authority.role,canEdit:roleAllows(authority.role,'editor')&&access?.can_edit!==false,canAdmin:roleAllows(authority.role,'admin')};
 });
 if(!focus.active)return null;
 if(resource.phase!=='ready'||!resource.data)return <SafeAreaView><ReadResourceState {...resource}/></SafeAreaView>;
 if(!roleAllows(resource.data.role,routeRolePolicies[route].read)||(routeRolePolicies[route].read==='editor'&&!resource.data.canEdit))return <SafeAreaView><Text accessibilityRole="alert">{t('workspace_role_denied')}</Text></SafeAreaView>;
 return <WorkspaceAccessContext.Provider value={resource.data}><View style={{flex:1}}><NavigationReturnLinks tenant={tenant} current={resource.current}/><View style={{flex:1}}>{children}</View></View></WorkspaceAccessContext.Provider>;
}
