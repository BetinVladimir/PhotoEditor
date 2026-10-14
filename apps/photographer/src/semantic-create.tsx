import {useEffect,useMemo,useRef,useState} from 'react';
import {Pressable,Text,TextInput,View} from 'react-native';
import {APIError,decodeSemanticEditIntent,type SemanticEditIntent,type SemanticJobTicket} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {ArtifactRef,Collection,V2VersionBinding} from './catalog-types';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
import type {SemanticAttemptStore} from './semantic-attempt-store';

type Props={api:Pick<OwnerAPI,'v2Binding'|'selectionSnapshot'|'proposeSemanticIntent'>;tenant:string;asset:string;version:string;mask:ArtifactRef;collection:Collection;disabled?:boolean;onSubmitted:(ticket:SemanticJobTicket)=>void;store?:SemanticAttemptStore;initialAttempt?:Attempt};
export type Attempt={snapshotKey:string;proposalKey:string;collection:Collection;mask:ArtifactRef;kind:SemanticEditIntent['kind'];strength:number;ev:number|null;prompt:string|null;binding?:V2VersionBinding;snapshot?:string;body?:SemanticEditIntent;ticket?:SemanticJobTicket};
const kinds=['remove_object','local_exposure','background_cleanup','retouch_region'] as const;
const labels={remove_object:'semantic_create_remove',local_exposure:'semantic_create_exposure',background_cleanup:'semantic_create_background',retouch_region:'semantic_create_retouch'} as const;

