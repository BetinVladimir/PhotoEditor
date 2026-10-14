import AsyncStorage from '@react-native-async-storage/async-storage';
import {ActivityIndicator,Text} from 'react-native';
import type {ApiClient} from '@photoeditor/api-client';
import {useEffect,useMemo,useState} from 'react';
import {useLocalSearchParams} from 'expo-router';
import {useLocale} from '../../src/locale-context';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {assertInterchangeScope} from '../../src/interchange-job';
import {InterchangeReviewAttemptStore} from '../../src/interchange-review-attempt-store';
import {InterchangeJobScreen} from '../../src/interchange-job-screen';
export default function InterchangeRoute(){
 const {tenant,project,id}=useLocalSearchParams<{tenant:string;project:string;id:string}>();
 const auth=useAuth(),client=useMemo(()=>auth.client(),[auth]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 return <ScopedInterchange key={JSON.stringify([generation,auth.origin,tenant,project,id])} client={client} origin={auth.origin} tenant={tenant||''} project={project||''} id={id||''}/>;
}
function ScopedInterchange({client,origin,tenant,project,id}:{client:ApiClient;origin:string;tenant:string;project:string;id:string}){
 const {t}=useLocale();
 const [journal,setJournal]=useState<InterchangeReviewAttemptStore|null>(null),[failed,setFailed]=useState(false);
 useEffect(()=>{const controller=new AbortController();
  void (async()=>{
   assertInterchangeScope(tenant,project,id);
   const user=await new OwnerAPI(client,tenant).me(controller.signal);
   if(!controller.signal.aborted)setJournal(new InterchangeReviewAttemptStore(AsyncStorage,origin,user.id,tenant,project,id));
  })().catch(()=>{if(!controller.signal.aborted)setFailed(true);});
  return()=>controller.abort();
 },[client,origin,tenant,project,id]);
 if(failed)return <Text accessibilityRole="alert">{t('interchange_restore_failed')}</Text>;
 if(!journal)return <ActivityIndicator accessibilityLabel={t('interchange_restore_loading')}/>;
 return <InterchangeJobScreen journal={journal} client={client} tenant={tenant} project={project} id={id}/>;
}
