import {useLocale} from './locale-context';
import {TouchableRipple,Button,ActivityIndicator,Text,TextInput} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {V2VersionBinding} from './catalog-types';
import {collectReflectionMaskPairs,type ReflectionMaskPair} from './reflection-mask-pairs';
import {ReflectionAttemptStore} from './reflection-attempt-store';
import {ReflectionControls} from './reflection-controls';
import {freezeReflectionPreparation,verifyReflectionRecoverySource,type ReflectionPreparation,type ReflectionTicket} from './reflection-intent';

type Props={api:OwnerAPI;origin:string;tenant:string;project:string;asset:string;version:string;disabled?:boolean;operationAvailable?:()=>boolean;onSubmitted:(ticket:ReflectionTicket)=>void};
type Context={user:string;base:V2VersionBinding;snapshot:string;recovery:boolean};

export function EditorReflection(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.project,props.asset,props.version]);
 const [generation,setGeneration]=useState(0),[foreground,setForeground]=useState((AppState?.currentState??'active')==='active');
 useEffect(()=>{if(!AppState?.addEventListener)return;const listener=AppState.addEventListener('change',state=>{setForeground(state==='active');if(state==='active')setGeneration(value=>value+1);});return()=>listener.remove();},[]);
 return foreground?<Picker key={`${scope}:${generation}`} {...props}/>:null;
}

function Picker(props:Props){
 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const [context,setContext]=useState<Context|null>(null),[pairs,setPairs]=useState<ReflectionMaskPair[]>([]),[pair,setPair]=useState<ReflectionMaskPair|null>(null);
 const [strength,setStrength]=useState(.5),[selection,setSelection]=useState<ReflectionPreparation|null>(null);
 const [busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const active=useRef(true),flight=useRef<AbortController|null>(null);
 useEffect(()=>{
  active.current=true;const controller=new AbortController();flight.current=controller;
  void(async()=>{try{
   const user=await props.api.me(controller.signal);if(!active.current||controller.signal.aborted)return;
   const base=await props.api.v2Binding(props.asset,controller.signal);if(!active.current||controller.signal.aborted)return;
   if(base.asset_version_id!==props.version)throw new Error('REFLECTION_SOURCE_CHANGED');
   const journal=new ReflectionAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.project,props.version);
   const saved=await journal.read();if(!active.current||controller.signal.aborted)return;
   if(saved){verifyReflectionRecoverySource(saved.body,base);setContext({user:user.id,base,snapshot:saved.body.snapshot_id,recovery:true});return;}
   const snapshot=await props.api.createRenderInputSnapshot(props.project,{sources:[base]},crypto.randomUUID(),controller.signal);if(!active.current||controller.signal.aborted)return;
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
 if(context?.recovery)return <ReflectionControls api={props.api} origin={props.origin} user={context.user} tenant={props.tenant} project={props.project} baseVersion={props.version} disabled={props.disabled} operationAvailable={props.operationAvailable} onSubmitted={props.onSubmitted}/>;
 if(context&&selection)return <ReflectionControls api={props.api} origin={props.origin} user={context.user} tenant={props.tenant} project={props.project} baseVersion={props.version} selection={selection} disabled={props.disabled} operationAvailable={props.operationAvailable} onSubmitted={props.onSubmitted}/>;
 return <View><Text accessibilityRole="header">{__uiT('ui_15f86809009f')}</Text><Text>{__uiT('ui_c8c21fd34bdd')}</Text>
  {message?<Text accessibilityRole="alert">{__uiMessage(message)}</Text>:null}{busy?<ActivityIndicator accessibilityLabel={__uiT('ui_f5fdf1a90200')}/>:null}
  {context&&pairs.length===0&&!busy?<Text>{__uiT('ui_1b9c085c1171')}</Text>:null}
  {pairs.map((item,index)=><TouchableRipple aria-checked={pair?.mask.artifact_id===item.mask.artifact_id} key={item.mask.artifact_id} accessibilityRole="radio" accessibilityState={{checked:pair?.mask.artifact_id===item.mask.artifact_id}} disabled={busy||props.disabled} onPress={()=>setPair(item)}><View style={{alignSelf:'stretch'}}><Text>{__uiT('ui_d1cc2ace6b80', {p0:(index+1)})}</Text></View></TouchableRipple>)}
  <Text>{__uiT('ui_63ee3a1b965a')}</Text><TextInput accessibilityLabel={__uiT('ui_f9804d616f70')} editable={!busy&&!props.disabled} keyboardType="decimal-pad" value={String(strength)} onChangeText={value=>{const number=Number(value);if(value.trim()!==''&&Number.isFinite(number)&&number>=0&&number<=1)setStrength(number);}}/>
  <Button accessibilityRole="button" disabled={busy||props.disabled||!pair} onPress={confirm} mode="outlined">{__uiT('ui_11a4bf707c09')}</Button>
 </View>;
}
