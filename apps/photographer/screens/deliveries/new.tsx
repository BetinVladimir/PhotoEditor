import {ActivityIndicator,Button,Text} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import type {ApiClient} from '@photoeditor/api-client';
import {SafeAreaView} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {router,useLocalSearchParams} from '../../src/localized-router';
import {useAuth} from '../../src/auth-context';
import {DeliveryCreateScreen} from '../../src/delivery-create-screen';
import {DeliveryCreateAttemptStore} from '../../src/delivery-create-attempt-store';
import {OwnerAPI} from '../../src/owner-api';
import {useLocale} from '../../src/locale-context';
import {assertDeliveryScope} from '../../src/delivery-package';
import {useReviewForeground} from '../../src/use-review-foreground';
export default function NewDeliveryRoute(){
 const params=useLocalSearchParams<{tenant:string;project:string;snapshot:string}>(),auth=useAuth(),client=useMemo(()=>auth.client(),[auth]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 return <ScopedRoute key={JSON.stringify([generation,auth.origin,params.tenant,params.project,params.snapshot])} client={client} origin={auth.origin} tenant={params.tenant||''} project={params.project||''} snapshot={params.snapshot||''}/>;
}
function ScopedRoute({client,origin,tenant,project,snapshot}:{client:ApiClient;origin:string;tenant:string;project:string;snapshot:string}){
 const {t}=useLocale(),foreground=useReviewForeground();
 const bootstrap=useRef<AbortController|null>(null);
 const mounted=useRef(true);
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[]);
 const [journal,setJournal]=useState<DeliveryCreateAttemptStore|null>(null),[failed,setFailed]=useState(false),[epoch,setEpoch]=useState(0);
 useEffect(()=>{
  setJournal(null);setFailed(false);
  if(!foreground.active)return;
  const controller=new AbortController();bootstrap.current=controller;let live=true;
  const timer=setTimeout(()=>{
   if(live){controller.abort();setFailed(true)}
  },8000);
  void (async()=>{
   assertDeliveryScope(tenant,project,snapshot);
   const user=await new OwnerAPI(client,tenant).me(controller.signal);
   if(live&&!controller.signal.aborted)setJournal(new DeliveryCreateAttemptStore(AsyncStorage,origin,user.id,tenant,project,snapshot));
  })().catch(()=>{if(live&&!controller.signal.aborted)setFailed(true)}).finally(()=>clearTimeout(timer));
  return()=>{live=false;clearTimeout(timer);controller.abort()};
 },[client,origin,tenant,project,snapshot,epoch,foreground.active,foreground.generation]);
 if(!foreground.active)return null;
 if(failed)return <SafeAreaView><Text accessibilityRole="alert">{t('delivery_create_failed')}</Text><Button accessibilityRole="button" onPress={()=>{setFailed(false);setEpoch(value=>value+1)}}>{t('delivery_create_reload')}</Button></SafeAreaView>;
 if(!journal)return <SafeAreaView><ActivityIndicator accessibilityLabel={t('delivery_loading')}/><Button accessibilityRole="button" onPress={()=>{bootstrap.current?.abort();setFailed(true)}}>{t('read_cancel')}</Button></SafeAreaView>;
 return <DeliveryCreateScreen journal={journal} client={client} tenant={tenant} project={project} snapshot={snapshot} onCreated={ticket=>{
  router.replace({pathname:'/runs/[id]',params:{id:ticket.run_id,tenant,deliveryWorkflow:ticket.workflow_id,deliveryProject:project}});
  // An acknowledged ticket is visible in Runs. If cleanup fails, reopening
  // this route replays that ticket and retries cleanup instead of resubmitting.
  void journal.clearAccepted(ticket).catch(()=>{if(mounted.current)setFailed(true)});
 }}/>;
}
