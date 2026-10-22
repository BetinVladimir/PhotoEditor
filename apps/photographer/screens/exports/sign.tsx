import {ActivityIndicator,Button,Text} from 'react-native-paper';
import {useEffect,useMemo,useState} from 'react';
import {router,useLocalSearchParams} from '../../src/localized-router';
import {View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {ApiClient} from '@photoeditor/api-client';
import {useAuth} from '../../src/auth-context';
import {useLocale} from '../../src/locale-context';
import {OwnerAPI} from '../../src/owner-api';
import {readDeadline} from '../../src/read-deadline';
import {SigningAttemptStore} from '../../src/signing-attempt-store';
import {SigningScreen} from '../../src/signing-screen';

export default function SigningRoute(){
 const params=useLocalSearchParams<{tenant:string;export:string}>(),auth=useAuth(),client=useMemo(()=>auth.client(),[auth]);
 const generation=useMemo(()=>crypto.randomUUID(),[client]);
 return <ScopedRoute key={JSON.stringify([generation,auth.origin,params.tenant,params.export])} client={client} origin={auth.origin} tenant={params.tenant||''} exportID={params.export||''}/>;
}
function ScopedRoute({client,origin,tenant,exportID}:{client:ApiClient;origin:string;tenant:string;exportID:string}){
 const {t}=useLocale(),[journal,setJournal]=useState<SigningAttemptStore|null>(null),[failed,setFailed]=useState(false),[attempt,setAttempt]=useState(0);
 useEffect(()=>{const controller=new AbortController();let live=true;setFailed(false);setJournal(null);
  void readDeadline(controller.signal,signal=>new OwnerAPI(client,tenant).me(signal)).then(user=>{if(live&&!controller.signal.aborted)setJournal(new SigningAttemptStore(AsyncStorage,origin,user.id,tenant,exportID));}).catch(()=>{if(live&&!controller.signal.aborted)setFailed(true);});
  return()=>{live=false;controller.abort();};
 },[client,origin,tenant,exportID,attempt]);
 if(failed)return <View><Text accessibilityRole="alert">{t('signing_failed')}</Text><Button accessibilityRole="button" onPress={()=>{setFailed(false);setAttempt(value=>value+1);}}>{t('retry')}</Button></View>;
 if(!journal)return <ActivityIndicator/>;
 return <SigningScreen client={client} tenant={tenant} journal={journal} onCreated={ticket=>router.replace({pathname:'/runs/[id]',params:{id:ticket.run_id,tenant}})}/>;
}
