import {ActivityIndicator,Button,Text} from 'react-native-paper';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {View} from 'react-native';
import type {ApiClient} from '@photoeditor/api-client';
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from '../../src/localized-router';
import {useLocale} from '../../src/locale-context';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {assertInterchangeScope} from '../../src/interchange-job';
import {freezeInterchangeCreate,type InterchangeCreateRequest} from '../../src/interchange-create';
import {InterchangeCreateAttemptStore} from '../../src/interchange-create-attempt-store';
import {InterchangeCreateScreen} from '../../src/interchange-create-screen';
type Params={tenant?:string;project?:string;direction?:string;format?:string;snapshot?:string;bundle?:string;sha256?:string};
/** Internal handoff from an already frozen snapshot or uploaded bundle; no authority is inferred from URL values. */
export function interchangeCreateRouteRequest(params:Params):InterchangeCreateRequest{
 if(params.direction==='export'){
  if(params.bundle!==undefined||params.sha256!==undefined)throw new Error('INTERCHANGE_CREATE_ROUTE_INVALID');
  return freezeInterchangeCreate({project_id:params.project,direction:params.direction,format:params.format,snapshot_id:params.snapshot},'route-validation');
 }
 if(params.direction==='import'){
  if(params.snapshot!==undefined)throw new Error('INTERCHANGE_CREATE_ROUTE_INVALID');
  return freezeInterchangeCreate({project_id:params.project,direction:params.direction,format:params.format,bundle:{artifact_id:params.bundle,sha256:params.sha256}},'route-validation');
 }
 throw new Error('INTERCHANGE_CREATE_ROUTE_INVALID');
}
export default function NewInterchangeRoute(){
 const params=useLocalSearchParams<Params>(),auth=useAuth(),client=useMemo(()=>auth.client(),[auth]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 return <ScopedRoute key={JSON.stringify([generation,auth.origin,params])} client={client} origin={auth.origin} params={params}/>;
}
function ScopedRoute({client,origin,params}:{client:ApiClient;origin:string;params:Params}){
 const {t}=useLocale(),[loaded,setLoaded]=useState<{request:InterchangeCreateRequest;journal:InterchangeCreateAttemptStore}|null>(null),[failed,setFailed]=useState(false),[attempt,setAttempt]=useState(0);
 const identity=useRef<AbortController|null>(null);
 function cancelIdentity(){identity.current?.abort();identity.current=null;setFailed(true);}
 useEffect(()=>{const controller=new AbortController();identity.current=controller;setFailed(false);setLoaded(null);const timer=setTimeout(()=>{if(identity.current===controller)cancelIdentity();},8000);
  void (async()=>{
   const request=interchangeCreateRouteRequest(params),tenant=params.tenant||'';
   assertInterchangeScope(tenant,request.project_id,request.direction==='export'?request.snapshot_id:request.bundle.artifact_id);
   const user=await new OwnerAPI(client,tenant).me(controller.signal);controller.signal.throwIfAborted();
   clearTimeout(timer);setLoaded({request,journal:new InterchangeCreateAttemptStore(AsyncStorage,origin,user.id,tenant,request)});
  })().catch(()=>{if(!controller.signal.aborted)setFailed(true);});
  return()=>{clearTimeout(timer);controller.abort();if(identity.current===controller)identity.current=null;};
 },[client,origin,attempt]);
 if(failed)return <View><Text accessibilityRole="alert">{t('interchange_create_failed')}</Text><Button accessibilityRole="button" onPress={()=>{setFailed(false);setAttempt(value=>value+1);}}>{t('retry')}</Button></View>;
 if(!loaded)return <View><ActivityIndicator accessibilityLabel={t('interchange_restore_loading')}/><Button accessibilityRole="button" onPress={cancelIdentity}>{t('read_cancel')}</Button></View>;
 return <InterchangeCreateScreen client={client} tenant={params.tenant!} request={loaded.request} journal={loaded.journal} onCreated={ticket=>router.replace({pathname:'/interchange/[id]',params:{id:ticket.workflow_id,tenant:params.tenant!,project:loaded.request.project_id}})}/>;
}
