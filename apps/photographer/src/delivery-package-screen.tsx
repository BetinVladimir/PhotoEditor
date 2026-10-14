import type {DeliveryPublishAttemptStore} from './delivery-publish-attempt-store';
import {useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,FlatList,Linking,Pressable,SafeAreaView,Text,View} from 'react-native';
import {APIError,type ApiClient} from '@photoeditor/api-client';
import {readDeliveryInspection,type DeliveryInspection} from './delivery-inspection';
import type {DeliveryItem} from './delivery-items';
import {OwnerAPI} from './owner-api';
import {prepareDeliveryPublication,type DeliveryPackage,type DeliveryPublishIntent} from './delivery-package';
import {catalogStyle as s} from './catalog-style';
import {useLocale} from './locale-context';
type Props={journal?:DeliveryPublishAttemptStore;client:ApiClient;tenant:string;project:string;id:string;onSign?:(exportID:string)=>void};
type Attempt={intent:DeliveryPublishIntent;key:string};
export function DeliveryPackageScreen(props:Props){
 const generation=useMemo(()=>crypto.randomUUID(),[props.client,props.journal]);
 return <Scope key={`${generation}:${props.tenant}:${props.project}:${props.id}`} {...props}/>;
}
function Scope({client,tenant,project,id,onSign,journal}:Props){
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
  const controller=new AbortController();flight.current?.abort();flight.current=controller;setBusy(true);setPackage(null);setItems(null);setInspection(null);setConfirmed(false);setError('');
  void (async()=>{
   if(journal){journal.assertScope(tenant,project,id);const saved=await journal.read();if(!active.current||controller.signal.aborted)return null;
    if(saved){attempt.current={intent:saved.body,key:saved.key};setRetry(true)}
   }
   return api.deliveryPackage(project,id,controller.signal);
  })().then(async value=>{
   if(!value||!active.current||controller.signal.aborted)return;
   const saved=attempt.current;
   setPackage(value);
   if(value.published_at!==null){
    attempt.current=null;setRetry(false);
    if(journal&&saved){
     try{await journal.clear(saved.key,value)}catch{
      if(active.current&&!controller.signal.aborted)setError(t('delivery_journal_cleanup_failed'));
     }
    }
   }
  }).catch(()=>{if(active.current&&!controller.signal.aborted)setError(t('delivery_load_failed'))}).finally(()=>{
   if(active.current&&!controller.signal.aborted){flight.current=null;setBusy(false)}
  });
  return()=>controller.abort();
 },[api,project,id,epoch,t,journal]);
 // Observe preparation/assembly without retrying any publication mutation.
 useEffect(()=>{
  if(busy||error||retry||pkg?.state!=='building'||pkg.published_at!==null)return;
  const timer=setTimeout(()=>setEpoch(value=>value+1),2000);
  return()=>clearTimeout(timer);
 },[pkg,busy,error,retry]);
 async function loadItems(){
  if(flight.current||!pkg)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setItems(null);setInspection(null);setConfirmed(false);setError('');
  try{
   const loaded=await api.deliveryItems(pkg,controller.signal);
   if(active.current&&!controller.signal.aborted)setItems(loaded);
  }catch(failure){
   if(active.current&&!controller.signal.aborted){
    if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){
     setPackage(null);setItems(null);attempt.current=null;setRetry(false);setError(t('delivery_access_lost'));
    }else setError(t('delivery_items_failed'));
   }
  }
  finally{if(active.current&&!controller.signal.aborted){flight.current=null;setBusy(false)}}
 }
 async function inspect(){
  if(flight.current||!pkg||items===null)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setInspection(null);setConfirmed(false);setError('');
  try{
   const proof=await api.inspectDelivery(pkg,items,controller.signal);
   if(active.current&&!controller.signal.aborted)setInspection(proof);
  }catch(failure){
   if(active.current&&!controller.signal.aborted){
    if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){
     setPackage(null);setItems(null);attempt.current=null;setRetry(false);setError(t('delivery_access_lost'));
    }else setError(t('delivery_inspection_failed'));
   }
  }finally{if(active.current&&!controller.signal.aborted){flight.current=null;setBusy(false)}}
 }
 async function publish(){
  if(flight.current||!pkg)return;
  let owned=attempt.current;
  try{
   if(!owned){
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
   if(!active.current||controller.signal.aborted)return;
   const value=await api.publishDelivery(owned.intent,owned.key,controller.signal);
   if(!active.current||controller.signal.aborted)return;
   setPackage(value);attempt.current=null;setRetry(false);
   if(journal){
    try{await journal.clear(owned.key,value)}catch{
     if(active.current&&!controller.signal.aborted)setError(t('delivery_journal_cleanup_failed'));
    }
   }
  }catch(failure){
   if(!active.current||controller.signal.aborted)return;
   if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){
    setPackage(null);setItems(null);attempt.current=null;setRetry(false);setError(t('delivery_access_lost'));
   }else{
    // Even a conflict must not silently replace a previous publication intent.
    setRetry(true);setError(t('delivery_publish_failed'));
   }
  }finally{if(active.current&&!controller.signal.aborted){flight.current=null;setBusy(false)}}
 }
 async function download(item?:DeliveryItem){
  if(flight.current||!pkg||pkg.state!=='ready'||pkg.published_at===null)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  try{
   const url=await api.downloadDelivery(pkg,item,crypto.randomUUID(),controller.signal);
   if(active.current&&!controller.signal.aborted)await Linking.openURL(url);
  }catch(failure){
   if(active.current&&!controller.signal.aborted){
    if(failure instanceof APIError&&[401,403,404,410].includes(failure.status)){
     setPackage(null);setItems(null);attempt.current=null;setRetry(false);setError(t('delivery_access_lost'));
    }else setError(t('delivery_download_failed'));
   }
  }finally{if(active.current&&!controller.signal.aborted){flight.current=null;setBusy(false)}}
 }
 const publishable=pkg?.published_at===null&&pkg.document!==null&&(pkg.state==='ready'||pkg.state==='review'&&inspection!==null&&confirmed);
 return <SafeAreaView style={s.root}><View style={s.header}>
  <Text style={s.title}>{t('delivery_title')}</Text>
  {busy?<ActivityIndicator accessibilityLabel={t('delivery_loading')}/>:null}
  {error?<Text accessibilityRole="alert" style={s.error}>{error}</Text>:null}
  {pkg?<>
   <Text style={s.name}>{pkg.name}</Text>
   <Text style={s.meta}>{t(`delivery_state_${pkg.state}`)}</Text>
   <Text style={s.meta}>{t('delivery_revision')}: {pkg.revision}</Text>
   {pkg.document?<Text selectable style={s.meta}>{t('delivery_manifest')}: {pkg.document.sha256}</Text>:null}
   {!busy?<Pressable accessibilityRole="button" style={s.button} onPress={()=>void loadItems()}><Text style={s.buttonText}>{t('delivery_load_items')}</Text></Pressable>:null}
   {items!==null?<Text accessibilityLiveRegion="polite" style={s.meta}>{t('delivery_item_count')}: {items.length}</Text>:null}
   {pkg.published_at?<Text accessibilityLiveRegion="polite" style={s.name}>{t('delivery_published')}</Text>:null}
   {pkg.state==='ready'&&pkg.published_at!==null&&pkg.document&&!busy?<Pressable accessibilityRole="button" style={s.button} onPress={()=>void download()}><Text style={s.buttonText}>{t('delivery_download_manifest')}</Text></Pressable>:null}
   {pkg.state==='review'?<>
    <Text style={s.meta}>{t('delivery_review_pending')}</Text>
    {!busy&&items!==null&&!retry?<Pressable accessibilityRole="button" style={s.button} onPress={()=>void inspect()}><Text style={s.buttonText}>{t('delivery_inspect')}</Text></Pressable>:null}
    {inspection?<>
     <Text style={s.meta}>{t('delivery_inspection_passed')}</Text>
     {!busy&&!retry?<Pressable accessibilityRole="checkbox" accessibilityState={{checked:confirmed}} style={s.button} onPress={()=>setConfirmed(value=>!value)}><Text style={s.buttonText}>{confirmed?'☑ ':'☐ '}{t('delivery_accept_review')}</Text></Pressable>:null}
    </>:null}
   </>:null}
   {!busy&&(publishable||retry)?<Pressable accessibilityRole="button" style={s.button} onPress={()=>void publish()}><Text style={s.buttonText}>{retry?t('delivery_retry_publish'):t('delivery_publish')}</Text></Pressable>:null}
  </>:null}
  {!busy?<Pressable accessibilityRole="button" style={s.button} onPress={()=>setEpoch(x=>x+1)}><Text style={s.buttonText}>{t('delivery_refresh')}</Text></Pressable>:null}
 </View>
 {items!==null?<FlatList data={items} keyExtractor={item=>item.id} initialNumToRender={20} renderItem={({item})=><View style={s.row}><View>
  <Text style={s.name}>{item.ordinal+1} · {item.preset_id}</Text>
  <Text style={s.meta}>{item.required?t('delivery_item_required'):t('delivery_item_optional')} · {t(`delivery_item_${item.state}`)}</Text>
  {item.reason?<Text style={s.meta}>{item.reason}</Text>:null}
  {item.export_id?<Text selectable style={s.meta}>{t('delivery_item_export')}: {item.export_id}</Text>:null}
  {pkg?.state==='ready'&&pkg.published_at!==null&&item.state==='ready'&&!busy?<Pressable accessibilityRole="button" style={s.button} onPress={()=>void download(item)}><Text style={s.buttonText}>{t('delivery_download_slot')}</Text></Pressable>:null}
  {onSign&&item.state==='ready'&&item.export_id?<Pressable accessibilityRole="button" style={s.button} onPress={()=>onSign(item.export_id!)}><Text style={s.buttonText}>{t('signing_title')}</Text></Pressable>:null}
 </View></View>}/>:null}
 </SafeAreaView>;
}
