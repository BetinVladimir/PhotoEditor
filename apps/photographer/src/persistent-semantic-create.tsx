import {useEffect,useMemo,useRef,useState} from 'react';
import {AppState,Pressable,Text,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {APIError,type SemanticJobTicket} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import type {ArtifactRef,Collection} from './catalog-types';
import {SemanticAttemptStore} from './semantic-attempt-store';
import {SemanticCreate,type Attempt} from './semantic-create';
import {useLocale} from './locale-context';
type Props={api:OwnerAPI;origin:string;tenant:string;asset:string;version:string;mask?:ArtifactRef;collection:Collection;disabled?:boolean;onSubmitted:(ticket:SemanticJobTicket)=>void};
export function PersistentSemanticCreate(props:Props){
 const key=useMemo(()=>crypto.randomUUID(),[props.api,props.origin,props.tenant,props.asset,props.version,props.collection.id,props.collection.revision,props.mask?.artifact_id,props.mask?.sha256]);
 return <Scope key={key} {...props}/>;
}
function Scope(props:Props){
 const lifecycle=useRef(0);
 const {t}=useLocale();const[state,setState]=useState<{store:SemanticAttemptStore;attempt?:Attempt}|null>(null),[failed,setFailed]=useState(false),[denied,setDenied]=useState(false),[accepted,setAccepted]=useState(false),[epoch,setEpoch]=useState(0),[clearing,setClearing]=useState(false);
 useEffect(()=>{let active=true,flight:AbortController|null=null;
  async function load(){lifecycle.current++;flight?.abort();const controller=new AbortController();flight=controller;const alive=()=>active&&!controller.signal.aborted&&flight===controller;setState(null);setFailed(false);setDenied(false);setAccepted(false);setClearing(false);try{
   const user=await props.api.me(controller.signal);if(!alive())return;
   const store=new SemanticAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.asset,props.version,props.collection.id);
   const value=await store.read();if(!alive())return;
   if(value?.ticket){
    const identity=await props.api.semanticRunIntent(value.ticket.run_id,controller.signal);if(!alive())return;
    if(identity.run_id!==value.ticket.run_id||identity.workflow_id!==value.ticket.workflow_id)throw new APIError(409,'SEMANTIC_RECOVERY_SCOPE_CHANGED');
   }else{
    // This read checks current editor authority only. A saved binding/body is
    // frozen and must never be replaced by the asset's newer current version.
    const source=await props.api.v2Binding(props.asset,controller.signal);if(!alive())return;
    if(!value?.binding&&source.asset_version_id!==props.version)throw new APIError(409,'SOURCE_CHANGED');
   }
   const attempt:Attempt|undefined=value?{snapshotKey:value.snapshotKey,proposalKey:value.proposalKey,collection:{...props.collection,...value.collection},mask:value.mask,kind:value.kind,strength:value.strength,ev:value.ev,prompt:value.prompt,...(value.binding?{binding:value.binding}:{}),...(value.snapshot?{snapshot:value.snapshot}:{}),...(value.body?{body:value.body}:{}),...(value.ticket?{ticket:value.ticket}:{})}:undefined;
   setState({store,attempt});
  }catch(error){if(alive()){setDenied(error instanceof APIError&&[401,403,404,409].includes(error.status));setFailed(true)}}finally{if(flight===controller)flight=null}}
  const listener=AppState.addEventListener('change',next=>{if(next==='active')void load();else{lifecycle.current++;flight?.abort();flight=null;setState(null);setAccepted(false)}});
  if((AppState.currentState??'active')==='active')void load();
  return()=>{lifecycle.current++;active=false;flight?.abort();listener.remove()};
 },[]);
 if(failed)return <Text accessibilityRole="alert">{t(denied?'semantic_create_rejected':'semantic_create_unknown')}</Text>;
 if(!state)return null;
 const mask=state.attempt?.mask??props.mask;if(!mask)return null;
 async function another(){if(!state||clearing)return;const checkpoint=lifecycle.current;setClearing(true);try{const current=await state.store.read();if(!current?.ticket)throw new Error('SEMANTIC_RECEIPT_REQUIRED');await state.store.clear(current.proposalKey);if(lifecycle.current!==checkpoint)return;setState({store:state.store});setAccepted(false);setEpoch(x=>x+1)}catch{if(lifecycle.current===checkpoint)setFailed(true)}finally{if(lifecycle.current===checkpoint)setClearing(false)}}
 return <View><SemanticCreate key={epoch} {...props} mask={mask} disabled={props.disabled||clearing} store={state.store} initialAttempt={state.attempt} onSubmitted={ticket=>{setAccepted(true);props.onSubmitted(ticket)}}/>{accepted||state.attempt?.ticket?<Pressable accessibilityRole="button" disabled={clearing} onPress={()=>void another()}><Text>{t('semantic_create_another')}</Text></Pressable>:null}</View>;
}
