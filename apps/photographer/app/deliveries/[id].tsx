import AsyncStorage from '@react-native-async-storage/async-storage';
import {ActivityIndicator,Text} from 'react-native';
import type {ApiClient} from '@photoeditor/api-client';
import {DeliveryPublishAttemptStore} from '../../src/delivery-publish-attempt-store';
import {OwnerAPI} from '../../src/owner-api';
import {useLocale} from '../../src/locale-context';
import {useEffect,useMemo,useState} from 'react';
import {router,useLocalSearchParams} from 'expo-router';
import {useAuth} from '../../src/auth-context';
import {DeliveryPackageScreen} from '../../src/delivery-package-screen';
export default function DeliveryRoute(){
 const {tenant,project,id}=useLocalSearchParams<{tenant:string;project:string;id:string}>();
 const auth=useAuth(),client=useMemo(()=>auth.client(),[auth]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 return <ScopedDelivery key={JSON.stringify([generation,auth.origin,tenant,project,id])} client={client} origin={auth.origin} tenant={tenant||''} project={project||''} id={id||''}/>;
}

function ScopedDelivery({client,origin,tenant,project,id}:{client:ApiClient;origin:string;tenant:string;project:string;id:string}){
 const {t}=useLocale(),[journal,setJournal]=useState<DeliveryPublishAttemptStore|null>(null),[failed,setFailed]=useState(false);
 useEffect(()=>{const controller=new AbortController();let live=true;
  void new OwnerAPI(client,tenant).me(controller.signal).then(user=>{
   if(live&&!controller.signal.aborted)setJournal(new DeliveryPublishAttemptStore(AsyncStorage,origin,user.id,tenant,project,id));
  }).catch(()=>{if(live&&!controller.signal.aborted)setFailed(true)});
  return()=>{live=false;controller.abort()};
 },[client,origin,tenant,project,id]);
 if(failed)return <Text accessibilityRole="alert">{t('delivery_load_failed')}</Text>;
 if(!journal)return <ActivityIndicator/>;
 return <DeliveryPackageScreen journal={journal} client={client} tenant={tenant} project={project} id={id} onSign={exportID=>router.push({pathname:'/exports/sign',params:{tenant,export:exportID}})}/>;
}
