import {useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,Pressable,Text,TextInput,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {V2VersionBinding} from './catalog-types';
import {collectReflectionMaskPairs,type ReflectionMaskPair} from './reflection-mask-pairs';
import {ReflectionAttemptStore} from './reflection-attempt-store';
import {ReflectionControls} from './reflection-controls';
import {freezeReflectionPreparation,verifyReflectionRecoverySource,type ReflectionPreparation,type ReflectionTicket} from './reflection-intent';

type Props={api:OwnerAPI;origin:string;tenant:string;project:string;asset:string;version:string;disabled?:boolean;onSubmitted:(ticket:ReflectionTicket)=>void};
type Context={user:string;base:V2VersionBinding;snapshot:string;recovery:boolean};

export function EditorReflection(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.project,props.asset,props.version]);
 return <Picker key={scope} {...props}/>;
}

function Picker(props:Props){
 const [context,setContext]=useState<Context|null>(null),[pairs,setPairs]=useState<ReflectionMaskPair[]>([]),[pair,setPair]=useState<ReflectionMaskPair|null>(null);
 const [strength,setStrength]=useState(.5),[selection,setSelection]=useState<ReflectionPreparation|null>(null);
 const [busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const active=useRef(true),flight=useRef<AbortController|null>(null);
 useEffect(()=>{
  active.current=true;const controller=new AbortController();flight.current=controller;
  void(async()=>{try{
   const user=await props.api.me(controller.signal);
   const base=await props.api.v2Binding(props.asset,controller.signal);
   if(base.asset_version_id!==props.version)throw new Error('REFLECTION_SOURCE_CHANGED');
   const journal=new ReflectionAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.project,props.version);
   const saved=await journal.read();if(!active.current||controller.signal.aborted)return;
   if(saved){verifyReflectionRecoverySource(saved.body,base);setContext({user:user.id,base,snapshot:saved.body.snapshot_id,recovery:true});return;}
   const snapshot=await props.api.createRenderInputSnapshot(props.project,{sources:[base]},crypto.randomUUID(),controller.signal);
   const rows=await collectReflectionMaskPairs((query,signal)=>props.api.reflectionMaskPairs(props.project,query,signal),{snapshot_id:snapshot.id,base,after:null},controller.signal);
   if(active.current&&!controller.signal.aborted){setContext({user:user.id,base,snapshot:snapshot.id,recovery:false});setPairs(rows);}
  }catch{if(active.current&&!controller.signal.aborted)setMessage('Reflection mask choices are unavailable.');}
  finally{if(active.current&&!controller.signal.aborted)setBusy(false);}})();
  return()=>{active.current=false;controller.abort();flight.current?.abort();};
 },[props.api,props.origin,props.tenant,props.project,props.asset,props.version]);
 function confirm(){
  if(!context||!pair||busy||props.disabled)return;
  try{setSelection(freezeReflectionPreparation({schema_version:1,snapshot_id:context.snapshot,ordinal:0,base:context.base,strength,
   mask:pair.mask,protected:pair.protected,target_pixel:pair.target_pixel,protected_pixel:pair.protected_pixel}));setMessage('');}
  catch{setMessage('Reflection selection is invalid.');}
 }
 if(context?.recovery)return <ReflectionControls api={props.api} origin={props.origin} user={context.user} tenant={props.tenant} project={props.project} baseVersion={props.version} disabled={props.disabled} onSubmitted={props.onSubmitted}/>;
 if(context&&selection)return <ReflectionControls api={props.api} origin={props.origin} user={context.user} tenant={props.tenant} project={props.project} baseVersion={props.version} selection={selection} disabled={props.disabled} onSubmitted={props.onSubmitted}/>;
 return <View><Text accessibilityRole="header">Reflection removal</Text><Text>Select a target and protected mask pair for this image.</Text>
  {message?<Text accessibilityRole="alert">{message}</Text>:null}{busy?<ActivityIndicator accessibilityLabel="Loading reflection masks"/>:null}
  {context&&pairs.length===0&&!busy?<Text>Create a target and protected mask first.</Text>:null}
  {pairs.map((item,index)=><Pressable key={item.mask.artifact_id} accessibilityRole="radio" accessibilityState={{checked:pair?.mask.artifact_id===item.mask.artifact_id}} disabled={busy||props.disabled} onPress={()=>setPair(item)}><Text>{`Mask pair ${index+1}`}</Text></Pressable>)}
  <Text>Strength</Text><TextInput accessibilityLabel="Reflection strength" editable={!busy&&!props.disabled} keyboardType="decimal-pad" value={String(strength)} onChangeText={value=>{const number=Number(value);if(value.trim()!==''&&Number.isFinite(number)&&number>=0&&number<=1)setStrength(number);}}/>
  <Pressable accessibilityRole="button" disabled={busy||props.disabled||!pair} onPress={confirm}><Text>Review reflection request</Text></Pressable>
 </View>;
}
