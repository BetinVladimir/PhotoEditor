import {ActivityIndicator,Button,Text,TextInput} from 'react-native-paper';
import {privateShareURL,sharePrivateURL} from './private-share-link';
import {SharePresetPicker} from './share-preset-picker';
import {useEffect,useMemo,useRef,useState} from 'react';
import {View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {APIError,type ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {CollectionAccessLinksAPI,freezeLinkPolicy,type AccessLink,type LinkPolicy,type LinkUsage} from './collection-access-links-api';
import {AccessLinkAttemptStore,type AccessLinkAttempt} from './access-link-attempt-store';
import {useReadResource} from './use-read-resource';
import {readDeadline} from './read-deadline';
import {useLocale} from './locale-context';
export type CollectionAccessLinksProps={client:ApiClient;origin:string;tenant:string;collectionId:string;canEdit:boolean;canRecoverEdit:boolean;current:()=>boolean;acquire:()=>symbol|null;release:(token:symbol)=>void;onChanged:()=>void};
const initial:LinkPolicy={label:'',permissions:{view:true,download:false,upload:false},limits:{max_upload_files:100,max_upload_bytes_total:1073741824,max_file_bytes:104857600,allowed_formats:['jpg','jpeg','png']},moderation:'owner_review',allowed_presets:[]};
export function CollectionAccessLinks(props:CollectionAccessLinksProps){
 const instance=useMemo(()=>crypto.randomUUID(),[props.client,props.origin,props.tenant,props.collectionId]);
 return <CollectionAccessLinksScope key={instance} {...props}/>;
}
function CollectionAccessLinksScope(props:CollectionAccessLinksProps){
 const __catalogT=useLocale().t;

 const __uiMessage=useLocale().message;

 const {locale}=useLocale();const tr=(en:string,ru:string,bg:string)=>locale==='ru'?ru:locale==='bg'?bg:en;
 const api=useMemo(()=>new CollectionAccessLinksAPI(props.client,props.tenant,props.collectionId),[props.client,props.tenant,props.collectionId]);
 const owner=useMemo(()=>new OwnerAPI(props.client,props.tenant),[props.client,props.tenant]);
 const read=useReadResource(`${props.origin}:${props.tenant}:${props.collectionId}:links`,api,async signal=>{const me=await owner.me(signal);signal.throwIfAborted();const store=new AccessLinkAttemptStore(AsyncStorage,props.origin,me.id,props.tenant,props.collectionId);const attempt=await store.read();signal.throwIfAborted();const page=await api.page(signal);return {store,attempt,items:page.items,next:page.next,seen:[] as string[]};});
 const [draft,setDraft]=useState<LinkPolicy>(initial),[editing,setEditing]=useState<AccessLink|null>(null),[pending,setPending]=useState<AccessLinkAttempt|null>(null),[busy,setBusy]=useState(false),[message,setMessage]=useState(''),[secret,setSecret]=useState<{id:string;value:string}|null>(null),[usage,setUsage]=useState<Record<string,LinkUsage>>({});
 const lease=useRef<{token:symbol;release:(token:symbol)=>void}|null>(null);
 const known=useRef<{attempt:AccessLinkAttempt;row:AccessLink;secret?:string}|null>(null);
 const form=useRef({draft,editing,locked:true});
 const flight=useRef<{controller:AbortController;token:symbol;mode:'read'|'fresh'|'recover'|'receipt'|'secret';released:boolean}|null>(null),alive=useRef(true),scope=useMemo(()=>({api,origin:props.origin}),[api,props.origin]),latest=useRef({props,scope,read});latest.current={props,scope,read};
 const current=()=>alive.current&&latest.current.scope===scope&&read.current()&&latest.current.read.current()&&latest.current.props.current();
 const knownScope=useRef(scope);if(knownScope.current!==scope){knownScope.current=scope;}
 useEffect(()=>{alive.current=true;setPending(null);setSecret(null);setEditing(null);setUsage({});return()=>{alive.current=false;const f=flight.current;flight.current=null;f?.controller.abort();const held=lease.current;lease.current=null;if(f&&f.token!==held?.token)props.release(f.token);if(held)held.release(held.token);};},[scope]);
 useEffect(()=>{if(read.phase==='ready')setPending(known.current?.attempt??read.data!.attempt);},[read.phase,read.data]);
 useEffect(()=>{const f=flight.current;if(f?.mode==='fresh'&&!props.canEdit){flight.current=null;f.controller.abort();if(lease.current?.token!==f.token)props.release(f.token);setBusy(false);}},[props.canEdit]);
 useEffect(()=>{if(!props.canRecoverEdit){if(known.current)known.current.secret=undefined;setSecret(null);const f=flight.current;flight.current=null;f?.controller.abort();const held=lease.current;lease.current=null;if(f&&f.token!==held?.token)props.release(f.token);if(held)held.release(held.token);setBusy(false);}},[props.canRecoverEdit]);
 function stop(f:typeof flight.current){if(!f||flight.current!==f||!current())return;flight.current=null;f.controller.abort();if(lease.current?.token!==f.token)props.release(f.token);setBusy(false);if(!lease.current)setSecret(null);setMessage("Stopped waiting. Recover the same saved request.");if(!lease.current)read.retry();}
 async function run(action:(signal:AbortSignal,check:()=>void)=>Promise<void>,mode:'read'|'fresh'|'recover'|'receipt'|'secret'='read'){
  if(!current()||read.phase!=='ready'||flight.current||mode==='fresh'&&!props.canEdit||(mode==='recover'||mode==='receipt'||mode==='secret')&&!props.canRecoverEdit||lease.current&&mode!=='secret'&&mode!=='receipt')return;
  const token=lease.current?.token??props.acquire();if(!token)return;const f={controller:new AbortController(),token,mode,released:false};flight.current=f;setBusy(true);setMessage('');
  const owns=()=>flight.current===f&&!f.controller.signal.aborted&&current();
  const check=()=>{f.controller.signal.throwIfAborted();if(!owns()||mode==='fresh'&&!latest.current.props.canEdit||(mode==='recover'||mode==='receipt'||mode==='secret')&&!latest.current.props.canRecoverEdit)throw Error('ACCESS_WITHDRAWN');};
  try{await readDeadline(f.controller.signal,async signal=>{const checked=()=>{signal.throwIfAborted();check();};checked();await action(signal,checked);checked();},15000);}
  catch(error){if(owns()){if(error instanceof APIError&&[401,403,404,410].includes(error.status)){if(known.current)known.current.secret=undefined;setSecret(null);const held=lease.current;lease.current=null;if(held){held.release(held.token);if(held.token===f.token)f.released=true;}read.retry();}setMessage(error instanceof APIError&&error.code==='SHARE_REPLAY_INTENT_REQUIRED'?"This old request needs its original explicit expiry. Keep the saved request; do not create a duplicate.":"The result is uncertain. Recover a saved creation or reload the current policy before editing.");}}
  finally{if(flight.current===f){flight.current=null;if(!f.released&&lease.current?.token!==token)props.release(token);if(alive.current)setBusy(false);}}
 }
 async function create(signal:AbortSignal,check:()=>void){
  // Capture the current displayed policy at action admission, before storage
  // can yield. A retained Create callback must not resurrect its older draft.
  const chosen=structuredClone(form.current.draft);
  const data=read.data!;let attempt=await data.store.read();check();if(known.current){if(attempt&&(attempt.key!==known.current.attempt.key||JSON.stringify(attempt.request)!==JSON.stringify(known.current.attempt.request)))throw Error('SHARE_JOURNAL_CONFLICT');attempt=known.current.attempt;}
  if(!attempt){if(chosen.permissions.download&&!chosen.allowed_presets.length){setMessage("Choose at least one download format.");return;}attempt={schema_version:1,key:crypto.randomUUID(),request:freezeLinkPolicy(chosen),ticket:null};await data.store.write(attempt);check();setPending(attempt);}
  if(attempt.ticket){if(known.current){await data.store.write(attempt);check();}let cursor:string|undefined;const seen=new Set<string>();for(;;){const page=await api.page(signal,cursor);check();const row=page.items.find(v=>v.id===attempt!.ticket!.resource_id);if(row){read.commit({...data,attempt,items:[row,...data.items.filter(v=>v.id!==row.id)]});setPending(attempt);if(known.current?.secret)setSecret({id:row.id,value:known.current.secret});setMessage("Recovered. The original secret cannot be displayed again.");return;}if(!page.next||seen.has(page.next))throw Error('SHARE_NOT_FOUND');seen.add(page.next);cursor=page.next;} }
  const result=await api.create(attempt.request,attempt.key,signal);check();const accepted={...attempt,ticket:{resource_id:result.row.id}};
  // Persist only the receipt identity, never the capability secret. Projection
  // is published before any later reset can remove the only accepted ticket.
  known.current={attempt:accepted,row:result.row,secret:result.secret};if(result.secret&&flight.current)lease.current={token:flight.current.token,release:props.release};setPending(accepted);read.commit({...data,attempt:accepted,items:[result.row,...data.items.filter(v=>v.id!==result.row.id)]});setSecret(result.secret?{id:result.row.id,value:result.secret}:null);
  await data.store.write(accepted);check();if(!result.secret)props.onChanged();setMessage(result.secret?"Copy this link now. Its secret is shown once.":"Recovered without a secret. Create a new link if the original URL was lost.");
 }
 async function abandonCreation(signal:AbortSignal,check:()=>void){const data=read.data!,saved=known.current?.attempt??await data.store.read();check();if(!saved||saved.ticket)throw Error('SHARE_ABANDON_INVALID');const result=await api.abandon(saved.request,saved.key,signal);check();if(!result.abandoned){const accepted={...saved,ticket:{resource_id:result.row.id}};known.current={attempt:accepted,row:result.row};setPending(accepted);read.commit({...data,attempt:accepted,items:[result.row,...data.items.filter(v=>v.id!==result.row.id)]});await data.store.write(accepted);check();return;}await data.store.clearAbandoned(saved.key);check();known.current=null;setPending(null);setSecret(null);setEditing(null);setDraft(initial);read.commit({...data,attempt:null});}
 async function nextCreation(signal:AbortSignal,check:()=>void){const data=read.data!,saved=await data.store.read();check();if(!saved?.ticket)throw Error('SHARE_NOT_ACCEPTED');await data.store.clearAccepted(saved.key,saved.ticket.resource_id);check();known.current=null;setPending(null);setSecret(null);setDraft(initial);setEditing(null);read.commit({...data,attempt:null});props.onChanged();}
 async function more(signal:AbortSignal,check:()=>void){const data=read.data!;if(!data.next)return;const cursor=data.next;if(data.seen.includes(cursor))throw Error('CURSOR_INVALID');const page=await api.page(signal,cursor);check();if(page.items.some(v=>data.items.some(old=>old.id===v.id)))throw Error('PAGE_CHANGED');read.commit({...data,items:[...data.items,...page.items],next:page.next,seen:[...data.seen,cursor]});}
 const recover=!!pending||!!read.data?.attempt,locked=busy||recover||!!lease.current||!props.canEdit;
 form.current={draft,editing,locked};
 const editable=()=>current()&&!flight.current&&!known.current&&!lease.current&&!form.current.locked&&latest.current.props.canEdit&&form.current.draft===draft&&form.current.editing===editing;
 function changePolicy(next:LinkPolicy){if(editable())setDraft(next);}
 function savePolicy(row:AccessLink){
  if(!current()||form.current.locked||flight.current||!latest.current.props.canEdit||form.current.editing?.id!==row.id)return;
  const data=latest.current.read.data,currentRow=data?.items.find(v=>v.id===row.id&&v.policy_version===row.policy_version);if(!data||!currentRow)return;
  const chosen=structuredClone(form.current.draft);
  void run(async(signal,check)=>{const request=freezeLinkPolicy(chosen);const result=await api.update(currentRow,request,signal);check();read.commit({...data,items:data.items.map(v=>v.id===row.id?result:v)});setEditing(null);},'fresh');
 }
 if(read.phase!=='ready')return <View><Text>{__catalogT('ui_67f253696586')}</Text>{read.phase==='loading'?<><ActivityIndicator/><Button accessibilityRole="button" onPress={read.cancel}>{__catalogT('ui_e7852dcdece5')}</Button></>:<Button accessibilityRole="button" onPress={read.retry}>{__catalogT('ui_72ea30970acd')}</Button>}</View>;
 const data=read.data!,attempt=pending??data.attempt,displayedFlight=flight.current,displayedSecret=secret,displayedLease=lease.current;
 return <View><Text>{__catalogT('ui_67f253696586')}</Text><Text>{__uiMessage(message)}</Text>{busy&&<Button accessibilityRole="button" onPress={()=>stop(displayedFlight)}>{__catalogT('ui_d4af24be4049')}</Button>}
 {!data.items.length&&<Text>{__catalogT('ui_8618086e46a7')}</Text>}
 <LinkPolicyForm defaultExpiry={!editing} value={draft} onChange={changePolicy} disabled={locked} locale={locale}/>{draft.permissions.download&&<SharePresetPicker client={props.client} tenant={props.tenant} value={draft.allowed_presets} onChange={allowed_presets=>{if(editable())setDraft(old=>({...old,allowed_presets}));}} disabled={locked}/>}
 <Button accessibilityRole="button" disabled={busy||(!attempt?!props.canEdit:!props.canRecoverEdit)} onPress={()=>void run(create,attempt?(known.current?.attempt.ticket?'receipt':'recover'):'fresh')}>{attempt?__catalogT('ui_e531a4fa7391'):editing?__catalogT('ui_0cb805aea019'):__catalogT('ui_e6b850cff6af')}</Button>
 {attempt&&!attempt.ticket&&<Button accessibilityRole="button" disabled={busy||!!lease.current||!props.canRecoverEdit} onPress={()=>void run(abandonCreation,'recover')}>{__catalogT('ui_25a7bc8125af')}</Button>}
 {attempt?.ticket&&<Button accessibilityRole="button" disabled={busy||!!lease.current||!props.canRecoverEdit} onPress={()=>void run(nextCreation,'recover')}>{__catalogT('ui_1367eb6b21c4')}</Button>}
 {secret&&current()&&props.canRecoverEdit&&<View><Text>{__catalogT('ui_b102592fadcf')}</Text><Button accessibilityRole="button" disabled={busy||!props.canRecoverEdit} onPress={()=>{const saved=secret;if(!current()||!displayedLease||lease.current!==displayedLease||known.current?.secret!==saved.value||known.current.row.id!==saved.id)return;void run(async(_signal,check)=>{const bound=()=>{check();if(lease.current!==displayedLease||known.current?.secret!==saved.value||known.current.row.id!==saved.id)throw Error('PRIVATE_LINK_WITHDRAWN');};bound();const url=privateShareURL(process.env.EXPO_PUBLIC_PORTAL_ORIGIN,locale,saved.id,saved.value);await sharePrivateURL(url,bound);},'secret');}}>{__catalogT('ui_ef97541c7992')}</Button><Button accessibilityRole="button" disabled={busy} onPress={()=>{if(!current()||busy||!latest.current.props.canRecoverEdit||!displayedSecret||!displayedLease||lease.current!==displayedLease||known.current?.row.id!==displayedSecret.id||known.current.secret!==displayedSecret.value)return;known.current.secret=undefined;setSecret(null);lease.current=null;displayedLease.release(displayedLease.token);props.onChanged();}}>{__catalogT('ui_3e674e084ecd')}</Button></View>}
 <Button accessibilityRole="button" disabled={busy||!!lease.current} onPress={()=>{if(!current()||flight.current||lease.current)return;setSecret(null);read.retry();}}>{__catalogT('ui_52a2a32efc6e')}</Button>
 {data.items.map(row=><View key={row.id}><Text>{row.label||row.id}</Text><Text>{row.revoked_at?__catalogT('ui_f6f738d04392'):row.expires_at&&Date.parse(row.expires_at)<=Date.now()?__catalogT('ui_424a2551d356'):__catalogT('ui_92340695899b')} · {row.expires_at??__catalogT('ui_fe06351db8da')} · {row.moderation==='owner_review'?__catalogT('ui_c6aef36ffce3'):__catalogT('ui_e818db923c43')}</Text><Text>{[row.permissions.view?__catalogT('ui_dcc839a4015c'):'',row.permissions.download?__catalogT('ui_d6eafe823591'):'',row.permissions.upload?__catalogT('ui_865e89de78d9'):''].filter(Boolean).join(' · ')}</Text>
 <Button accessibilityRole="button" disabled={locked||!!row.revoked_at} onPress={()=>{if(!current()||flight.current||form.current.locked||lease.current||!latest.current.props.canEdit||!latest.current.read.data?.items.some(v=>v.id===row.id&&v.policy_version===row.policy_version))return;setEditing(row);setDraft({label:row.label,expires_at:row.expires_at??null,permissions:{...row.permissions},limits:{...row.limits,allowed_formats:[...row.limits.allowed_formats]},moderation:row.moderation,allowed_presets:[...row.allowed_presets]});}}>{__catalogT('ui_18f41a84465f')}</Button>
 {editing?.id===row.id&&<Button accessibilityRole="button" disabled={locked} onPress={()=>savePolicy(row)}>{__catalogT('ui_57ee14ce1425')}</Button>}
 <Button accessibilityRole="button" disabled={busy||!!lease.current||!props.canRecoverEdit||!!row.revoked_at} onPress={()=>void run(async(signal,check)=>{const out=await api.revoke(row,signal);check();read.commit({...data,items:data.items.map(v=>v.id===row.id?out:v)});if(secret?.id===row.id)setSecret(null);},'recover')}>{__catalogT('ui_da371ee1e141')}</Button>
 <Button accessibilityRole="button" disabled={busy||!!lease.current} onPress={()=>void run(async(signal,check)=>{const out=await api.usage(row.id,signal);check();setUsage(old=>({...old,[row.id]:out}));})}>{__catalogT('ui_8d59829c1e15')}</Button>{usage[row.id]&&<Text>{__catalogT('ui_5a773dcc2295')}: {[usage[row.id].uploaded_files,usage[row.id].uploaded_bytes,usage[row.id].quota_used_files,usage[row.id].quota_used_bytes,usage[row.id].reserved_files,usage[row.id].reserved_bytes,usage[row.id].download_authorizations,usage[row.id].download_jobs].join(' / ')}</Text>}
 </View>)}{data.next&&<Button accessibilityRole="button" disabled={busy||!!lease.current} onPress={()=>void run(more)}>{__catalogT('ui_18c67781bdf7')}</Button>}
 </View>;
}
export function LinkPolicyForm({value,onChange,disabled,locale,defaultExpiry=true}:{value:LinkPolicy;onChange:(v:LinkPolicy)=>void;disabled:boolean;locale:string;defaultExpiry?:boolean}){
 const __catalogT=useLocale().t;

 const __uiT=useLocale().t;

 const tr=(en:string,ru:string,bg:string)=>locale==='ru'?ru:locale==='bg'?bg:en;
 const change=(patch:Partial<LinkPolicy>)=>onChange({...value,...patch});
 return <View><TextInput accessibilityLabel={__catalogT('ui_acaa9ea6688f')} value={value.label} editable={!disabled} onChangeText={label=>change({label})}/>
 {(['view','download','upload'] as const).map(permission=><Button accessibilityRole="button" key={permission} disabled={disabled} onPress={()=>change({permissions:{...value.permissions,[permission]:!value.permissions[permission]}})}>{value.permissions[permission]?'✓ ':''}{permission==='view'?__catalogT('ui_324ca03d1fe9'):permission==='download'?__catalogT('ui_559bfb716367'):__catalogT('ui_794cab7e1321')}</Button>)}
 <Text>{defaultExpiry?__catalogT('ui_23c620d1df5b'):__catalogT('ui_a4c43a8d51b9')}</Text><TextInput accessibilityLabel={__uiT('ui_5d4cf7e765f4')} value={value.expires_at??''} editable={!disabled} onChangeText={expires_at=>{const next={...value};if(expires_at)next.expires_at=expires_at;else delete next.expires_at;onChange(next);}}/><Button accessibilityRole="button" disabled={disabled} onPress={()=>change({expires_at:null})}>{__catalogT('ui_34729d6775c2')}</Button><Text>{value.expires_at===null?__catalogT('ui_9fb386e8970d'):''}</Text>
 {(['max_upload_files','max_upload_bytes_total','max_file_bytes'] as const).map(key=><View key={key}><Text>{key==='max_upload_files'?__catalogT('ui_a1876259a94a'):key==='max_upload_bytes_total'?__catalogT('ui_059ad9da3d62'):__catalogT('ui_59d5aa7eaed1')}</Text><TextInput accessibilityLabel={key} value={String(value.limits[key])} editable={!disabled} keyboardType="numeric" onChangeText={raw=>change({limits:{...value.limits,[key]:/^(0|[1-9][0-9]*)$/.test(raw)&&Number.isSafeInteger(Number(raw))?Number(raw):NaN}})}/></View>)}
 <TextInput accessibilityLabel={__uiT('ui_e5e51ccefe89')} value={value.limits.allowed_formats.join(',')} editable={!disabled} onChangeText={raw=>change({limits:{...value.limits,allowed_formats:raw.split(',').map(v=>v.trim().toLowerCase()).filter(Boolean)}})}/>
 {(['owner_review','auto_after_validation'] as const).map(moderation=><Button accessibilityRole="button" key={moderation} disabled={disabled} onPress={()=>change({moderation})}>{value.moderation===moderation?'✓ ':''}{moderation==='owner_review'?__catalogT('ui_c6aef36ffce3'):__catalogT('ui_e818db923c43')}</Button>)}
 </View>;
}
