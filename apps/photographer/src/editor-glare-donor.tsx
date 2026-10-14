import {useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,Pressable,Text,TextInput,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import type {OwnerAPI} from './owner-api';
import type {ShootSession} from './capture-types';
import type {V2VersionBinding} from './catalog-types';
import {collectGlareSourceCandidates,collectGlareDonorCandidates,collectGlareMaskPairs,verifyGlareDonorCandidateBinding,type GlareSourceCandidate,type GlareDonorCandidate,type GlareMaskPair} from './glare-donor-candidates';
import {GlareDonorAttemptStore} from './glare-donor-attempt-store';
import {GlareDonorControls} from './glare-donor-controls';
import {freezeGlareDonorPreparation,type GlareDonorPreparation,type GlareDonorTicket} from './glare-donor-intent';

type Props={api:OwnerAPI;origin:string;tenant:string;project:string;asset:string;version:string;collection?:string;disabled?:boolean;onSubmitted:(ticket:GlareDonorTicket)=>void};
type Context={user:string;base:V2VersionBinding;snapshot:string;sessions:ShootSession[];recovery:boolean};

export function EditorGlareDonor(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.project,props.asset,props.version,props.collection]);
 return <Picker key={scope} {...props}/>;
}

function Picker(props:Props){
 const [context,setContext]=useState<Context|null>(null),[session,setSession]=useState<ShootSession|null>(null);
 const [sources,setSources]=useState<GlareSourceCandidate[]>([]),[masks,setMasks]=useState<GlareMaskPair[]>([]),[donors,setDonors]=useState<GlareDonorCandidate[]>([]);
 const [source,setSource]=useState<GlareSourceCandidate|null>(null),[mask,setMask]=useState<GlareMaskPair|null>(null),[donor,setDonor]=useState<GlareDonorCandidate|null>(null),[donorBinding,setDonorBinding]=useState<V2VersionBinding|null>(null);
 const [strength,setStrength]=useState(.5),[busy,setBusy]=useState(true),[message,setMessage]=useState('');
 const [selection,setSelection]=useState<GlareDonorPreparation|null>(null);
 const active=useRef(true),flight=useRef<AbortController|null>(null),generation=useRef(0);
 useEffect(()=>{
  active.current=true;const controller=new AbortController();flight.current=controller;
  void(async()=>{try{
   const user=await props.api.me(controller.signal);
   const base=await props.api.v2Binding(props.asset,controller.signal);
   if(base.asset_version_id!==props.version)throw new Error('GLARE_SOURCE_CHANGED');
   const journal=new GlareDonorAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.project,props.version);
   const saved=await journal.read();if(!active.current||controller.signal.aborted)return;
   if(saved){setContext({user:user.id,base,snapshot:saved.body.snapshot_id,sessions:[],recovery:true});return;}
   const snapshot=await props.api.createRenderInputSnapshot(props.project,{sources:[base]},crypto.randomUUID(),controller.signal);
   const sessions:ShootSession[]=[];let cursor:string|null=null;
   for(let i=0;i<256;i++){
    const page=await props.api.shootSessions(props.project,cursor??undefined,controller.signal);
    sessions.push(...page.items.filter(item=>item.state==='open'||item.state==='closed'));
    if(!page.next_cursor)break;
    if(cursor===page.next_cursor)throw new Error('GLARE_SESSION_CURSOR');
    cursor=page.next_cursor;
    if(i===255)throw new Error('GLARE_SESSION_PAGE_LIMIT');
   }
   if(!active.current||controller.signal.aborted)return;
   setContext({user:user.id,base,snapshot:snapshot.id,sessions:sessions.filter(item=>!props.collection||item.collection_id===props.collection),recovery:false});
  }catch{if(active.current&&!controller.signal.aborted)setMessage('Donor glare choices are unavailable.');}
  finally{if(active.current&&!controller.signal.aborted)setBusy(false);}})();
  return()=>{active.current=false;controller.abort();flight.current?.abort();};
 },[props.api,props.origin,props.tenant,props.project,props.asset,props.version,props.collection]);

 async function chooseSession(next:ShootSession){
  if(!context||busy||props.disabled)return;
  const token=++generation.current;flight.current?.abort();const controller=new AbortController();flight.current=controller;
  setSession(next);setSources([]);setMasks([]);setSource(null);setDonors([]);setDonor(null);setDonorBinding(null);setMask(null);setBusy(true);setMessage('');
  try{
   const [sourceRows,maskRows]=await Promise.all([
    collectGlareSourceCandidates((query,signal)=>props.api.glareSourceCandidates(props.project,query,signal),{shoot_session_id:next.id,snapshot_id:context.snapshot,base:context.base,after:null},controller.signal),
    collectGlareMaskPairs((query,signal)=>props.api.glareMaskPairs(props.project,query,signal),{snapshot_id:context.snapshot,base:context.base,after:null},controller.signal),
   ]);
   if(active.current&&!controller.signal.aborted&&generation.current===token){setSources(sourceRows);setMasks(maskRows);}
  }catch{if(active.current&&!controller.signal.aborted&&generation.current===token)setMessage('Source observations or masks are unavailable.');}
  finally{if(active.current&&!controller.signal.aborted&&generation.current===token)setBusy(false);}
 }

 async function chooseSource(next:GlareSourceCandidate){
  if(!context||!session||busy||props.disabled)return;
  const token=++generation.current;flight.current?.abort();const controller=new AbortController();flight.current=controller;
  setSource(next);setDonors([]);setDonor(null);setDonorBinding(null);setBusy(true);setMessage('');
  try{
   const result=await collectGlareDonorCandidates((query,signal)=>props.api.glareDonorCandidates(props.project,query,signal),{shoot_session_id:session.id,snapshot_id:context.snapshot,base:context.base,source_observation_id:next.observation_id,after:null},controller.signal);
   if(result.source_faces.artifact_id!==next.source_faces.artifact_id||result.source_faces.sha256!==next.source_faces.sha256)throw new Error('GLARE_SOURCE_EVIDENCE_CHANGED');
   if(active.current&&!controller.signal.aborted&&generation.current===token)setDonors(result.items);
  }catch{if(active.current&&!controller.signal.aborted&&generation.current===token)setMessage('Donor observations are unavailable.');}
  finally{if(active.current&&!controller.signal.aborted&&generation.current===token)setBusy(false);}
 }

 async function chooseDonor(next:GlareDonorCandidate){
  if(!context||busy||props.disabled)return;
  const token=++generation.current;flight.current?.abort();const controller=new AbortController();flight.current=controller;
  setDonor(null);setDonorBinding(null);setBusy(true);setMessage('');
  try{
   const binding=await props.api.v2Binding(next.asset_id,controller.signal);
   const verified=verifyGlareDonorCandidateBinding(next,binding);
   if(active.current&&!controller.signal.aborted&&generation.current===token){setDonor(next);setDonorBinding(verified);}
  }catch{if(active.current&&!controller.signal.aborted&&generation.current===token)setMessage('Donor frame changed. Reload choices.');}
  finally{if(active.current&&!controller.signal.aborted&&generation.current===token)setBusy(false);}
 }

 function confirm(){
  if(!context||!session||!source||!mask||!donor||!donorBinding||busy||props.disabled)return;
  try{setSelection(freezeGlareDonorPreparation({schema_version:1,shoot_session_id:session.id,snapshot_id:context.snapshot,ordinal:0,
   base:context.base,donor:donorBinding,strength,glasses_mask:mask.glasses_mask,protected:mask.protected,
   target_pixel:mask.target_pixel,protected_pixel:mask.protected_pixel,source_faces:source.source_faces,
   donor_faces:donor.donor_faces,source_observation_id:source.observation_id,donor_observation_id:donor.observation_id}));setMessage('');}
  catch{setMessage('Selection is invalid.');}
 }

 if(context?.recovery)return <GlareDonorControls api={props.api} origin={props.origin} user={context.user} tenant={props.tenant} project={props.project} baseVersion={props.version} disabled={props.disabled} onSubmitted={props.onSubmitted}/>;
 if(selection&&context)return <GlareDonorControls api={props.api} origin={props.origin} user={context.user} tenant={props.tenant} project={props.project} baseVersion={props.version} selection={selection} disabled={props.disabled} onSubmitted={props.onSubmitted}/>;
 const option=(label:string,checked:boolean,action:()=>void,key:string)=><Pressable key={key} accessibilityRole="radio" accessibilityState={{checked}} disabled={busy||props.disabled} onPress={action}><Text>{label}</Text></Pressable>;
 return <View><Text accessibilityRole="header">Donor glare</Text><Text>Select a confirmed face and a mask pair from this shoot.</Text>
  {message?<Text accessibilityRole="alert">{message}</Text>:null}{busy?<ActivityIndicator accessibilityLabel="Loading donor glare choices"/>:null}
  {context?.sessions.map((item,index)=>option(`Shoot ${index+1}: ${item.name}`,session?.id===item.id,()=>void chooseSession(item),item.id))}
  {session&&sources.length===0&&!busy?<Text>No confirmed source observations.</Text>:null}
  {sources.map((item,index)=>option(`Source face ${index+1}`,source?.observation_id===item.observation_id,()=>void chooseSource(item),item.observation_id))}
  {source&&donors.length===0&&!busy?<Text>No eligible donor frames.</Text>:null}
  {donors.map((item,index)=>option(`Donor frame ${index+1}`,donor?.observation_id===item.observation_id,()=>void chooseDonor(item),item.observation_id))}
  {session&&masks.length===0&&!busy?<Text>Create a target and protected mask first.</Text>:null}
  {masks.map((item,index)=>option(`Mask pair ${index+1}`,mask?.glasses_mask.artifact_id===item.glasses_mask.artifact_id,()=>setMask(item),item.glasses_mask.artifact_id))}
  <Text>Strength</Text><TextInput accessibilityLabel="Donor glare strength" editable={!busy&&!props.disabled} keyboardType="decimal-pad" value={String(strength)} onChangeText={value=>{const number=Number(value);if(value.trim()!==''&&Number.isFinite(number)&&number>=0&&number<=1)setStrength(number);}}/>
  <Pressable accessibilityRole="button" disabled={busy||props.disabled||!session||!source||!donor||!donorBinding||!mask} onPress={confirm}><Text>Review donor glare request</Text></Pressable>
 </View>;
}
