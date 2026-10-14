import {useEffect,useMemo,useState} from 'react';
import {ActivityIndicator,Pressable,Text,View} from 'react-native';
import {router,useLocalSearchParams} from 'expo-router';
import type {ApiClient} from '@photoeditor/api-client';
import {useAuth} from '../../src/auth-context';
import {useLocale} from '../../src/locale-context';
import {OwnerAPI} from '../../src/owner-api';
import {archiveUUID} from '../../src/interchange-archive-upload';
import {pickInterchangeArchive,prepareArchiveCache} from '../../src/interchange-archive-picker';
import {InterchangeArchiveScreen} from '../../src/interchange-archive-screen';
type Params={tenant?:string;project?:string;format?:string};
export function archiveUploadRouteScope(params:Params){
 const format=params.format;
 if(!archiveUUID(params.tenant)||!archiveUUID(params.project)||typeof format!=='string'||!['photoeditor_bundle','xmp','captureone_sidecar'].includes(format))throw new Error('INTERCHANGE_ARCHIVE_ROUTE_INVALID');
 return {tenant:params.tenant,project:params.project,format};
}
export default function ArchiveUploadRoute(){
 const params=useLocalSearchParams<Params>(),auth=useAuth(),client=useMemo(()=>auth.client(),[auth]),generation=useMemo(()=>crypto.randomUUID(),[client]);
 return <Scope key={JSON.stringify([generation,auth.origin,params])} client={client} origin={auth.origin} params={params}/>;
}
function Scope({client,origin,params}:{client:ApiClient;origin:string;params:Params}){
 const {t}=useLocale(),[loaded,setLoaded]=useState<{tenant:string;project:string;format:string;user:string}|null>(null),[failed,setFailed]=useState(false),[attempt,setAttempt]=useState(0);
 useEffect(()=>{const controller=new AbortController();setFailed(false);setLoaded(null);
  void(async()=>{const scope=archiveUploadRouteScope(params);prepareArchiveCache(controller.signal);const user=await new OwnerAPI(client,scope.tenant).me(controller.signal);controller.signal.throwIfAborted();if(!archiveUUID(user.id))throw new Error('INTERCHANGE_ARCHIVE_PRINCIPAL_INVALID');setLoaded({...scope,user:user.id});})().catch(()=>{if(!controller.signal.aborted)setFailed(true);});
  return()=>controller.abort();
 },[client,origin,attempt]);
 if(failed)return <View><Text accessibilityRole="alert">{t('interchange_archive_failed')}</Text><Pressable accessibilityRole="button" onPress={()=>{setFailed(false);setAttempt(value=>value+1);}}><Text>{t('retry')}</Text></Pressable></View>;
 if(!loaded)return <ActivityIndicator accessibilityLabel={t('interchange_restore_loading')}/>;
 return <InterchangeArchiveScreen picker={pickInterchangeArchive} client={client} origin={origin} user={loaded.user} tenant={loaded.tenant} project={loaded.project} onReady={artifact=>router.replace({pathname:'/interchange/new',params:{tenant:loaded.tenant,project:loaded.project,direction:'import',format:loaded.format,bundle:artifact.artifact_id,sha256:artifact.sha256}})}/>;
}
