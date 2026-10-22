import {Button,Text,TextInput} from 'react-native-paper';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from '../../../src/localized-router';
import {SafeAreaView,ScrollView,View} from 'react-native';
import {APIError} from '@photoeditor/api-client';
import {useAuth} from '../../../src/auth-context';
import {useLocale} from '../../../src/locale-context';
import {OwnerAPI} from '../../../src/owner-api';
import {useReadResource} from '../../../src/use-read-resource';
import {readDeadline} from '../../../src/read-deadline';
import {ReadResourceState} from '../../../src/read-resource-state';
import {TrainingAttemptStore,type TrainingCommand} from '../../../src/training-attempt-store';
import {readTrainingSet,resumeTraining} from '../../../src/training-recovery';
import {catalogStyle as s} from '../../../src/catalog-style';
export default function Training(){
 const __displayValue=useLocale().value;

 const __uiT=useLocale().t;

 const{id,tenant}=useLocalSearchParams<{id:string;tenant:string}>(),auth=useAuth(),{t}=useLocale(),api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 const resource=useReadResource(`${auth.origin}:${tenant}:${id}`,api,async signal=>{
  if(!tenant||!id)throw new Error('Training scope required');const user=await api.me(signal),style=await api.style(id,signal);signal.throwIfAborted();
  if(style.id!==id||style.tenant_id!==tenant)throw new Error('Training style invalid');const access=await api.projectAccess(style.project_id,signal);signal.throwIfAborted();
  const store=new TrainingAttemptStore(AsyncStorage,auth.origin,user.id,tenant,id),state=await store.read();signal.throwIfAborted();
  const set=state.set_id?await readTrainingSet(api,state.set_id,id,signal):undefined;return{store,state,set,canEdit:access.can_edit};
 });
 const data=resource.data,[before,setBefore]=useState(''),[after,setAfter]=useState(''),[session,setSession]=useState('default'),[busy,setBusy]=useState(false),[error,setError]=useState(false),flight=useRef<AbortController|null>(null);
 // Cancel/refresh invalidates the action, but keeps the photographer's pair draft.
 useEffect(()=>{flight.current?.abort();flight.current=null;setBusy(false);setError(false);return()=>{flight.current?.abort();flight.current=null;};},[api,id,tenant,resource.phase]);
 useEffect(()=>{setBefore('');setAfter('');setSession('default');},[api,id,tenant]);
 async function execute(command?:TrainingCommand){
  if(!data||flight.current||!resource.current()||!data.canEdit&&!data.state.pending?.ticket)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError(false);
  const current=()=>!controller.signal.aborted&&flight.current===controller&&resource.current();
  const timeout=setTimeout(()=>{if(current()){controller.abort();flight.current=null;setBusy(false);setError(true);resource.retry();}},15000);
  let state=data.state;
  try{
   if(command){
    if(command.kind==='analyze'){
     const descriptor=await readDeadline(controller.signal,signal=>api.analysisCapabilities(id,command.body.analysis_options.geometry_enabled,signal));
     if(!current())return;if(!descriptor.available){setError(true);return;}
    }
    state=await data.store.begin(command);if(!current())return;resource.commit({...data,state});}
   const result=await resumeTraining(api,id,data.store,state,controller.signal,current);if(!current())return;
   resource.commit({...data,state:result.state,set:result.set??data.set});setBefore('');setAfter('');
   if(result.state.last_run_id)router.replace({pathname:'/runs/[id]',params:{id:result.state.last_run_id,tenant}});
  }catch(cause){
   if(current()){
    if(cause instanceof APIError&&[401,403,404,410].includes(cause.status)){resource.retry();return;}
    if(state.pending&&!state.pending.ticket&&cause instanceof APIError&&[409,412,422].includes(cause.status))await data.store.reject(state.pending.key).catch(()=>{});
    if(!current())return;
    const saved=await data.store.read().catch(()=>state);if(!current())return;resource.commit({...data,state:saved});setError(true);
   }
  }finally{clearTimeout(timeout);if(flight.current===controller){flight.current=null;if(resource.current())setBusy(false);}}
 }
 async function newSet(){
  if(!data||flight.current||!data.canEdit||!resource.current())return;
  const controller=new AbortController();flight.current=controller;setBusy(true);
  try{const state=await readDeadline(controller.signal,()=>data.store.reset());if(!controller.signal.aborted&&flight.current===controller&&resource.current())resource.commit({...data,state,set:undefined});}
  catch{if(!controller.signal.aborted&&resource.current())setError(true);}
  finally{if(flight.current===controller){flight.current=null;if(resource.current())setBusy(false);}}
 }
 function analyze(){if(!data?.set)return;const revision=Number(data.set.revision);if(!Number.isSafeInteger(revision)||revision<1){setError(true);return;}void execute({kind:'analyze',set_id:data.set.id,body:{training_set_id:data.set.id,training_set_revision:revision,analysis_options:{alignment_mode:'crop_aware',geometry_enabled:true,seed:0}}});}
 if(!data)return <SafeAreaView style={s.root}><ReadResourceState {...resource}/></SafeAreaView>;
 const locked=busy||!!data.state.pending||!data.canEdit||!!data.state.last_run_id;
 return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={s.header}><Text style={s.title}>{__uiT('ui_a88ea8516de7')}</Text><Text style={s.subtitle}>{__uiT('ui_3e403e20f5ee')}</Text>
 {data.state.pending?<Button  accessibilityRole="button" disabled={busy||!data.canEdit&&!data.state.pending.ticket} onPress={()=>void execute()}  mode="contained" contentStyle={{minHeight:48}}>{t('training_recover')}</Button>:null}
 {!data.set?<Button  accessibilityRole="button" disabled={locked} onPress={()=>void execute({kind:'create',body:{}})}  mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_4a2f35d58a1d')}</Button>:<><Text style={s.name}>{data.set.pairs.items.length} {__uiT('ui_42bab22990e1')}{' '}{data.set.revision}</Text><TextInput editable={!locked} accessibilityLabel={__uiT('ui_3fbb75228e8e')} value={before} onChangeText={setBefore} autoCapitalize="none" placeholder={__uiT('ui_3fbb75228e8e')} placeholderTextColor="#888" style={s.input}/><TextInput editable={!locked} accessibilityLabel={__uiT('ui_d8450b4b0c0a')} value={after} onChangeText={setAfter} autoCapitalize="none" placeholder={__uiT('ui_d8450b4b0c0a')} placeholderTextColor="#888" style={s.input}/><TextInput editable={!locked} accessibilityLabel={__uiT('ui_cb9ac5c561da')} value={session} onChangeText={setSession} placeholder={__uiT('ui_cb9ac5c561da')} placeholderTextColor="#888" style={s.input}/><Button  accessibilityRole="button" disabled={locked||!before.trim()||!after.trim()} onPress={()=>void execute({kind:'pair',set_id:data.set!.id,body:{before_asset_version_id:before.trim(),after_asset_version_id:after.trim(),session_id:session.trim(),confirmed:true,included:true}})}  mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_c585263058fc')}</Button><Button  accessibilityRole="button" disabled={locked||!data.set.pairs.items.some(pair=>pair.confirmed&&pair.included)} onPress={analyze}  mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_176b243f59f2')}</Button>{data.set.pairs.items.map(pair=><View key={pair.id} style={s.row}><View><Text style={s.name}>{pair.session_id}</Text><Text style={s.meta}>{__displayValue(pair.validation_status)} · {pair.confirmed?__uiT('ui_9599915bd8cd'):__uiT('ui_c78a686d7cf5')}</Text></View></View>)}</>}
 {busy?<Button  accessibilityRole="button" onPress={()=>{flight.current?.abort();flight.current=null;setBusy(false);setError(true);resource.retry();}}  mode="contained" contentStyle={{minHeight:48}}>{t('read_cancel')}</Button>:null}
 {data.state.last_run_id?<><Button  accessibilityRole="button"  onPress={()=>router.push({pathname:'/runs/[id]',params:{id:data.state.last_run_id!,tenant}})} mode="contained" contentStyle={{minHeight:48}}>{t('training_open_run')}</Button><Button  accessibilityRole="button" disabled={busy||!data.canEdit}  onPress={()=>void newSet()} mode="contained" contentStyle={{minHeight:48}}>{t('training_new_set')}</Button></>:null}
 {error?<Text accessibilityRole="alert" style={s.error}>{t('read_error')}</Text>:null}<Button labelStyle={s.buttonText} accessibilityRole="button" disabled={busy} onPress={resource.retry}>{t('read_retry')}</Button>
 </ScrollView></SafeAreaView>;
}
