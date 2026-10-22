import {ActivityIndicator,Button,Text,TextInput} from 'react-native-paper';
import {privateShareURL,sharePrivateURL} from './private-share-link';
import {SharePresetPicker} from './share-preset-picker';
import {readCreatedCollection,readCollectionCreatedEnvelope} from './collection-create-response';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {APIError,type ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {CollectionCreateAttemptStore,type CollectionCreateAttempt} from './collection-create-attempt-store';
import {freezeLinkPolicy,linkUUID,readCreatedAccessLink,readCreationAbandoned,type LinkPolicy} from './collection-access-links-api';
import {LinkPolicyForm} from './collection-access-links';
import {readDeadline} from './read-deadline';
import {useReadResource} from './use-read-resource';
import {useLocale} from './locale-context';
import type {Collection} from './catalog-types';
type Props={client:ApiClient;origin:string;tenant:string;project:string;canEdit:boolean;current:()=>boolean;onCreated:(collection:Collection)=>void};
const defaultPolicy:LinkPolicy={label:'',permissions:{view:true,download:false,upload:false},limits:{max_upload_files:100,max_upload_bytes_total:1073741824,max_file_bytes:104857600,allowed_formats:['jpg','jpeg','png']},moderation:'owner_review',allowed_presets:[]};
export function CollectionCreateWizard(props:Props){
 const instance=useMemo(()=>crypto.randomUUID(),[props.client,props.origin,props.tenant,props.project]);
 return <CollectionCreateWizardScope key={instance} {...props}/>;
}
function CollectionCreateWizardScope(props:Props){
 const __catalogT=useLocale().t;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const {locale}=useLocale(),tr=(en:string,ru:string,bg:string)=>locale==='ru'?ru:locale==='bg'?bg:en;
 const owner=useMemo(()=>new OwnerAPI(props.client,props.tenant),[props.client,props.tenant]),scope=useMemo(()=>({owner,origin:props.origin,project:props.project}),[owner,props.origin,props.project]);
 const read=useReadResource(`create:${props.origin}:${props.tenant}:${props.project}`,scope,async signal=>{const user=await owner.me(signal);signal.throwIfAborted();const store=new CollectionCreateAttemptStore(AsyncStorage,props.origin,user.id,props.tenant,props.project),attempt=await store.read();signal.throwIfAborted();return {store,attempt};});
 const [name,setName]=useState(''),[share,setShare]=useState(false),[policy,setPolicy]=useState(defaultPolicy),[attempt,setAttempt]=useState<CollectionCreateAttempt|null>(null),[busy,setBusy]=useState(false),[message,setMessage]=useState(''),[secret,setSecret]=useState<{id:string;secret:string}|null>(null),[accepted,setAccepted]=useState<Collection|null>(null);
 const draftState=useRef({name,share,policy,attempt,busy});draftState.current={name,share,policy,attempt,busy};
 const known=useRef<{attempt:CollectionCreateAttempt;secret:{id:string;secret:string}|null}|null>(null);
 const alive=useRef(true),flight=useRef<AbortController|null>(null),latest=useRef({props,scope,read});latest.current={props,scope,read};
 const current=()=>alive.current&&latest.current.scope===scope&&read.current()&&latest.current.read.current()&&latest.current.props.current();
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;flight.current?.abort();flight.current=null;};},[scope]);
 useEffect(()=>{if(read.phase==='ready'){setAttempt(known.current?.attempt??read.data!.attempt);if(known.current?.attempt??read.data!.attempt)setName((known.current?.attempt??read.data!.attempt)!.request.name);}},[read.phase,read.data]);
 useEffect(()=>{if(!props.canEdit){if(known.current)known.current.secret=null;flight.current?.abort();flight.current=null;setBusy(false);setSecret(null);}},[props.canEdit]);
 const stop=(own:AbortController|null)=>{if(!own||flight.current!==own)return;flight.current=null;own.abort();setBusy(false);setSecret(null);setMessage("Stopped waiting. The saved request is retained.");read.retry();};
 async function run(action:(signal:AbortSignal,check:()=>void)=>Promise<void>){if(!current()||read.phase!=='ready'||flight.current)return;const controller=new AbortController();flight.current=controller;setBusy(true);setMessage('');const own=()=>flight.current===controller&&!controller.signal.aborted&&current();try{await readDeadline(controller.signal,async signal=>{const check=()=>{signal.throwIfAborted();if(!own())throw Error('CREATE_SCOPE_WITHDRAWN');};await action(signal,check);check();},15000);}catch(error){if(own()){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){if(known.current)known.current.secret=null;setSecret(null);read.retry();}setMessage("Unable to confirm creation. Retry uses the same saved request.");}}finally{if(flight.current===controller){flight.current=null;if(alive.current)setBusy(false);}}}
 async function create(signal:AbortSignal,check:()=>void){
  const chosen=draftState.current;
  const data=read.data!;let saved=await data.store.read();check();if(known.current){if(saved&&(saved.key!==known.current.attempt.key||JSON.stringify(saved.request)!==JSON.stringify(known.current.attempt.request)))throw Error('CREATE_JOURNAL_CONFLICT');saved=known.current.attempt;}if(!saved){if(chosen.share&&chosen.policy.permissions.download&&!chosen.policy.allowed_presets.length){setMessage("Choose at least one download format.");return;}if(!latest.current.props.canEdit)throw Error('FORBIDDEN');const request={project_id:props.project,name:chosen.name.trim(),description:'' as const,...(chosen.share?{initial_share:freezeLinkPolicy(chosen.policy)}:{})};saved={schema_version:1,project_id:props.project,key:crypto.randomUUID(),request,ticket:null};await data.store.write(saved);check();setAttempt(saved);}
  let oneTime:{id:string;secret:string}|null=known.current?.secret??null;
  if(!saved.ticket){if(!latest.current.props.canEdit)throw Error('FORBIDDEN');const raw=await props.client.request<unknown>(`/api/v1/tenants/${props.tenant}/collections`,{method:'POST',idempotencyKey:saved.key,body:saved.request,signal});check();const result=readCollectionCreatedEnvelope(raw);const c=readCreatedCollection(result.collection,props.tenant,props.project);if(c.name!==saved.request.name||c.description!==saved.request.description||c.state!==(saved.request.initial_share?'published':'draft')||c.revision!==(saved.request.initial_share?'2':'1'))throw Error('CREATE_RECEIPT_INVALID');if(saved.request.initial_share){const link=readCreatedAccessLink(result.initial_share,c.id,saved.request.initial_share);if(link.secret)oneTime={id:link.row.id,secret:link.secret};}else if(result.initial_share!==undefined)throw Error('CREATE_RECEIPT_INVALID');saved={...saved,ticket:{collection_id:c.id}};known.current={attempt:saved,secret:oneTime};setAttempt(saved);setSecret(oneTime);}
  if(known.current){await data.store.write(saved);check();}
  const rawRow=await props.client.request<unknown>(`/api/v1/tenants/${props.tenant}/collections/${saved.ticket!.collection_id}`,{signal});check();const row=readCreatedCollection(rawRow,props.tenant,props.project);if(row.id!==saved.ticket!.collection_id)throw Error('CREATE_RECEIPT_INVALID');
  // Publish the authoritative current projection before any later explicit
  // journal reset. A storage timeout cannot erase the sole discoverable result.
  props.onCreated(row);check();setAccepted(row);setSecret(oneTime);read.commit({...data,attempt:saved});setAttempt(saved);setMessage(oneTime?"The collection and link were created together. Copy the private link now.":"Collection confirmed. A lost link secret is never displayed again.");
 }
 async function abandonCreation(signal:AbortSignal,check:()=>void){const data=read.data!,saved=known.current?.attempt??await data.store.read();check();if(!latest.current.props.canEdit||!saved||saved.ticket)throw Error('CREATE_ABANDON_INVALID');const raw=await props.client.request<unknown>(`/api/v1/tenants/${props.tenant}/collections/abandon`,{method:'POST',idempotencyKey:saved.key,body:saved.request,signal});check();if(!readCreationAbandoned(raw,saved.key,'project_id',props.project)){const result=readCollectionCreatedEnvelope(raw),row=readCreatedCollection(result.collection,props.tenant,props.project);if(row.name!==saved.request.name||row.description!==saved.request.description||row.revision!==(saved.request.initial_share?'2':'1')||row.state!==(saved.request.initial_share?'published':'draft'))throw Error('CREATE_RECEIPT_INVALID');if(saved.request.initial_share){const link=readCreatedAccessLink(result.initial_share,row.id,saved.request.initial_share);if(link.secret)throw Error('ABANDON_SECRET_INVALID');}else if(result.initial_share!==undefined)throw Error('CREATE_RECEIPT_INVALID');const acceptedAttempt={...saved,ticket:{collection_id:row.id}};known.current={attempt:acceptedAttempt,secret:null};setAttempt(acceptedAttempt);await data.store.write(acceptedAttempt);check();await create(signal,check);return;}await data.store.clearAbandoned(saved.key);check();known.current=null;setAttempt(null);setAccepted(null);setSecret(null);read.commit({...data,attempt:null});}
 const editable=()=>current()&&!flight.current&&!known.current&&!draftState.current.attempt&&!latest.current.read.data?.attempt&&latest.current.props.canEdit&&draftState.current.policy===policy&&draftState.current.name===name&&draftState.current.share===share;
 const saved=attempt??read.data?.attempt,locked=busy||!!saved||!props.canEdit,displayedFlight=flight.current;
 if(read.phase!=='ready')return <View>{read.phase==='loading'?<><ActivityIndicator/><Button accessibilityRole="button" onPress={read.cancel}>{__catalogT('ui_e7852dcdece5')}</Button></>:<Button accessibilityRole="button" onPress={read.retry}>{__catalogT('ui_a446148e1037')}</Button>}</View>;
 return <View><Text>{__catalogT('ui_151679b80ad6')}</Text>{message?<Text accessibilityRole="alert">{__uiMessage(message)}</Text>:null}<TextInput accessibilityLabel={__uiT('ui_922214922b48')} value={name} editable={!locked} onChangeText={value=>{if(editable())setName(value);}}/><Button accessibilityRole="button" disabled={locked} onPress={()=>{if(editable())setShare(v=>!v);}}>{share?'✓ ':''}{__catalogT('ui_46d65569afe6')}</Button>{share&&<LinkPolicyForm value={policy} onChange={value=>{if(editable())setPolicy(value);}} disabled={locked} locale={locale}/>}{share&&policy.permissions.download&&<SharePresetPicker client={props.client} tenant={props.tenant} value={policy.allowed_presets} onChange={allowed_presets=>{if(editable())setPolicy(old=>({...old,allowed_presets}));}} disabled={locked}/>}
 <Button accessibilityRole="button" disabled={busy||(!saved&&!props.canEdit)||(!saved&&!name.trim())||!!saved&&!saved.ticket&&!props.canEdit} onPress={()=>void run(create)}>{saved?__catalogT('ui_fe11a27bbb16'):__catalogT('ui_4759498ac2a7')}</Button>{busy&&<Button accessibilityRole="button" onPress={()=>stop(displayedFlight)}>{__catalogT('ui_d4af24be4049')}</Button>}
 {secret&&current()&&props.canEdit&&<Button accessibilityRole="button" disabled={busy} onPress={()=>void run(async(_signal,check)=>{const url=privateShareURL(process.env.EXPO_PUBLIC_PORTAL_ORIGIN,locale,secret.id,secret.secret);await sharePrivateURL(url,check);})}>{__catalogT('ui_ef97541c7992')}</Button>}
 {saved&&!saved.ticket&&<Button accessibilityRole="button" disabled={busy||!props.canEdit} onPress={()=>void run(abandonCreation)}>{__catalogT('ui_25a7bc8125af')}</Button>}
 {saved?.ticket&&accepted&&<Button accessibilityRole="button" disabled={busy} onPress={()=>void run(async(_signal,check)=>{await read.data!.store.clearAccepted(saved.key,saved.ticket!.collection_id);check();known.current=null;setAttempt(null);setAccepted(null);setSecret(null);setName('');read.commit({...read.data!,attempt:null});})}>{__catalogT('ui_c10a68af1ed7')}</Button>}
 </View>;
}
