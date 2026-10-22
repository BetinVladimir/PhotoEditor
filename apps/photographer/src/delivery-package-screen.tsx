import {ActivityIndicator,Button,Text,TouchableRipple} from 'react-native-paper';
import {useWorkspaceAccess} from './workspace-access-context';
import {readDeadline} from './read-deadline';
import {useReviewForeground} from './use-review-foreground';
import type {DeliveryPublishAttemptStore} from './delivery-publish-attempt-store';
import {useEffect,useMemo,useRef,useState} from 'react';
import {FlatList,Linking,SafeAreaView,View} from 'react-native';
import {APIError,type ApiClient} from '@photoeditor/api-client';
import {readDeliveryInspection,type DeliveryInspection} from './delivery-inspection';
import type {DeliveryItem} from './delivery-items';
import {OwnerAPI} from './owner-api';
import {prepareDeliveryPublication,readPublishedDelivery,deliveryPackageEnvelope,type DeliveryPackage,type DeliveryPublishIntent} from './delivery-package';
import {catalogStyle as s} from './catalog-style';
import {useLocale} from './locale-context';
type Props={journal?:DeliveryPublishAttemptStore;client:ApiClient;tenant:string;project:string;id:string;onRun?:(runID:string)=>void;onSign?:(exportID:string)=>void};
type Attempt={intent:DeliveryPublishIntent;key:string};
export function DeliveryPackageScreen(props:Props){
 const access=useWorkspaceAccess(props.tenant);
 const generation=useMemo(()=>crypto.randomUUID(),[props.client,props.journal,access?.actor,access?.canEdit]);
 return <Scope key={`${generation}:${props.tenant}:${props.project}:${props.id}`} {...props}/>;
}
function Scope({client,tenant,project,id,onSign,onRun,journal}:Props){
 const __uiMessage=useLocale().message;

 const canEdit=useWorkspaceAccess(tenant)?.canEdit===true;
 const foreground=useReviewForeground(),foregroundActive=useRef(foreground.active);
 foregroundActive.current=foreground.active;
 const {t}=useLocale(),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const [pkg,setPackage]=useState<DeliveryPackage|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState(''),[epoch,setEpoch]=useState(0),[retry,setRetry]=useState(false);
 const [items,setItems]=useState<DeliveryItem[]|null>(null);
 const [inspection,setInspection]=useState<DeliveryInspection|null>(null),[confirmed,setConfirmed]=useState(false);
 const active=useRef(false),flight=useRef<AbortController|null>(null),attempt=useRef<Attempt|null>(null);
 useEffect(()=>{
  active.current=true;
  return()=>{active.current=false;flight.current?.abort();flight.current=null};
 },[]);
 useEffect(()=>{
  if(!foreground.active){
   // Abort transport work, retaining the immutable publication attempt for replay.
   flight.current?.abort();flight.current=null;setBusy(false);
   setInspection(null);setConfirmed(false);return;
  }
  const controller=new AbortController();flight.current?.abort();flight.current=controller;setBusy(true);setPackage(null);setItems(null);setInspection(null);setConfirmed(false);setError('');
  void readDeadline(controller.signal,async signal=>{
   if(journal){journal.assertScope(tenant,project,id);const saved=await journal.read();if(!active.current||!foregroundActive.current||signal.aborted)return null;
    if(saved){attempt.current={intent:saved.body,key:saved.key};setRetry(true)}
   }
   const value=await api.deliveryPackage(project,id,signal);
   if(!value||!active.current||!foregroundActive.current||signal.aborted)return;
   const saved=attempt.current;
   // A published package can acknowledge only the exact saved CAS transition.
   // Keep an ambiguous attempt recoverable when a refresh returns another revision.
   if(value.published_at!==null&&saved){
    readPublishedDelivery(deliveryPackageEnvelope(value),tenant,saved.intent);
   }
   setPackage(value);
   if(value.published_at!==null){
    attempt.current=null;setRetry(false);
    if(journal&&saved){
     try{await journal.clear(saved.key,value)}catch{
      if(active.current&&foregroundActive.current&&!signal.aborted)setError(t('delivery_journal_cleanup_failed'));
     }
    }
   }
  }).catch(failure=>{
   if(!active.current||!foregroundActive.current||controller.signal.aborted)return;
   if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){
    attempt.current=null;setRetry(false);setPackage(null);setItems(null);setError(t('delivery_access_lost'));
   }else setError(t('delivery_load_failed'));
  }).finally(()=>{
   if(flight.current===controller){flight.current=null;if(active.current)setBusy(false)}
  });
  return()=>controller.abort();
 },[api,tenant,project,id,epoch,t,journal,foreground.active,foreground.generation]);
 // Observe preparation/assembly without retrying any publication mutation.
 useEffect(()=>{
  if(!foreground.active||busy||error||retry||pkg?.state!=='building'||pkg.published_at!==null)return;
  const timer=setTimeout(()=>setEpoch(value=>value+1),2000);
  return()=>clearTimeout(timer);
 },[pkg,busy,error,retry,foreground.active]);
 async function loadItems(){
  if(!foregroundActive.current||flight.current||!pkg)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setItems(null);setInspection(null);setConfirmed(false);setError('');
  try{
   const loaded=await readDeadline(controller.signal,signal=>api.deliveryItems(pkg,signal));
   if(active.current&&foregroundActive.current&&!controller.signal.aborted)setItems(loaded);
  }catch(failure){
   if(active.current&&foregroundActive.current&&!controller.signal.aborted){
    if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){
     setPackage(null);setItems(null);attempt.current=null;setRetry(false);setError(t('delivery_access_lost'));
    }else setError(t('delivery_items_failed'));
   }
  }
  finally{if(flight.current===controller){flight.current=null;if(active.current)setBusy(false)}}
 }
 async function inspect(){
  if(!canEdit||!foregroundActive.current||flight.current||!pkg||items===null)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setInspection(null);setConfirmed(false);setError('');
  try{
   const proof=await readDeadline(controller.signal,signal=>api.inspectDelivery(pkg,items,signal));
   if(active.current&&foregroundActive.current&&!controller.signal.aborted)setInspection(proof);
  }catch(failure){
   if(active.current&&foregroundActive.current&&!controller.signal.aborted){
    if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){
     setPackage(null);setItems(null);attempt.current=null;setRetry(false);setError(t('delivery_access_lost'));
    }else setError(t('delivery_inspection_failed'));
   }
  }finally{if(flight.current===controller){flight.current=null;if(active.current)setBusy(false)}}
 }
 async function publish(){
  if(!canEdit||!foregroundActive.current||flight.current||!pkg&&!attempt.current)return;
  let owned=attempt.current;
  try{
   if(!owned){
    if(!pkg)throw new Error('DELIVERY_NOT_PUBLISHABLE');
    if(pkg.state==='review'){
     if(!confirmed||!inspection||items===null)throw new Error('DELIVERY_REVIEW_CONFIRMATION_REQUIRED');
     readDeliveryInspection(inspection,pkg,items);
    }
    owned={intent:prepareDeliveryPublication(pkg,pkg.state==='review'&&confirmed),key:crypto.randomUUID()};attempt.current=owned;
   }
  }catch{setError(t('delivery_not_publishable'));return}
  const controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  try{
   if(journal)await journal.write({version:1,key:owned.key,body:owned.intent});
   if(!active.current||!foregroundActive.current||controller.signal.aborted)return;
   const value=await api.publishDelivery(owned.intent,owned.key,controller.signal);
   if(!active.current||!foregroundActive.current||controller.signal.aborted)return;
   setPackage(value);attempt.current=null;setRetry(false);
   if(journal){
    try{await journal.clear(owned.key,value)}catch{
     if(active.current&&foregroundActive.current&&!controller.signal.aborted)setError(t('delivery_journal_cleanup_failed'));
    }
   }
  }catch(failure){
   if(!active.current||!foregroundActive.current||controller.signal.aborted)return;
   if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){
    setPackage(null);setItems(null);attempt.current=null;setRetry(false);setError(t('delivery_access_lost'));
   }else{
    // Even a conflict must not silently replace a previous publication intent.
    setRetry(true);setError(t('delivery_publish_failed'));
   }
  }finally{if(flight.current===controller){flight.current=null;if(active.current)setBusy(false)}}
 }
 async function download(item?:DeliveryItem){
  if(!foregroundActive.current||flight.current||!pkg||pkg.state!=='ready'||pkg.published_at===null)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  try{
   const url=await api.downloadDelivery(pkg,item,crypto.randomUUID(),controller.signal);
   if(active.current&&foregroundActive.current&&!controller.signal.aborted)await Linking.openURL(url);
  }catch(failure){
   if(active.current&&foregroundActive.current&&!controller.signal.aborted){
    if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){
     setPackage(null);setItems(null);attempt.current=null;setRetry(false);setError(t('delivery_access_lost'));
    }else setError(t('delivery_download_failed'));
   }
  }finally{if(flight.current===controller){flight.current=null;if(active.current)setBusy(false)}}
 }
 async function openRun(){
  if(!foregroundActive.current||flight.current||!pkg||!onRun)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  try{
   const value=await api.get<unknown>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(pkg.workflow_id)}`,controller.signal);
   controller.signal.throwIfAborted();
   if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('DELIVERY_RUN_SCOPE_INVALID');
   const row=value as Record<string,unknown>;
   if(row.workflow_id!==pkg.workflow_id||typeof row.run_id!=='string'||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(row.run_id))throw new Error('DELIVERY_RUN_SCOPE_INVALID');
   if(active.current&&foregroundActive.current&&!controller.signal.aborted)onRun(row.run_id);
  }catch(failure){
   if(active.current&&foregroundActive.current&&!controller.signal.aborted){
    if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){
     setPackage(null);setItems(null);setInspection(null);setConfirmed(false);attempt.current=null;setRetry(false);setError(t('delivery_access_lost'));
    }else setError(t('delivery_load_failed'));
   }
  }
  finally{if(flight.current===controller){flight.current=null;if(active.current)setBusy(false)}}
 }
 function cancel(){const current=flight.current;if(!current)return;current.abort();flight.current=null;setBusy(false);setInspection(null);setConfirmed(false);if(attempt.current)setRetry(true);setError(t('delivery_load_failed'))}
 useEffect(()=>{if(!busy)return;const current=flight.current;const timer=setTimeout(()=>{if(flight.current===current)cancel()},15000);return()=>clearTimeout(timer)},[busy]);
 const publishable=pkg?.published_at===null&&pkg.document!==null&&(pkg.state==='ready'||pkg.state==='review'&&inspection!==null&&confirmed);
 if(!foreground.active)return <SafeAreaView style={s.root}><Text style={s.title}>{t('delivery_title')}</Text></SafeAreaView>;
 return <SafeAreaView style={s.root}><View style={s.header}>
  <Text style={s.title}>{t('delivery_title')}</Text>
  {busy?<><ActivityIndicator accessibilityLabel={t('delivery_loading')}/><Button accessibilityRole="button" onPress={cancel}>{t('read_cancel')}</Button></>:null}
  {error?<Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text>:null}
  {pkg?<>
   <Text style={s.name}>{pkg.name}</Text>
   <Text style={s.meta}>{t(`delivery_state_${pkg.state}`)}</Text>
   <Text style={s.meta}>{t('delivery_revision')}: {pkg.revision}</Text>
   {onRun&&!busy?<Button  accessibilityRole="button"  onPress={()=>void openRun()} mode="contained" contentStyle={{minHeight:48}}>{t('delivery_open_run')}</Button>:null}
   {pkg.document?<Text selectable style={s.meta}>{t('delivery_manifest')}: {pkg.document.sha256}</Text>:null}
   {!busy?<Button  accessibilityRole="button"  onPress={()=>void loadItems()} mode="contained" contentStyle={{minHeight:48}}>{t('delivery_load_items')}</Button>:null}
   {items!==null?<Text accessibilityLiveRegion="polite" style={s.meta}>{t('delivery_item_count')}: {items.length}</Text>:null}
   {pkg.published_at?<Text accessibilityLiveRegion="polite" style={s.name}>{t('delivery_published')}</Text>:null}
   {pkg.state==='ready'&&pkg.published_at!==null&&pkg.document&&!busy?<Button  accessibilityRole="button"  onPress={()=>void download()} mode="contained" contentStyle={{minHeight:48}}>{t('delivery_download_manifest')}</Button>:null}
   {pkg.state==='review'?<>
    <Text style={s.meta}>{t('delivery_review_pending')}</Text>
    {canEdit&&!busy&&items!==null&&!retry?<Button  accessibilityRole="button"  onPress={()=>void inspect()} mode="contained" contentStyle={{minHeight:48}}>{t('delivery_inspect')}</Button>:null}
    {inspection?<>
     <Text style={s.meta}>{t('delivery_inspection_passed')}</Text>
     {canEdit&&!busy&&!retry?<TouchableRipple  accessibilityRole="checkbox" accessibilityState={{checked:confirmed}} aria-checked={confirmed} style={s.button} onPress={()=>setConfirmed(value=>!value)}><Text style={s.buttonText}>{confirmed?'☑ ':'☐ '}{t('delivery_accept_review')}</Text></TouchableRipple>:null}
    </>:null}
   </>:null}
   {canEdit&&!busy&&(publishable||retry)?<Button  accessibilityRole="button"  onPress={()=>void publish()} mode="contained" contentStyle={{minHeight:48}}>{retry?t('delivery_retry_publish'):t('delivery_publish')}</Button>:null}
  </>:null}
  {canEdit&&!busy&&!pkg&&retry?<Button  accessibilityRole="button"  onPress={()=>void publish()} mode="contained" contentStyle={{minHeight:48}}>{t('delivery_retry_publish')}</Button>:null}
  {!busy?<Button  accessibilityRole="button"  onPress={()=>setEpoch(x=>x+1)} mode="contained" contentStyle={{minHeight:48}}>{t('delivery_refresh')}</Button>:null}
 </View>
 {items!==null?<FlatList data={items} keyExtractor={item=>item.id} initialNumToRender={20} renderItem={({item})=><View style={s.row}><View>
  <Text style={s.name}>{item.ordinal+1} · {item.preset_id}</Text>
  <Text style={s.meta}>{item.required?t('delivery_item_required'):t('delivery_item_optional')} · {t(`delivery_item_${item.state}`)}</Text>
  {item.reason?<Text style={s.meta}>{item.reason}</Text>:null}
  {item.export_id?<Text selectable style={s.meta}>{t('delivery_item_export')}: {item.export_id}</Text>:null}
  {pkg?.state==='ready'&&pkg.published_at!==null&&item.state==='ready'&&!busy?<Button  accessibilityRole="button"  onPress={()=>void download(item)} mode="contained" contentStyle={{minHeight:48}}>{t('delivery_download_slot')}</Button>:null}
  {canEdit&&onSign&&item.state==='ready'&&item.export_id?<Button  accessibilityRole="button"  onPress={()=>onSign(item.export_id!)} mode="contained" contentStyle={{minHeight:48}}>{t('signing_title')}</Button>:null}
 </View></View>}/>:null}
 </SafeAreaView>;
}