// Component scope changes abort active IO. Production persistence restores the
// original request under its source/principal scope instead of replacing keys.
export function SemanticCreate(props:Props){
 const scope=useMemo(()=>crypto.randomUUID(),[props.api,props.tenant,props.asset,props.version,props.mask.artifact_id,props.mask.sha256,props.collection.id,props.collection.revision]);
 return <SemanticCreateScope key={scope} {...props}/>;
}
function SemanticCreateScope({api,asset,version,mask,collection,disabled,onSubmitted,store,initialAttempt}:Props){
 const {t}=useLocale();
 const[kind,setKind]=useState<SemanticEditIntent['kind']>(initialAttempt?.kind??'local_exposure'),[strength,setStrength]=useState(String(initialAttempt?.strength??0.5)),[ev,setEV]=useState(String(initialAttempt?.ev??0)),[prompt,setPrompt]=useState(initialAttempt?.prompt??'');
 const[busy,setBusy]=useState(false),[retry,setRetry]=useState(false),[rejected,setRejected]=useState(false),[error,setError]=useState<'semantic_create_invalid'|'semantic_create_rejected'|'semantic_create_unknown'|''>('');
 const attempt=useRef<Attempt|null>(initialAttempt?structuredClone(initialAttempt):null),flight=useRef<AbortController|null>(null);
 useEffect(()=>()=>{flight.current?.abort();flight.current=null},[]);
 async function submit(){
  if(flight.current||disabled&&!attempt.current?.ticket||rejected)return;
  if(!attempt.current){
   const weight=Number(strength),exposure=Number(ev);
   if(!strength.trim()||!Number.isFinite(weight)||weight<0||weight>1||kind==='local_exposure'&&(!ev.trim()||!Number.isFinite(exposure)||exposure< -1||exposure>1)||Array.from(prompt).length>1024){setError('semantic_create_invalid');return}
   attempt.current={snapshotKey:crypto.randomUUID(),proposalKey:crypto.randomUUID(),collection:structuredClone(collection),mask:structuredClone(mask),kind,strength:weight,ev:kind==='local_exposure'?exposure:null,prompt:prompt.trim()?prompt:null};
  }
  const current=attempt.current,controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  try{
   const persist=async()=>{if(store)await store.write({schema_version:1,...current,collection:{id:current.collection.id,revision:current.collection.revision},binding:current.binding??null,snapshot:current.snapshot??null,body:current.body??null,ticket:current.ticket??null})};
   await persist();if(controller.signal.aborted)return;
   if(current.ticket){onSubmitted(current.ticket);return}
   if(!current.binding){
    const binding=await api.v2Binding(asset,controller.signal);
    if(controller.signal.aborted)return;
    if(binding.asset_version_id!==version)throw new APIError(409,'SOURCE_CHANGED');
    current.binding=structuredClone(binding);
    await persist();if(controller.signal.aborted)return;
   }
   if(!current.snapshot){
    const snapshot=await api.selectionSnapshot(current.collection,current.snapshotKey,controller.signal);
    if(controller.signal.aborted)return;
    if(!Number.isSafeInteger(snapshot.item_count)||snapshot.item_count<1||snapshot.item_count>5000||!/^[0-9a-f]{64}$/.test(snapshot.digest)||!Number.isFinite(Date.parse(snapshot.expires_at))||Date.parse(snapshot.expires_at)<=Date.now())throw new APIError(422,'SNAPSHOT_INVALID');
    current.snapshot=snapshot.id;
    await persist();if(controller.signal.aborted)return;
   }
   if(!current.body)current.body=decodeSemanticEditIntent({schema_version:1,kind:current.kind,reference:current.binding,reference_mask:current.mask,target_snapshot_id:current.snapshot,strength:current.strength,ev:current.ev,preserve_persons:true,prompt:current.prompt});
   await persist();if(controller.signal.aborted)return;
   const ticket=await api.proposeSemanticIntent(current.body,current.proposalKey,controller.signal);
   if(controller.signal.aborted)return;
   current.ticket=structuredClone(ticket);await persist();if(controller.signal.aborted)return;
   setRetry(false);onSubmitted(ticket);setRejected(true);
  }catch(cause){
   if(controller.signal.aborted)return;
   if(cause instanceof APIError&&[400,401,403,404,409,410,422].includes(cause.status)||cause instanceof Error&&cause.message==='SEMANTIC_PROPOSE_REQUEST_INVALID'){
    if(store)await store.clear(current.proposalKey).catch(()=>{});
    attempt.current=null;setRetry(false);setRejected(true);setError('semantic_create_rejected');
   }else{setRetry(true);setError('semantic_create_unknown')}
  }finally{if(flight.current===controller){flight.current=null;if(!controller.signal.aborted)setBusy(false)}}
 }
 const locked=busy||retry||rejected||disabled||!!initialAttempt;
 return <View><Text accessibilityRole="header" style={s.subtitle}>{t('semantic_create_title')}</Text><Text>{t('semantic_create_targets').replace('{name}',collection.name)}</Text><Text>{t('semantic_create_protection')}</Text>
  <View style={{flexDirection:'row',flexWrap:'wrap',gap:8}}>{kinds.map(value=><Pressable key={value} accessibilityRole="button" accessibilityState={{selected:kind===value}} disabled={locked} onPress={()=>setKind(value)} style={s.button}><Text style={s.buttonText}>{t(labels[value])}</Text></Pressable>)}</View>
  <TextInput accessibilityLabel={t('semantic_create_strength')} editable={!locked} value={strength} onChangeText={setStrength} keyboardType="decimal-pad" style={s.input}/>
  {kind==='local_exposure'?<TextInput accessibilityLabel={t('semantic_create_ev')} editable={!locked} value={ev} onChangeText={setEV} keyboardType="numbers-and-punctuation" style={s.input}/>:null}
  <TextInput accessibilityLabel={t('semantic_create_prompt')} editable={!locked} value={prompt} onChangeText={setPrompt} maxLength={2048} style={s.input}/>
  {error?<Text accessibilityRole="alert" style={s.error}>{t(error)}</Text>:null}
  {initialAttempt?<Text>{t('semantic_create_restored')}</Text>:null}
  <Pressable accessibilityRole="button" disabled={busy||disabled&&!attempt.current?.ticket||rejected} onPress={()=>void submit()} style={s.button}><Text style={s.buttonText}>{t(retry?'semantic_create_retry':'semantic_create_submit')}</Text></Pressable>
 </View>;
}
