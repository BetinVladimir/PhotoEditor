import {useLocale} from './locale-context';
import {Button,Text,TextInput} from 'react-native-paper';
import {useEffect,useRef,useState} from 'react';
import {View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import {RelightAttempt} from './relight-attempt';
import type {RelightAttemptStore} from './relight-attempt-store';
import {freezeRelightIntent,type RelightIntent,type RelightLight} from './relight-intent';
import type {RegionJobTicket} from './region-preview';
export type RelightLabels={title:string;approximate:string;strength:string;light:string;x:string;y:string;radius:string;ev:string;add:string;remove:string;submit:string;retry:string;open:string;loading:string;unknown:string;invalid:string;unavailable:string;another:string};
type Props={api:OwnerAPI;project:string;prepared:RelightIntent;store:RelightAttemptStore;labels:RelightLabels;disabled?:boolean;capabilityAvailable?:boolean;operationAvailable?:()=>boolean;verifyTicket:(ticket:RegionJobTicket,signal:AbortSignal)=>Promise<void>;onSubmitted:(ticket:RegionJobTicket)=>void;onAnother?:()=>void};
/** Mount only with authorized prepared original source/depth/mask bindings.
 * Preparation and acknowledgement validation remain owned by the parent flow.
 */
export function RelightControls(props:Props){
 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const [body,setBody]=useState(()=>freezeRelightIntent(props.prepared)),[attempt,setAttempt]=useState<RelightAttempt|null>(null),[ready,setReady]=useState(false),[busy,setBusy]=useState(false),[message,setMessage]=useState(''),[ticket,setTicket]=useState<RegionJobTicket|null>(null);
 const live=useRef(0),flight=useRef<AbortController|null>(null),locked=useRef(false);
 const disabledNow=useRef(props.disabled);disabledNow.current=props.disabled;
 const capabilityNow=useRef(props.capabilityAvailable);capabilityNow.current=props.capabilityAvailable;
 const availabilityNow=useRef(props.operationAvailable);availabilityNow.current=props.operationAvailable;
 const capabilityUnavailable=()=>capabilityNow.current===false||(availabilityNow.current!==undefined&&availabilityNow.current()!==true);
 const labels=props.labels;
 useEffect(()=>{
  const epoch=++live.current,controller=new AbortController();flight.current=controller;setReady(false);setAttempt(null);setTicket(null);setBusy(false);setMessage('');locked.current=false;
  void (async()=>{try{
   const saved=await props.store.read();if(controller.signal.aborted||live.current!==epoch)return;
   if(saved){
    const restored=RelightAttempt.restore(saved,props.store);
    if(saved.ticket)await props.verifyTicket(saved.ticket,controller.signal);
    if(controller.signal.aborted||live.current!==epoch)return;
    setBody(saved.body);setAttempt(restored);setTicket(saved.ticket);
   }else setBody(freezeRelightIntent(props.prepared));
   setReady(true);
  }catch{if(!controller.signal.aborted&&live.current===epoch)setMessage(labels.unavailable);}})();
  return()=>{live.current++;controller.abort();flight.current?.abort();flight.current=null;};
 },[props.store,props.api,props.project,props.prepared.snapshot_id,props.prepared.params.base.asset_version_id]);
 const canEdit=ready&&!busy&&!attempt&&!props.disabled&&props.capabilityAvailable!==false;
 function numeric(text:string,set:(value:number)=>void){if(text.trim()===''||!Number.isFinite(Number(text))){setMessage(labels.invalid);return;}setMessage('');set(Number(text));}
 function editLight(index:number,key:keyof RelightLight,value:number){if(!canEdit)return;setBody(old=>({...old,params:{...old.params,lights:old.params.lights.map((light,i)=>i===index?{...light,[key]:value}:light)}}));}
 async function submit(){
  if(!ready||locked.current||props.disabled||!ticket&&!attempt&&capabilityUnavailable())return;
  locked.current=true;setBusy(true);setMessage('');const epoch=live.current,controller=new AbortController();flight.current=controller;let staged=false;const recovering=attempt?.state==='unknown',recoveryOnly=recovering&&capabilityUnavailable();
  try{
   if(ticket){await props.verifyTicket(ticket,controller.signal);if(!controller.signal.aborted&&live.current===epoch&&!disabledNow.current)props.onSubmitted(ticket);return;}
   const current=attempt??new RelightAttempt(props.project,crypto.randomUUID(),freezeRelightIntent(body),props.store);
   setAttempt(current);staged=true;
   const accepted=await current.submit((project,request,key,signal)=>{if(controller.signal.aborted||live.current!==epoch||disabledNow.current||capabilityUnavailable()&&!recovering)throw new Error("RELIGHT_CAPABILITY_UNAVAILABLE");const replayOnly=recoveryOnly||recovering&&capabilityUnavailable();return props.api.submitRelight(project,request,key,signal,replayOnly)},controller.signal);
   // Server authority must be rechecked even when the attempt reused a local receipt.
   await props.verifyTicket(accepted,controller.signal);
   if(controller.signal.aborted||live.current!==epoch)return;
   setTicket(accepted);if(!disabledNow.current)props.onSubmitted(accepted);
  }catch{if(!controller.signal.aborted&&live.current===epoch)setMessage(staged||attempt?labels.unknown:labels.invalid);}
  finally{if(live.current===epoch){locked.current=false;setBusy(false);}if(flight.current===controller)flight.current=null;}
 }
 async function another(){
  if(!ticket||!attempt||locked.current||props.disabled||!props.onAnother)return;
  locked.current=true;setBusy(true);setMessage('');const epoch=live.current,controller=new AbortController();flight.current=controller;
  try{
   await props.verifyTicket(ticket,controller.signal);
   if(controller.signal.aborted||live.current!==epoch||disabledNow.current)return;
   await props.store.clear(attempt.snapshot.key);
   if(!controller.signal.aborted&&live.current===epoch&&!disabledNow.current)props.onAnother();
  }catch{if(!controller.signal.aborted&&live.current===epoch)setMessage(labels.unavailable);}
  finally{if(live.current===epoch){locked.current=false;setBusy(false);}if(flight.current===controller)flight.current=null;}
 }
 return <View accessibilityLabel={labels.title}>
  <Text>{labels.title}</Text><Text>{labels.approximate}</Text>
  {!ready&&!message?<Text>{labels.loading}</Text>:null}
  <Text>{labels.strength}</Text><TextInput accessibilityLabel={labels.strength} editable={canEdit} keyboardType="decimal-pad" value={String(body.params.strength)} onChangeText={text=>numeric(text,value=>{if(canEdit)setBody(old=>({...old,params:{...old.params,strength:value}}));})}/>
  {body.params.lights.map((light,index)=><View key={index}><Text>{labels.light} {index+1}</Text>{(['x','y','radius','ev'] as const).map(key=><View key={key}><Text>{labels[key]}</Text><TextInput accessibilityLabel={__uiT('ui_542e783f2e45', {p0:(labels.light),p1:(index+1),p2:(labels[key])})} editable={canEdit} keyboardType="numbers-and-punctuation" value={String(light[key])} onChangeText={text=>numeric(text,value=>editLight(index,key,value))}/></View>)}<Button accessibilityRole="button" disabled={!canEdit||body.params.lights.length===1} onPress={()=>setBody(old=>({...old,params:{...old.params,lights:old.params.lights.filter((_,i)=>i!==index)}}))} mode="outlined">{labels.remove}</Button></View>)}
  <Button accessibilityRole="button" disabled={!canEdit||body.params.lights.length>=4} onPress={()=>setBody(old=>({...old,params:{...old.params,lights:[...old.params.lights,{x:.5,y:.5,radius:.3,ev:0}]}}))} mode="outlined">{labels.add}</Button>
  {message?<Text accessibilityRole="alert">{__uiMessage(message)}</Text>:null}
  {ticket&&props.onAnother?<Button accessibilityRole="button" disabled={busy||props.disabled} onPress={()=>void another()} mode="outlined">{labels.another}</Button>:null}
  <Button accessibilityRole="button" disabled={!ready||busy||props.disabled||!ticket&&!attempt&&capabilityUnavailable()} onPress={()=>void submit()} mode="outlined">{ticket?labels.open:attempt?labels.retry:labels.submit}</Button>
 </View>;
}
