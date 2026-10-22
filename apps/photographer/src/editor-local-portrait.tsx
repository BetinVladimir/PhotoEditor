import {useLocale} from './locale-context';
import {TouchableRipple,Button,ActivityIndicator,Text,TextInput} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {V2VersionBinding} from './catalog-types';
import {collectReflectionMaskPairs,type ReflectionMaskPair} from './reflection-mask-pairs';
import {LocalPortraitAttemptStore} from './local-portrait-attempt-store';
import {LocalPortraitControls} from './local-portrait-controls';
import {freezeLocalPortraitSelection,verifyLocalPortraitRecoverySource,type LocalPortraitSelection,type LocalPortraitTicket} from './local-portrait-intent';

type Props={api:OwnerAPI;origin:string;tenant:string;project:string;asset:string;version:string;hairAvailable:boolean;clothingAvailable:boolean;disabled?:boolean;operationAvailable?:(operation:'portrait.hair'|'portrait.clothing')=>boolean;onSubmitted:(ticket:LocalPortraitTicket)=>void};
type Context={user:string;base:V2VersionBinding;snapshot:string;recovery:boolean;operation?:'portrait.hair'|'portrait.clothing'};
export function EditorLocalPortrait(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.project,props.asset,props.version]);
 return <Picker key={scope} {...props}/>;
}
function Picker(props:Props){
 const __catalogT=useLocale().t;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const [context,setContext]=useState<Context|null>(null),[pairs,setPairs]=useState<ReflectionMaskPair[]>([]),[pair,setPair]=useState<ReflectionMaskPair|null>(null);
 const [operation,setOperation]=useState<'portrait.hair'|'portrait.clothing'>(props.hairAvailable?'portrait.hair':'portrait.clothing');
 const [strength,setStrength]=useState(.5),[hue,setHue]=useState(0),[saturation,setSaturation]=useState(1),[detail,setDetail]=useState(.3),[wrinkle,setWrinkle]=useState(.3);
 const [selection,setSelection]=useState<LocalPortraitSelection|null>(null),[busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const active=useRef(true),flight=useRef<AbortController|null>(null);
 useEffect(()=>{
  active.current=true;const controller=new AbortController();flight.current=controller;
  void(async()=>{try{
   const user=await props.api.me(controller.signal);
   const base=await props.api.v2Binding(props.asset,controller.signal);
   if(base.asset_version_id!==props.version)throw new Error('LOCAL_PORTRAIT_SOURCE_CHANGED');
   const journal=new LocalPortraitAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.project,props.version);
   const saved=await journal.read();if(!active.current||controller.signal.aborted)return;
   if(saved){verifyLocalPortraitRecoverySource(saved.body,base);
    setContext({user:user.id,base,snapshot:saved.body.snapshot_id,recovery:true,operation:saved.body.operation});return;}
   if(!props.hairAvailable&&!props.clothingAvailable)throw new Error('LOCAL_PORTRAIT_CAPABILITY_UNAVAILABLE');
   const snapshot=await props.api.createRenderInputSnapshot(props.project,{sources:[base]},crypto.randomUUID(),controller.signal);
   const rows=await collectReflectionMaskPairs((query,signal)=>props.api.localPortraitMaskPairs(props.project,query,signal),{snapshot_id:snapshot.id,base,after:null},controller.signal);
   if(active.current&&!controller.signal.aborted){setContext({user:user.id,base,snapshot:snapshot.id,recovery:false});setPairs(rows);}
  }catch(error){if(active.current&&!controller.signal.aborted)setMessage(error instanceof Error&&error.message==='LOCAL_PORTRAIT_CAPABILITY_UNAVAILABLE'?'Local portrait is unavailable on this deployment.':'Local portrait mask choices are unavailable.');}
  finally{if(active.current&&!controller.signal.aborted)setBusy(false);}})();
  return()=>{active.current=false;controller.abort();flight.current?.abort();};
 },[props.api,props.origin,props.tenant,props.project,props.asset,props.version,props.hairAvailable,props.clothingAvailable]);
 const selectedOperation=selection?.operation??context?.operation??operation;
 const operationDisabled=props.disabled||(selectedOperation==='portrait.hair'?!props.hairAvailable:!props.clothingAvailable);
 function confirm(){
  if(!context||!pair||busy||operationDisabled)return;
  try{
   const shared={schema_version:1 as const,snapshot_id:context.snapshot,ordinal:0,target_pixel:pair.target_pixel,protected_pixel:pair.protected_pixel};
   const params={schema_version:1 as const,base:context.base,strength,mask:pair.mask,protected:pair.protected,hue_delta_degrees:hue,saturation_factor:saturation};
   const selected:LocalPortraitSelection=operation==='portrait.hair'
    ?{...shared,operation,params:{...params,detail}}
    :{...shared,operation,params:{...params,wrinkle_strength:wrinkle}};
   setSelection(freezeLocalPortraitSelection(selected));setMessage('');
  }catch{setMessage('Local portrait selection is invalid.');}
 }
 if(context?.recovery)return <LocalPortraitControls api={props.api} origin={props.origin} user={context.user} tenant={props.tenant} project={props.project} baseVersion={props.version} disabled={props.disabled} operationAvailable={()=>props.operationAvailable?props.operationAvailable(selectedOperation):(selectedOperation==='portrait.hair'?props.hairAvailable:props.clothingAvailable)} onSubmitted={props.onSubmitted}/>;
 if(context&&selection)return <LocalPortraitControls api={props.api} origin={props.origin} user={context.user} tenant={props.tenant} project={props.project} baseVersion={props.version} selection={selection} disabled={props.disabled} operationAvailable={()=>props.operationAvailable?props.operationAvailable(selectedOperation):(selectedOperation==='portrait.hair'?props.hairAvailable:props.clothingAvailable)} onSubmitted={props.onSubmitted}/>;
 const numeric=(label:string,value:number,set:(value:number)=>void,lo:number,hi:number)=><View><Text>{label}</Text><TextInput accessibilityLabel={label} editable={!busy&&!props.disabled} keyboardType="decimal-pad" value={String(value)} onChangeText={raw=>{const next=Number(raw);if(raw.trim()!==''&&Number.isFinite(next)&&next>=lo&&next<=hi)set(next);}}/></View>;
 return <View><Text accessibilityRole="header">{__uiT('ui_cd37e90ce93f')}</Text><Text>{__uiT('ui_873753f5cff6')}</Text>
  {message?<Text accessibilityRole="alert">{__uiMessage(message)}</Text>:null}{busy?<ActivityIndicator accessibilityLabel={__uiT('ui_7e8bd56f04ab')}/>:null}
  <TouchableRipple aria-checked={operation==='portrait.hair'} accessibilityRole="radio" accessibilityState={{checked:operation==='portrait.hair'}} disabled={busy||props.disabled||!props.hairAvailable} onPress={()=>{setOperation('portrait.hair');setHue(0);setSaturation(1);}}><View style={{alignSelf:'stretch'}}><Text>{__uiT('ui_ababed4823ab')}</Text></View></TouchableRipple>
  <TouchableRipple aria-checked={operation==='portrait.clothing'} accessibilityRole="radio" accessibilityState={{checked:operation==='portrait.clothing'}} disabled={busy||props.disabled||!props.clothingAvailable} onPress={()=>{setOperation('portrait.clothing');setHue(0);setSaturation(1);}}><View style={{alignSelf:'stretch'}}><Text>{__uiT('ui_5bea3ab36d88')}</Text></View></TouchableRipple>
  {context&&pairs.length===0&&!busy?<Text>{__uiT('ui_1b9c085c1171')}</Text>:null}
  {pairs.map((item,index)=><TouchableRipple aria-checked={pair?.mask.artifact_id===item.mask.artifact_id} key={item.mask.artifact_id} accessibilityRole="radio" accessibilityState={{checked:pair?.mask.artifact_id===item.mask.artifact_id}} disabled={busy||props.disabled} onPress={()=>setPair(item)}><View style={{alignSelf:'stretch'}}><Text>{__uiT('ui_d1cc2ace6b80', {p0:(index+1)})}</Text></View></TouchableRipple>)}
  {numeric(__catalogT('ui_63ee3a1b965a'),strength,setStrength,0,1)}
  {numeric(__catalogT('ui_d0652332f3b7'),hue,setHue,operation==='portrait.hair'?-30:-180,operation==='portrait.hair'?30:180)}
  {numeric(__catalogT('ui_993f024b650e'),saturation,setSaturation,operation==='portrait.hair'?0.7:0,operation==='portrait.hair'?1.3:2)}
  {operation==='portrait.hair'?numeric(__catalogT('ui_fb5f27d5457c'),detail,setDetail,0,1):numeric(__catalogT('ui_bf2019817daa'),wrinkle,setWrinkle,0,1)}
  <Button accessibilityRole="button" disabled={busy||props.disabled||!pair||operation==='portrait.hair'&&!props.hairAvailable||operation==='portrait.clothing'&&!props.clothingAvailable} onPress={confirm} mode="outlined">{__uiT('ui_0023168f20ea')}</Button>
 </View>;
}
