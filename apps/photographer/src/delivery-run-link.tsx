import {ActivityIndicator,Button,Text} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import {readDeadline} from './read-deadline';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';

type Props={api:OwnerAPI;run:string;workflow:string;project:string;onOpen:(id:string)=>void};
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
export function DeliveryRunLink({api,run,workflow,project,onOpen}:Props){
 const{t}=useLocale(),[epoch,setEpoch]=useState(0);
 const scope=useMemo(()=>({}),[api,run,workflow,project,epoch]);
 const currentScope=useRef(scope);currentScope.current=scope;
 const flight=useRef<AbortController|null>(null);
 const[state,setState]=useState<{scope:object;id:string|null;phase:'loading'|'ready'|'error'}>({scope,id:null,phase:'loading'});
 const visible=state.scope===scope?state:{scope,id:null,phase:'loading' as const};
 useEffect(()=>{
  const controller=new AbortController();flight.current=controller;let live=true;
  const current=()=>live&&currentScope.current===scope&&flight.current===controller&&!controller.signal.aborted;
  setState({scope,id:null,phase:'loading'});
  void readDeadline(controller.signal,async signal=>{
   if(!uuid.test(run)||!uuid.test(workflow)||!uuid.test(project))throw new Error('DELIVERY_RUN_SCOPE_INVALID');
   const status=await api.get<unknown>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(workflow)}`,signal);signal.throwIfAborted();
   if(!status||typeof status!=='object'||Array.isArray(status)||(status as Record<string,unknown>).run_id!==run||(status as Record<string,unknown>).workflow_id!==workflow)throw new Error('DELIVERY_RUN_SCOPE_INVALID');
   const pkg=await api.deliveryPackageForWorkflow(project,workflow,signal);signal.throwIfAborted();
   return pkg.id;
  }).then(id=>{if(current())setState({scope,id,phase:'ready'});},()=>{if(current())setState({scope,id:null,phase:'error'});})
   .finally(()=>{if(flight.current===controller)flight.current=null;});
  return()=>{live=false;controller.abort();if(flight.current===controller)flight.current=null;};
 },[api,run,workflow,project,scope]);
 function stop(){const controller=flight.current;if(currentScope.current!==scope||!controller)return;controller.abort();if(flight.current===controller)flight.current=null;setState({scope,id:null,phase:'error'});}
 return <View>{visible.id?<Button  accessibilityRole="button"  onPress={()=>{if(currentScope.current===scope&&visible.id)onOpen(visible.id);}} mode="contained" contentStyle={{minHeight:48}}>{t('delivery_title')}</Button>:null}
  {visible.phase==='loading'?<><ActivityIndicator/><Button  accessibilityRole="button"  onPress={stop} mode="contained" contentStyle={{minHeight:48}}>{t('read_cancel')}</Button></>:null}
  {visible.phase==='error'?<><Text accessibilityRole="alert" style={s.error}>{t('delivery_load_failed')}</Text><Button  accessibilityRole="button"  onPress={()=>setEpoch(value=>value+1)} mode="contained" contentStyle={{minHeight:48}}>{t('delivery_refresh')}</Button></>:null}</View>;
}
