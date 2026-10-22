import {useLocale} from './locale-context';
import {TouchableRipple,Button,ActivityIndicator,Text,TextInput} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {ShootSession} from './capture-types';
import type {V2VersionBinding} from './catalog-types';
import {collectGlareSourceCandidates,collectGlareMaskPairs,type GlareSourceCandidate,type GlareMaskPair} from './glare-donor-candidates';
import {GlareSourceAttemptStore} from './glare-source-attempt';
import {GlareSourceControls} from './glare-source-controls';
import {freezeGlareSourcePreparation,verifyGlareSourceRecovery,type GlareSourcePreparation,type GlareSourceTicket} from './glare-source-intent';
type Props={api:OwnerAPI;origin:string;tenant:string;project:string;asset:string;version:string;collection?:string;disabled?:boolean;operationAvailable?:()=>boolean;onSubmitted:(ticket:GlareSourceTicket)=>void};
type Context={user:string;base:V2VersionBinding;snapshot:string;sessions:ShootSession[];recovery:boolean};
export function EditorGlareSource(props:Props){const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.project,props.asset,props.version,props.collection]);return <Picker key={scope} {...props}/>;}
function Picker(props:Props){
 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const [context,setContext]=useState<Context|null>(null),[session,setSession]=useState<ShootSession|null>(null),[sources,setSources]=useState<GlareSourceCandidate[]>([]),[masks,setMasks]=useState<GlareMaskPair[]>([]),[source,setSource]=useState<GlareSourceCandidate|null>(null),[mask,setMask]=useState<GlareMaskPair|null>(null),[selection,setSelection]=useState<GlareSourcePreparation|null>(null),[strength,setStrength]=useState(.5),[busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const active=useRef(true),flight=useRef<AbortController|null>(null),generation=useRef(0);
 useEffect(()=>{active.current=true;const controller=new AbortController();flight.current=controller;
  void(async()=>{try{
   const user=await props.api.me(controller.signal);if(!active.current||controller.signal.aborted)return;
   const base=await props.api.v2Binding(props.asset,controller.signal);if(!active.current||controller.signal.aborted)return;if(base.asset_version_id!==props.version)throw new Error('SOURCE_CHANGED');
   const saved=await new GlareSourceAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.project,props.version).read();if(!active.current||controller.signal.aborted)return;
   if(saved){verifyGlareSourceRecovery(saved.body,base);setContext({user:user.id,base,snapshot:saved.body.snapshot_id,sessions:[],recovery:true});return;}
   const available=await props.api.glareSourceAvailable(props.project,controller.signal);if(!active.current||controller.signal.aborted)return;if(!available)throw new Error('GENERATIVE_UNAVAILABLE');
   const snapshot=await props.api.createRenderInputSnapshot(props.project,{sources:[base]},crypto.randomUUID(),controller.signal);if(!active.current||controller.signal.aborted)return;const sessions:ShootSession[]=[];let cursor:string|null=null;const seen=new Set<string>();
   for(let i=0;i<256;i++){if(!active.current||controller.signal.aborted)return;const page=await props.api.shootSessions(props.project,cursor??undefined,controller.signal);if(!active.current||controller.signal.aborted)return;sessions.push(...page.items.filter(s=>s.state==='open'||s.state==='closed'));if(!page.next_cursor)break;if(seen.has(page.next_cursor)||i===255)throw new Error('CURSOR');seen.add(page.next_cursor);cursor=page.next_cursor;}
   if(active.current&&!controller.signal.aborted)setContext({user:user.id,base,snapshot:snapshot.id,sessions:sessions.filter(s=>!props.collection||s.collection_id===props.collection),recovery:false});
  }catch{if(active.current&&!controller.signal.aborted)setMessage('Generative glare choices are unavailable.');}finally{if(active.current&&!controller.signal.aborted)setBusy(false);}})();
  return()=>{active.current=false;controller.abort();flight.current?.abort();};
 },[props.api,props.origin,props.tenant,props.project,props.asset,props.version,props.collection]);
 async function chooseSession(next:ShootSession){if(!context||busy||props.disabled)return;const token=++generation.current;flight.current?.abort();const controller=new AbortController();flight.current=controller;setSession(next);setSources([]);setMasks([]);setSource(null);setMask(null);setBusy(true);setMessage('');try{
  const [rows,pairs]=await Promise.all([collectGlareSourceCandidates((q,s)=>props.api.glareSourceCandidates(props.project,q,s),{shoot_session_id:next.id,snapshot_id:context.snapshot,base:context.base,after:null},controller.signal),collectGlareMaskPairs((q,s)=>props.api.glareMaskPairs(props.project,q,s),{snapshot_id:context.snapshot,base:context.base,after:null},controller.signal)]);
  if(active.current&&!controller.signal.aborted&&generation.current===token){setSources(rows);setMasks(pairs);}
 }catch{if(active.current&&!controller.signal.aborted&&generation.current===token)setMessage('Source observations or masks are unavailable.');}finally{if(active.current&&!controller.signal.aborted&&generation.current===token)setBusy(false);}}
 function confirm(){if(!context||!session||!source||!mask||busy||props.disabled)return;try{setSelection(freezeGlareSourcePreparation({schema_version:1,shoot_session_id:session.id,snapshot_id:context.snapshot,ordinal:0,base:context.base,strength,glasses_mask:mask.glasses_mask,protected:mask.protected,target_pixel:mask.target_pixel,protected_pixel:mask.protected_pixel,source_faces:source.source_faces,source_observation_id:source.observation_id}));}catch{setMessage('Selection is invalid.');}}
 function revokeAccess(){generation.current++;flight.current?.abort();setContext(null);setSelection(null);setSession(null);setSources([]);setMasks([]);setSource(null);setMask(null);setBusy(false);setMessage('Access is no longer available. Reopen the editor after access is restored.');}
 if(context?.recovery||selection&&context)return <GlareSourceControls api={props.api} origin={props.origin} user={context.user} tenant={props.tenant} project={props.project} baseVersion={props.version} selection={selection??undefined} disabled={props.disabled} operationAvailable={props.operationAvailable} onSubmitted={props.onSubmitted} onAccessDenied={revokeAccess}/>;
 const option=(label:string,checked:boolean,action:()=>void,key:string)=><TouchableRipple aria-checked={checked} key={key} accessibilityRole="radio" accessibilityState={{checked}} disabled={busy||props.disabled} onPress={action}><View style={{alignSelf:'stretch'}}><Text>{label}</Text></View></TouchableRipple>;
 return <View><Text accessibilityRole="header">{__uiT('ui_b241a65d1938')}</Text><Text>{__uiT('ui_0ae20d19b684')}</Text>{message?<Text accessibilityRole="alert">{__uiMessage(message)}</Text>:null}{busy?<ActivityIndicator accessibilityLabel={__uiT('ui_28acf1d687d3')}/>:null}{context?.sessions.map((s,i)=>option(`Shoot ${i+1}: ${s.name}`,session?.id===s.id,()=>void chooseSession(s),s.id))}{sources.map((s,i)=>option(`Source face ${i+1}`,source?.observation_id===s.observation_id,()=>setSource(s),s.observation_id))}{masks.map((m,i)=>option(`Mask pair ${i+1}`,mask?.glasses_mask.artifact_id===m.glasses_mask.artifact_id,()=>setMask(m),m.glasses_mask.artifact_id))}{session&&!busy&&!sources.length?<Text>{__uiT('ui_56e7a956c305')}</Text>:null}{session&&!busy&&!masks.length?<Text>{__uiT('ui_1b9c085c1171')}</Text>:null}<Text>{__uiT('ui_63ee3a1b965a')}</Text><TextInput accessibilityLabel={__uiT('ui_ba3fbaeeda61')} editable={!busy&&!props.disabled} keyboardType="decimal-pad" value={String(strength)} onChangeText={s=>{const n=Number(s);if(s.trim()&&Number.isFinite(n)&&n>=0&&n<=1)setStrength(n);}}/><Button accessibilityRole="button" disabled={busy||props.disabled||!session||!source||!mask} onPress={confirm} mode="outlined">{__uiT('ui_fc614d1bd587')}</Button></View>;
}
