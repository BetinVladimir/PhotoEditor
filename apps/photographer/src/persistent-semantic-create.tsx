import {Button,Text} from 'react-native-paper';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {APIError,type SemanticEditIntent,type SemanticJobTicket} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {ArtifactRef,Collection} from './catalog-types';
import {SemanticAttemptStore} from './semantic-attempt-store';
import {SemanticCreate,type Attempt} from './semantic-create';
import {useLocale} from './locale-context';
import {useReviewForeground} from './use-review-foreground';
import {useReadResource} from './use-read-resource';
import {ReadResourceState} from './read-resource-state';
import {readDeadline} from './read-deadline';
type Props={api:OwnerAPI;origin:string;tenant:string;asset:string;version:string;mask?:ArtifactRef;collection:Collection;disabled?:boolean;kindAvailable?:(kind:SemanticEditIntent['kind'])=>boolean;onSubmitted:(ticket:SemanticJobTicket)=>void};
export function PersistentSemanticCreate(props:Props){
 const foreground=useReviewForeground();
 const key=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.asset,props.version,props.collection.id,props.collection.revision,props.mask?.artifact_id,props.mask?.sha256,foreground.generation]);
 return foreground.active?<Scope key={key} {...props}/>:null;
}
function Scope(props:Props){
 const {t}=useLocale();
 const[accepted,setAccepted]=useState<SemanticJobTicket|null>(null),[epoch,setEpoch]=useState(0),[clearing,setClearing]=useState(false),[clearFailed,setClearFailed]=useState(false),[fresh,setFresh]=useState(false);
 const clearFlight=useRef<AbortController|null>(null),clearKnown=useRef<{store:SemanticAttemptStore;key:string;receipt:SemanticJobTicket}|null>(null),liveDisabled=useRef(props.disabled);liveDisabled.current=props.disabled;
 useEffect(()=>()=>{clearFlight.current?.abort();clearFlight.current=null},[]);
 const resource=useReadResource('semantic-create-journal',props.api,async signal=>{
  const user=await props.api.me(signal);signal.throwIfAborted();
  const store=new SemanticAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.asset,props.version,props.collection.id);
  const value=await store.read();signal.throwIfAborted();
  if(value?.ticket){
   const identity=await props.api.semanticRunIntent(value.ticket.run_id,signal);signal.throwIfAborted();
   if(identity.run_id!==value.ticket.run_id||identity.workflow_id!==value.ticket.workflow_id)throw new APIError(409,'SEMANTIC_RECOVERY_SCOPE_CHANGED');
  }else{
   // Recheck current authority without rewriting the saved source/body.
   const source=await props.api.v2Binding(props.asset,signal);signal.throwIfAborted();
   if(!value?.binding&&source.asset_version_id!==props.version)throw new APIError(409,'SOURCE_CHANGED');
  }
  const attempt:Attempt|undefined=value?{snapshotKey:value.snapshotKey,proposalKey:value.proposalKey,collection:{...props.collection,...value.collection},mask:value.mask,kind:value.kind,strength:value.strength,ev:value.ev,prompt:value.prompt,...(value.binding?{binding:value.binding}:{}),...(value.snapshot?{snapshot:value.snapshot}:{}),...(value.body?{body:value.body}:{}),...(value.ticket?{ticket:value.ticket}:{})}:undefined;
  return{store,attempt};
 });
 if(resource.phase!=='ready')return <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel}/>;
 const state=resource.data;if(!state)return null;
 const initialAttempt=fresh?undefined:state.attempt,expectedReceipt=accepted??initialAttempt?.ticket,mask=initialAttempt?.mask??props.mask;if(!mask)return null;
 async function another(){
  if(!state||!expectedReceipt||clearFlight.current||liveDisabled.current||!resource.current())return;
  const controller=new AbortController();clearFlight.current=controller;setClearing(true);setClearFailed(false);
  try{await readDeadline(controller.signal,async signal=>{const current=await state.store.read();signal.throwIfAborted();if(liveDisabled.current)throw new Error('SEMANTIC_EDIT_DISABLED');const known=clearKnown.current;if(current){if(!current.ticket||JSON.stringify(current.ticket)!==JSON.stringify(expectedReceipt)||known&&(known.store!==state.store||known.key!==current.proposalKey))throw new Error('SEMANTIC_RECEIPT_REQUIRED');clearKnown.current={store:state.store,key:current.proposalKey,receipt:structuredClone(current.ticket)};}else if(!known||known.store!==state.store||JSON.stringify(known.receipt)!==JSON.stringify(expectedReceipt))throw new Error('SEMANTIC_RECEIPT_REQUIRED');await state.store.clear(clearKnown.current!.key);signal.throwIfAborted();if(liveDisabled.current)throw new Error('SEMANTIC_EDIT_DISABLED');if(clearFlight.current!==controller||!resource.current())return;clearKnown.current=null;setFresh(true);setAccepted(null);setEpoch(x=>x+1);});}
  catch{if(clearFlight.current===controller&&resource.current())setClearFailed(true);}
  finally{if(clearFlight.current===controller){clearFlight.current=null;setClearing(false);}}
 }
 const displayedClear=clearFlight.current;
 function stopClear(){if(!displayedClear||clearFlight.current!==displayedClear||!resource.current())return;displayedClear.abort();clearFlight.current=null;setClearing(false);setClearFailed(true);}
 return <View><SemanticCreate key={epoch} {...props} mask={mask} disabled={props.disabled||clearing} store={state.store} initialAttempt={initialAttempt} onSubmitted={ticket=>{if(resource.current()){setAccepted(structuredClone(ticket));props.onSubmitted(ticket)}}}/>{accepted||initialAttempt?.ticket?<Button accessibilityRole="button" disabled={clearing} onPress={()=>void another()} mode="outlined">{t('semantic_create_another')}</Button>:null}{clearing?<Button accessibilityRole="button" onPress={stopClear} mode="outlined">{t('read_cancel')}</Button>:null}{clearFailed?<><Text accessibilityRole="alert">{t('semantic_create_unknown')}</Text><Button accessibilityRole="button" disabled={clearing} onPress={()=>void another()} mode="outlined">{t('read_retry')}</Button></>:null}</View>;
}
