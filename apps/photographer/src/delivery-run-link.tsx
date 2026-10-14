import {useEffect,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';

type Props={api:OwnerAPI;run:string;workflow:string;project:string;onOpen:(id:string)=>void};
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
export function DeliveryRunLink({api,run,workflow,project,onOpen}:Props){
 const{t}=useLocale(),[id,setID]=useState<string|null>(null),[error,setError]=useState(false),[epoch,setEpoch]=useState(0);
 useEffect(()=>{
  const controller=new AbortController();let live=true;
  setID(null);setError(false);
  void(async()=>{try{
   if(!uuid.test(run)||!uuid.test(workflow)||!uuid.test(project))throw new Error('DELIVERY_RUN_SCOPE_INVALID');
   const status=await api.get<unknown>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(workflow)}`,controller.signal);
   if(!status||typeof status!=='object'||Array.isArray(status)||(status as Record<string,unknown>).run_id!==run||(status as Record<string,unknown>).workflow_id!==workflow)throw new Error('DELIVERY_RUN_SCOPE_INVALID');
   const pkg=await api.deliveryPackageForWorkflow(project,workflow,controller.signal);
   if(live&&!controller.signal.aborted)setID(pkg.id);
  }catch{if(live&&!controller.signal.aborted)setError(true)}})();
  return()=>{live=false;controller.abort()};
 },[api,run,workflow,project,epoch]);
 return <View>{id?<Pressable accessibilityRole="button" style={s.button} onPress={()=>onOpen(id)}><Text style={s.buttonText}>{t('delivery_title')}</Text></Pressable>:null}
  {error?<><Text accessibilityRole="alert" style={s.error}>{t('delivery_load_failed')}</Text><Pressable accessibilityRole="button" style={s.button} onPress={()=>setEpoch(value=>value+1)}><Text style={s.buttonText}>{t('delivery_refresh')}</Text></Pressable></>:null}</View>;
}
