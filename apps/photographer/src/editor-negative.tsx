import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,Pressable,Text,TextInput,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import {prepareNextNegative} from './negative-next';
import {NegativeAttempt} from './negative-attempt';
import {NegativeAttemptStore} from './negative-attempt-store';
import {freezeNegativeIntent,type NegativeIntent} from './negative-intent';
import type {RegionJobTicket} from './region-preview';
import {useLocale} from './locale-context';
type Props={api:OwnerAPI;origin:string;tenant:string;project:string;asset:string;version:string;disabled?:boolean;onSubmitted:(ticket:RegionJobTicket)=>void};
export function EditorNegative(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.project,props.asset,props.version]);
 const [generation,setGeneration]=useState(0),[foreground,setForeground]=useState((AppState?.currentState??'active')==='active');
 useEffect(()=>{if(!AppState?.addEventListener)return;const listener=AppState.addEventListener('change',state=>{setForeground(state==='active');if(state==='active')setGeneration(n=>n+1)});return()=>listener.remove()},[]);
 return foreground?<NegativeScope key={`${scope}:${generation}`} {...props}/>:null;
}
function NegativeScope(props:Props){
 const {t}=useLocale();
 const [body,setBody]=useState<NegativeIntent|null>(null),[store,setStore]=useState<NegativeAttemptStore|null>(null),[attempt,setAttempt]=useState<NegativeAttempt|null>(null),[busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const [values,setValues]=useState({strength:'1',exposure:'0',red:'1',green:'1',blue:'1'});
 const live=useRef(true),locked=useRef(false),flight=useRef<AbortController|null>(null);
 useEffect(()=>{
  const controller=new AbortController();flight.current=controller;live.current=true;
  const active=()=>live.current&&!controller.signal.aborted;
  void(async()=>{try{
   const user=await props.api.me(controller.signal);if(!active())return;
   const journal=new NegativeAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.project,props.version);
   const saved=await journal.read();if(!active())return;setStore(journal);
   if(saved){setBody(saved.body);setAttempt(NegativeAttempt.restore(saved,journal));setValues({strength:String(saved.body.params.strength),exposure:String(saved.body.params.exposure_ev),red:String(saved.body.params.film_base_rgb[0]),green:String(saved.body.params.film_base_rgb[1]),blue:String(saved.body.params.film_base_rgb[2])});return}
   const configured=await props.api.negativeCapabilities(props.project,controller.signal);if(!active())return;
   const base=await props.api.v2Binding(props.asset,controller.signal);if(!active())return;
   if(base.asset_version_id!==props.version)throw new Error('NEGATIVE_SOURCE_CHANGED');
   const snapshot=await props.api.createRenderInputSnapshot(props.project,{sources:[base]},crypto.randomUUID(),controller.signal);if(!active())return;
   setBody(freezeNegativeIntent({schema_version:1,snapshot_id:snapshot.id,params:{schema_version:1,base,strength:1,film_base_rgb:[1,1,1],calibration:configured.calibration,exposure_ev:0}}));
  }catch{if(active())setMessage(t('negative_unavailable'))}finally{if(active())setBusy(false)}})();
  return()=>{live.current=false;controller.abort();flight.current?.abort()};
 },[]);
 async function submit(){
  if(!body||!store||locked.current||busy||props.disabled)return;
  locked.current=true;setBusy(true);setMessage('');const controller=new AbortController();flight.current=controller;let staged=!!attempt;
  try{
   if(!attempt&&Object.values(values).some(v=>v.trim()===''||!Number.isFinite(Number(v))))throw new Error('NEGATIVE_INTENT_INVALID');
   const next=attempt?.snapshot.body??freezeNegativeIntent({...body,params:{...body.params,strength:Number(values.strength),exposure_ev:Number(values.exposure),film_base_rgb:[Number(values.red),Number(values.green),Number(values.blue)]}});
   const current=attempt??new NegativeAttempt(props.project,crypto.randomUUID(),next,store);
   setBody(next);setAttempt(current);staged=true;
   const ticket=await current.submit((project,request,key,signal)=>props.api.submitNegative(project,request,key,signal),controller.signal);
   if(!live.current||controller.signal.aborted)return;
   const status=await props.api.get<{run_id:string;workflow_id:string}>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(ticket.workflow_id)}`,controller.signal);
   if(status.run_id!==ticket.run_id||status.workflow_id!==ticket.workflow_id)throw new Error('NEGATIVE_TICKET_SCOPE_CHANGED');
   if(live.current&&!controller.signal.aborted)props.onSubmitted(ticket);
  }catch{if(live.current&&!controller.signal.aborted)setMessage(t(staged?'negative_unknown':'negative_invalid'))}
  finally{locked.current=false;if(live.current)setBusy(false)}
 }
 async function another(){
  if(!attempt||attempt.state!=='accepted'||!store||locked.current||busy||props.disabled)return;
  locked.current=true;setBusy(true);setMessage('');const controller=new AbortController();flight.current=controller;
  try{
   const next=await prepareNextNegative(props.api,props.asset,attempt.snapshot,store,controller.signal);
   if(!live.current||controller.signal.aborted)return;
   setBody(next);setAttempt(null);
  }catch{if(live.current&&!controller.signal.aborted)setMessage(t('negative_unavailable'))}
  finally{locked.current=false;if(live.current)setBusy(false)}
 }
 const editable=!!body&&!attempt&&!busy&&!props.disabled;
 return <View><Text>{t('negative_title')}</Text><Text>{t('negative_hint')}</Text>
 {body?(['strength','exposure','red','green','blue'] as const).map(field=><View key={field}><Text>{t(`negative_${field}`)}</Text><TextInput accessibilityLabel={t(`negative_${field}`)} keyboardType="decimal-pad" editable={editable} value={values[field]} onChangeText={value=>{if(editable)setValues(old=>({...old,[field]:value}))}}/></View>):null}
 {message?<Text accessibilityRole="alert">{message}</Text>:null}
 <Pressable accessibilityRole="button" disabled={!body||!store||busy||props.disabled} onPress={()=>void submit()}><Text>{t(attempt?.state==='accepted'?'negative_open':attempt?'negative_retry':'negative_submit')}</Text></Pressable>
 {attempt?.state==='accepted'?<Pressable accessibilityRole="button" disabled={busy||props.disabled} onPress={()=>void another()}><Text>{t('negative_another')}</Text></Pressable>:null}
 </View>;
}
