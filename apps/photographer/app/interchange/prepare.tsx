import AsyncStorage from '@react-native-async-storage/async-storage';
import {ActivityIndicator,Text} from 'react-native';
import {useEffect,useMemo,useState} from 'react';
import type {ApiClient} from '@photoeditor/api-client';
import {router,useLocalSearchParams} from 'expo-router';
import {useAuth} from '../../src/auth-context';
import {useLocale} from '../../src/locale-context';
import {OwnerAPI} from '../../src/owner-api';
import {assertInterchangeScope} from '../../src/interchange-job';
import {freezeInterchangeSnapshot,type InterchangeSnapshotRequest} from '../../src/interchange-export-snapshot';
import {InterchangeSnapshotAttemptStore} from '../../src/interchange-snapshot-attempt-store';
import {InterchangeSnapshotScreen} from '../../src/interchange-snapshot-screen';
type Params={tenant?:string;project?:string;version?:string;source?:string;source_sha?:string;canvas?:string;recipe?:string;recipe_sha?:string};
export function editorInterchangeSnapshotRequest(params:Params):InterchangeSnapshotRequest{
 return freezeInterchangeSnapshot({items:[{ordinal:0,binding:{asset_version_id:params.version,source:{artifact_id:params.source,sha256:params.source_sha},canvas_hash:params.canvas},working_image:{artifact_id:params.source,sha256:params.source_sha},recipe:{artifact_id:params.recipe,sha256:params.recipe_sha},captured_at:null,camera_hash:null}]},'route-validation');
}
export default function PrepareRoute(){
 const params=useLocalSearchParams<Params>(),auth=useAuth(),client=useMemo(()=>auth.client(),[auth]),generation=useMemo(()=>crypto.randomUUID(),[client]);
 return <Scope key={JSON.stringify([generation,auth.origin,params])} client={client} origin={auth.origin} params={params}/>;
}
function Scope({client,origin,params}:{client:ApiClient;origin:string;params:Params}){
 const {t}=useLocale(),[loaded,setLoaded]=useState<{body:InterchangeSnapshotRequest;journal:InterchangeSnapshotAttemptStore}|null>(null),[failed,setFailed]=useState(false);
 useEffect(()=>{const controller=new AbortController();
  void (async()=>{const body=editorInterchangeSnapshotRequest(params),tenant=params.tenant||'',project=params.project||'';
   assertInterchangeScope(tenant,project,body.items[0].binding.asset_version_id);
   const user=await new OwnerAPI(client,tenant).me(controller.signal);controller.signal.throwIfAborted();
   setLoaded({body,journal:new InterchangeSnapshotAttemptStore(AsyncStorage,origin,user.id,tenant,project,body)});
  })().catch(()=>{if(!controller.signal.aborted)setFailed(true)});
  return()=>controller.abort();
 },[client,origin]);
 if(failed)return <Text accessibilityRole="alert">{t('interchange_snapshot_failed')}</Text>;
 if(!loaded)return <ActivityIndicator accessibilityLabel={t('interchange_restore_loading')}/>;
 return <InterchangeSnapshotScreen client={client} tenant={params.tenant!} project={params.project!} request={loaded.body} journal={loaded.journal} onPrepared={receipt=>router.replace({pathname:'/interchange/format',params:{tenant:params.tenant!,project:params.project!,snapshot:receipt.id}})}/>;
}
