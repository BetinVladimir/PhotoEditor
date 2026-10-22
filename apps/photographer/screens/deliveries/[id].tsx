import {ActivityIndicator,Button,Text} from 'react-native-paper';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {SafeAreaView} from 'react-native';
import type {ApiClient} from '@photoeditor/api-client';
import {DeliveryPublishAttemptStore} from '../../src/delivery-publish-attempt-store';
import {OwnerAPI} from '../../src/owner-api';
import {useLocale} from '../../src/locale-context';
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from '../../src/localized-router';
import {useAuth} from '../../src/auth-context';
import {DeliveryPackageScreen} from '../../src/delivery-package-screen';
import {assertDeliveryScope} from '../../src/delivery-package';
import {useReviewForeground} from '../../src/use-review-foreground';
export default function DeliveryRoute(){
 const {tenant,project,id}=useLocalSearchParams<{tenant:string;project:string;id:string}>();
 const auth=useAuth(),client=useMemo(()=>auth.client(),[auth]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 return <ScopedDelivery key={JSON.stringify([generation,auth.origin,tenant,project,id])} client={client} origin={auth.origin} tenant={tenant||''} project={project||''} id={id||''}/>;
}

function ScopedDelivery({client,origin,tenant,project,id}:{client:ApiClient;origin:string;tenant:string;project:string;id:string}){
 const {t}=useLocale(),foreground=useReviewForeground();
 const bootstrap=useRef<AbortController|null>(null);
 const mounted=useRef(true);
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[]);
 const [journal,setJournal]=useState<DeliveryPublishAttemptStore|null>(null),[failed,setFailed]=useState(false),[epoch,setEpoch]=useState(0);
 useEffect(()=>{
  setJournal(null);setFailed(false);
  if(!foreground.active)return;
  const controller=new AbortController();bootstrap.current=controller;let live=true;
  // A stalled account read must not leave a permanent spinner or accept a
  // late identity after the user has retried or changed account/route.
  const timer=setTimeout(()=>{
   if(live){controller.abort();setFailed(true)}
  },8000);
  void (async()=>{
   assertDeliveryScope(tenant,project,id);
   const user=await new OwnerAPI(client,tenant).me(controller.signal);
   if(live&&!controller.signal.aborted)setJournal(new DeliveryPublishAttemptStore(AsyncStorage,origin,user.id,tenant,project,id));
  })().catch(()=>{if(live&&!controller.signal.aborted)setFailed(true)}).finally(()=>clearTimeout(timer));
  return()=>{live=false;clearTimeout(timer);controller.abort()};
 },[client,origin,tenant,project,id,epoch,foreground.active,foreground.generation]);
 if(!foreground.active)return null;
 if(failed)return <SafeAreaView><Text accessibilityRole="alert">{t('delivery_load_failed')}</Text><Button accessibilityRole="button" onPress={()=>{setFailed(false);setEpoch(value=>value+1)}}>{t('delivery_refresh')}</Button></SafeAreaView>;
 if(!journal)return <SafeAreaView><ActivityIndicator accessibilityLabel={t('delivery_loading')}/><Button accessibilityRole="button" onPress={()=>{bootstrap.current?.abort();setFailed(true)}}>{t('read_cancel')}</Button></SafeAreaView>;
 return <DeliveryPackageScreen journal={journal} client={client} tenant={tenant} project={project} id={id} onRun={runID=>router.push({pathname:'/runs/[id]',params:{tenant,id:runID}})} onSign={exportID=>router.push({pathname:'/exports/sign',params:{tenant,export:exportID}})}/>;
}
