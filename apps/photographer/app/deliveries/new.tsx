import {useEffect,useMemo,useState} from 'react';
import type {ApiClient} from '@photoeditor/api-client';
import {ActivityIndicator,Text} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {router,useLocalSearchParams} from 'expo-router';
import {useAuth} from '../../src/auth-context';
import {DeliveryCreateScreen} from '../../src/delivery-create-screen';
import {DeliveryCreateAttemptStore} from '../../src/delivery-create-attempt-store';
import {OwnerAPI} from '../../src/owner-api';
import {useLocale} from '../../src/locale-context';
export default function NewDeliveryRoute(){
 const params=useLocalSearchParams<{tenant:string;project:string;snapshot:string}>(),auth=useAuth(),client=useMemo(()=>auth.client(),[auth]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 return <ScopedRoute key={JSON.stringify([generation,auth.origin,params.tenant,params.project,params.snapshot])} client={client} origin={auth.origin} tenant={params.tenant||''} project={params.project||''} snapshot={params.snapshot||''}/>;
}
function ScopedRoute({client,origin,tenant,project,snapshot}:{client:ApiClient;origin:string;tenant:string;project:string;snapshot:string}){
 const {t}=useLocale(),[journal,setJournal]=useState<DeliveryCreateAttemptStore|null>(null),[failed,setFailed]=useState(false);
 useEffect(()=>{const controller=new AbortController();let live=true;
  void new OwnerAPI(client,tenant).me(controller.signal).then(user=>{if(live&&!controller.signal.aborted)setJournal(new DeliveryCreateAttemptStore(AsyncStorage,origin,user.id,tenant,project,snapshot));}).catch(()=>{if(live&&!controller.signal.aborted)setFailed(true);});
  return()=>{live=false;controller.abort();};
 },[client,origin,tenant,project,snapshot]);
 if(failed)return <Text accessibilityRole="alert">{t('delivery_create_failed')}</Text>;
 if(!journal)return <ActivityIndicator/>;
 return <DeliveryCreateScreen journal={journal} client={client} tenant={tenant} project={project} snapshot={snapshot} onCreated={ticket=>{
  router.replace({pathname:'/runs/[id]',params:{id:ticket.run_id,tenant,deliveryWorkflow:ticket.workflow_id,deliveryProject:project}});
  // An acknowledged ticket is visible in Runs. If cleanup fails, reopening
  // this route replays that ticket and retries cleanup instead of resubmitting.
  void journal.clearAccepted(ticket).catch(()=>setFailed(true));
 }}/>;
}
