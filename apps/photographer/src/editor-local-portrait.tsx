import {useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,Pressable,Text,TextInput,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {V2VersionBinding} from './catalog-types';
import {collectReflectionMaskPairs,type ReflectionMaskPair} from './reflection-mask-pairs';
import {LocalPortraitAttemptStore} from './local-portrait-attempt-store';
import {LocalPortraitControls} from './local-portrait-controls';
import {freezeLocalPortraitSelection,verifyLocalPortraitRecoverySource,type LocalPortraitSelection,type LocalPortraitTicket} from './local-portrait-intent';

type Props={api:OwnerAPI;origin:string;tenant:string;project:string;asset:string;version:string;hairAvailable:boolean;clothingAvailable:boolean;disabled?:boolean;onSubmitted:(ticket:LocalPortraitTicket)=>void};
type Context={user:string;base:V2VersionBinding;snapshot:string;recovery:boolean};
export function EditorLocalPortrait(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.project,props.asset,props.version]);
 return <Picker key={scope} {...props}/>;
}
function Picker(props:Props){
 const [context,setContext]=useState<Context|null>(null),[pairs,setPairs]=useState<ReflectionMaskPair[]>([]),[pair,setPair]=useState<ReflectionMaskPair|null>(null);
 const [operation,setOperation]=useState<'portrait.hair'|'portrait.clothing'>(props.hairAvailable?'portrait.hair':'portrait.clothing');
 const [strength,setStrength]=useState(.5),[hue,setHue]=useState(0),[saturation,setSaturation]=useState(1),[detail,setDetail]=useState(.3),[wrinkle,setWrinkle]=useState(.3);
 const [selection,setSelection]=useState<LocalPortraitSelection|null>(null),[busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const active=useRef(true),flight=useRef<AbortController|null>(null);
 useEffect(()=>{
  active.current=true;const controller=new AbortController();flight.current=controller;
  void(async()=>{try{
   if(!props.hairAvailable&&!props.clothingAvailable)throw new Error('LOCAL_PORTRAIT_CAPABILITY_UNAVAILABLE');
   const user=await props.api.me(controller.signal);
   const base=await props.api.v2Binding(props.asset,controller.signal);
   if(base.asset_version_id!==props.version)throw new Error('LOCAL_PORTRAIT_SOURCE_CHANGED');
   const journal=new LocalPortraitAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.project,props.version);
   const saved=await journal.read();if(!active.current||controller.signal.aborted)return;
   if(saved){verifyLocalPortraitRecoverySource(saved.body,base);
    if(saved.body.operation==='portrait.hair'&&!props.hairAvailable||saved.body.operation==='portrait.clothing'&&!props.clothingAvailable)throw new Error('LOCAL_PORTRAIT_CAPABILITY_UNAVAILABLE');
    setContext({user:user.id,base,snapshot:saved.body.snapshot_id,recovery:true});return;}
   const snapshot=await props.api.createRenderInputSnapshot(props.project,{sources:[base]},crypto.randomUUID(),controller.signal);
   const rows=await collectReflectionMaskPairs((query,signal)=>props.api.localPortraitMaskPairs(props.project,query,signal),{snapshot_id:snapshot.id,base,after:null},controller.signal);
   if(active.current&&!controller.signal.aborted){setContext({user:user.id,base,snapshot:snapshot.id,recovery:false});setPairs(rows);}
  }catch(error){if(active.current&&!controller.signal.aborted)setMessage(error instanceof Error&&error.message==='LOCAL_PORTRAIT_CAPABILITY_UNAVAILABLE'?'Local portrait is unavailable on this deployment.':'Local portrait mask choices are unavailable.');}
  finally{if(active.current&&!controller.signal.aborted)setBusy(false);}})();
  return()=>{active.current=false;controller.abort();flight.current?.abort();};
 },[props.api,props.origin,props.tenant,props.project,props.asset,props.version,props.hairAvailable,props.clothingAvailable]);
 function confirm(){
  if(!context||!pair||busy||props.disabled)return;
  try{
   const shared={schema_version:1 as const,snapshot_id:context.snapshot,ordinal:0,target_pixel:pair.target_pixel,protected_pixel:pair.protected_pixel};
   const params={schema_version:1 as const,base:context.base,strength,mask:pair.mask,protected:pair.protected,hue_delta_degrees:hue,saturation_factor:saturation};
   const selected:LocalPortraitSelection=operation==='portrait.hair'
    ?{...shared,operation,params:{...params,detail}}
    :{...shared,operation,params:{...params,wrinkle_strength:wrinkle}};
   setSelection(freezeLocalPortraitSelection(selected));setMessage('');
  }catch{setMessage('Local portrait selection is invalid.');}
 }
 if(context?.recovery)return <LocalPortraitControls api={props.api} origin={props.origin} user={context.user} tenant={props.tenant} project={props.project} baseVersion={props.version} disabled={props.disabled} onSubmitted={props.onSubmitted}/>;
 if(context&&selection)return <LocalPortraitControls api={props.api} origin={props.origin} user={context.user} tenant={props.tenant} project={props.project} baseVersion={props.version} selection={selection} disabled={props.disabled} onSubmitted={props.onSubmitted}/>;
 const numeric=(label:string,value:number,set:(value:number)=>void,lo:number,hi:number)=><View><Text>{label}</Text><TextInput accessibilityLabel={label} editable={!busy&&!props.disabled} keyboardType="decimal-pad" value={String(value)} onChangeText={raw=>{const next=Number(raw);if(raw.trim()!==''&&Number.isFinite(next)&&next>=lo&&next<=hi)set(next);}}/></View>;
 return <View><Text accessibilityRole="header">Local portrait adjustments</Text><Text>Select an original manual target and protected mask pair.</Text>
  {message?<Text accessibilityRole="alert">{message}</Text>:null}{busy?<ActivityIndicator accessibilityLabel="Loading local portrait masks"/>:null}
  <Pressable accessibilityRole="radio" accessibilityState={{checked:operation==='portrait.hair'}} disabled={busy||props.disabled||!props.hairAvailable} onPress={()=>{setOperation('portrait.hair');setHue(0);setSaturation(1);}}><Text>Hair</Text></Pressable>
  <Pressable accessibilityRole="radio" accessibilityState={{checked:operation==='portrait.clothing'}} disabled={busy||props.disabled||!props.clothingAvailable} onPress={()=>{setOperation('portrait.clothing');setHue(0);setSaturation(1);}}><Text>Clothing</Text></Pressable>
  {context&&pairs.length===0&&!busy?<Text>Create a target and protected mask first.</Text>:null}
  {pairs.map((item,index)=><Pressable key={item.mask.artifact_id} accessibilityRole="radio" accessibilityState={{checked:pair?.mask.artifact_id===item.mask.artifact_id}} disabled={busy||props.disabled} onPress={()=>setPair(item)}><Text>{`Mask pair ${index+1}`}</Text></Pressable>)}
  {numeric('Strength',strength,setStrength,0,1)}
  {numeric('Hue degrees',hue,setHue,operation==='portrait.hair'?-30:-180,operation==='portrait.hair'?30:180)}
  {numeric('Saturation',saturation,setSaturation,operation==='portrait.hair'?0.7:0,operation==='portrait.hair'?1.3:2)}
  {operation==='portrait.hair'?numeric('Detail',detail,setDetail,0,1):numeric('Wrinkle reduction',wrinkle,setWrinkle,0,1)}
  <Pressable accessibilityRole="button" disabled={busy||props.disabled||!pair||operation==='portrait.hair'&&!props.hairAvailable||operation==='portrait.clothing'&&!props.clothingAvailable} onPress={confirm}><Text>Review local portrait request</Text></Pressable>
 </View>;
}
