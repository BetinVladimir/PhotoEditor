import {MultiApplyInbox} from '../../../src/multi-apply-inbox';
import {multiCollectionSelection} from '../../../src/multi-collection-selection';
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from 'expo-router';
import {ActivityIndicator,FlatList,Image,Pressable,SafeAreaView,Text,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {useAuth} from '../../../src/auth-context';
import {useLocale} from '../../../src/locale-context';
import {useCommands} from '../../../src/use-commands';
import {OwnerAPI} from '../../../src/owner-api';
import type {Collection,CollectionItem,Page} from '../../../src/catalog-types';
import {loadCollectionPages} from '../../../src/collection-pages';
import {MultiReviewView} from '../../../src/multi-review-view';
import {MultiSelection} from '../../../src/multi-selection';
import {multiUUID,type MultiCapabilities,type MultiOperation} from '../../../src/multi-intent';
import {MultiPreparationStore,type MultiPreparationRecord} from '../../../src/multi-attempt-store';
import {beginMultiPreparation,MultiPreparation} from '../../../src/multi-preparation';
import {loadMultiFrameBinding} from '../../../src/multi-frame-binding';
import {catalogStyle as s} from '../../../src/catalog-style';

export default function MultiRoute(){
 const {id,tenant,operation}=useLocalSearchParams<{id:string;tenant:string;operation:string}>();
 const auth=useAuth(),client=useMemo(()=>auth.client(),[auth]);
 return <MultiScope key={JSON.stringify([id,tenant,operation])} id={id} tenant={tenant} operation={operation} origin={auth.origin} client={client}/>;
}
function MultiScope({id,tenant,operation,origin,client}:{id:string;tenant:string;operation:string;origin:string;client:ReturnType<ReturnType<typeof useAuth>['client']>}){
 const {t}=useLocale();
 const api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const lifetime=useRef<AbortController|null>(null),mutating=useRef(false);
 const selection=useRef<MultiSelection|null>(null),journal=useRef<MultiPreparationStore|null>(null);
 const [collection,setCollection]=useState<Collection>(),[items,setItems]=useState<CollectionItem[]>([]);
 const [capabilities,setCapabilities]=useState<MultiCapabilities|null>(null),[prepared,setPrepared]=useState<MultiPreparationRecord|null>(null);
 const [revision,setRevision]=useState(0),[reload,setReload]=useState(0),[busy,setBusy]=useState(true),[error,setError]=useState<Parameters<ReturnType<typeof useLocale>['t']>[0]|''>('');
 const supported=operation==='optical.panorama'||operation==='optical.focus_stack';
 useEffect(()=>{
  const controller=new AbortController();lifetime.current=controller;
  selection.current=null;journal.current=null;setCollection(undefined);setItems([]);setCapabilities(null);setPrepared(null);setBusy(true);setError('');
  async function load(){
   if(!multiUUID(id)||!multiUUID(tenant)||!supported){setError('multi_scope_error');setBusy(false);return;}
   try{
    const root=`/api/v1/tenants/${encodeURIComponent(tenant)}/collections/${encodeURIComponent(id)}`;
    const [value,user]=await Promise.all([client.request<Collection>(root,{signal:controller.signal}),api.me(controller.signal)]);
    controller.signal.throwIfAborted();
    if(value.id!==id||value.tenant_id!==tenant||!multiUUID(value.project_id))throw new Error('MULTI_COLLECTION_INVALID');
    const store=new MultiPreparationStore(AsyncStorage,origin,user.id,tenant,value.project_id,operation as MultiOperation);
    const saved=await store.read();controller.signal.throwIfAborted();
    journal.current=store;setCollection(value);setPrepared(saved);
    // Recovery remains available when fresh authoring is unavailable. Server repeats authority on retry.
    const [caps,frames]=await Promise.allSettled([
     api.multiCapabilities(operation as MultiOperation,value.project_id,controller.signal),
     loadCollectionPages(value,cursor=>client.request<Page<CollectionItem>>(`${root}/items?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,{signal:controller.signal}),controller.signal),
    ]);
    controller.signal.throwIfAborted();
    // Existing acknowledged results still need the current collection CAS
    // binding when new producer admission is temporarily unavailable.
    if(frames.status==='fulfilled')setItems(frames.value);
    if(caps.status==='fulfilled'&&frames.status==='fulfilled'){
     setCapabilities(caps.value);selection.current=new MultiSelection(caps.value);setRevision(n=>n+1);
    }else setError(saved?'multi_recovery_only':'multi_unavailable');
   }catch{if(!controller.signal.aborted)setError('multi_load_error');}
   finally{if(!controller.signal.aborted)setBusy(false);}
  }
  void load();return()=>{controller.abort();if(lifetime.current===controller)lifetime.current=null;};
 },[id,tenant,operation,origin,client,api,reload,supported]);
 const live=()=>{const controller=lifetime.current;if(!controller||controller.signal.aborted)throw new Error('MULTI_ATTEMPT_CANCELLED');return controller;};
 async function add(item:CollectionItem){
  if(mutating.current||prepared||!selection.current)return;
  const controller=live(),current=selection.current;mutating.current=true;setBusy(true);setError('');
  try{
   const binding=await loadMultiFrameBinding(api,item,controller.signal);
   if(lifetime.current!==controller)return;current.add(binding);setRevision(n=>n+1);
  }catch{if(!controller.signal.aborted)setError('multi_frame_error');}
  finally{mutating.current=false;if(!controller.signal.aborted)setBusy(false);}
 }
 function edit(action:(value:MultiSelection)=>void){
  if(mutating.current||prepared||!selection.current)return;
  try{action(selection.current);setRevision(n=>n+1);setError('');}catch{setError('multi_edit_error');}
 }
 async function submit(){
  if(mutating.current||!journal.current||!collection)return;
  const controller=live(),store=journal.current;mutating.current=true;setBusy(true);setError('');
  try{
   if(!prepared){
    if(!selection.current)throw new Error('MULTI_SELECTION_INVALID');
    const snapshotKey=crypto.randomUUID();
    await beginMultiPreparation(store,operation as MultiOperation,collection.project_id,selection.current.build(snapshotKey).params,{snapshot:snapshotKey,intent:crypto.randomUUID()});
   }
   controller.signal.throwIfAborted();
   const saved=await store.read();controller.signal.throwIfAborted();setPrepared(saved);
   const ticket=await new MultiPreparation(store,api).submit(controller.signal);
   controller.signal.throwIfAborted();
   const accepted=await store.read();controller.signal.throwIfAborted();setPrepared(accepted);
   router.push({pathname:'/runs/[id]',params:{id:ticket.run_id,tenant}});
  }catch{
   if(!controller.signal.aborted){
    try{const saved=await store.read();if(!controller.signal.aborted)setPrepared(saved);}catch{
     if(!controller.signal.aborted){journal.current=null;selection.current=null;setCapabilities(null);}
    }
    if(!controller.signal.aborted)setError('multi_unknown');
   }
  }finally{mutating.current=false;if(!controller.signal.aborted)setBusy(false);}
 }
 async function startAnother(){
  if(mutating.current||!prepared?.attempt?.ticket||!journal.current||!capabilities)return;
  const controller=live();mutating.current=true;setBusy(true);
  try{await journal.current.clear(prepared.intent_key);controller.signal.throwIfAborted();selection.current=new MultiSelection(capabilities);setPrepared(null);setRevision(n=>n+1);setError('');}
  catch{if(!controller.signal.aborted)setError('multi_clear_error');}
  finally{mutating.current=false;if(!controller.signal.aborted)setBusy(false);}
 }
 const draft=selection.current?.snapshot;
 const frames=prepared?.params.frames??draft?.frames??[];
 const reference=prepared?prepared.params.frames[prepared.params.reference_ordinal].asset_version_id:draft?.reference_version;
 const currentScope=collection?.id===id&&collection?.tenant_id===tenant;
 const currentPrepared=currentScope&&prepared?.project===collection?.project_id&&prepared?.operation===operation;
 const applySelection=currentScope&&currentPrepared&&collection&&reference?multiCollectionSelection(collection,items,reference):null;
 const ticket=prepared?.attempt?.ticket;
 const locked=busy||prepared!==null;
 // Revision rerenders the owned selection; no React state stores mutable frame bindings.
 void revision;
 useCommands({'multi.submit':()=>{if(!busy&&journal.current&&(prepared||frames.length>=2))void submit();}});
 return <SafeAreaView style={s.root}><FlatList data={prepared?[]:items} keyExtractor={item=>item.id} contentContainerStyle={s.list} ListHeaderComponent={<View style={s.header}>
  <Text accessibilityRole="header" style={s.title}>{t(operation==='optical.focus_stack'?'multi_focus':'multi_panorama')}</Text>
  <Text style={s.subtitle}>{collection?.name??t('multi_collection')}</Text>
  {busy?<ActivityIndicator accessibilityLabel={t('multi_busy')}/>:null}
  {error?<Text accessibilityRole="alert" style={s.error}>{t(error)}</Text>:null}
  <Pressable accessibilityRole="button" disabled={busy} onPress={()=>setReload(n=>n+1)} style={s.button}><Text style={s.buttonText}>{t('multi_reload')}</Text></Pressable>
  <Text style={s.name}>{t('multi_frames')} · {frames.length}{capabilities?` / ${capabilities.frame_limit}`:''}</Text>
  {prepared?<Text style={s.subtitle}>{t(ticket?'multi_acknowledged':'multi_locked')}</Text>:<Text style={s.subtitle}>{t('multi_instructions')}</Text>}
  {frames.map((frame,index)=><View key={frame.asset_version_id} style={{gap:8,paddingVertical:8}}>
   <Text style={s.name}>{index+1}. {items.find(item=>item.asset_version_id===frame.asset_version_id)?`${t('multi_photo')} ${items.find(item=>item.asset_version_id===frame.asset_version_id)!.position+1}`:frame.asset_version_id}{reference===frame.asset_version_id?` · ${t('multi_reference')}`:''}</Text>
   <View style={{flexDirection:'row',flexWrap:'wrap',gap:8}}>
    <Pressable accessibilityRole="button" accessibilityLabel={t('multi_move_up').replace('{n}',String(index+1))} disabled={locked||index===0} onPress={()=>edit(value=>value.move(frame.asset_version_id,index-1))} style={s.button}><Text style={s.buttonText}>{t('multi_up')}</Text></Pressable>
    <Pressable accessibilityRole="button" accessibilityLabel={t('multi_move_down').replace('{n}',String(index+1))} disabled={locked||index===frames.length-1} onPress={()=>edit(value=>value.move(frame.asset_version_id,index+1))} style={s.button}><Text style={s.buttonText}>{t('multi_down')}</Text></Pressable>
    <Pressable accessibilityRole="button" accessibilityLabel={t('multi_use_reference').replace('{n}',String(index+1))} disabled={locked} onPress={()=>edit(value=>value.selectReference(frame.asset_version_id))} style={s.button}><Text style={s.buttonText}>{t('multi_reference')}</Text></Pressable>
    <Pressable accessibilityRole="button" accessibilityLabel={t('multi_remove_frame').replace('{n}',String(index+1))} disabled={locked} onPress={()=>edit(value=>value.remove(frame.asset_version_id))} style={s.button}><Text style={s.buttonText}>{t('multi_remove')}</Text></Pressable>
   </View>
  </View>)}
  {operation==='optical.panorama'?<View style={{gap:8}}><Text style={s.name}>{t('multi_projection')}</Text>{(['planar','cylindrical'] as const).map(value=><Pressable key={value} accessibilityRole="button" disabled={locked||!capabilities} onPress={()=>edit(selection=>selection.setProjection(value))} style={s.button}><Text style={s.buttonText}>{(prepared?'projection' in prepared.params&&prepared.params.projection===value:draft?.projection===value)?'✓ ':''}{t(value==='planar'?'multi_planar':'multi_cylindrical')}</Text></Pressable>)}</View>:<View style={{gap:8}}><Text style={s.name}>{t('multi_motion')}</Text>{(['review','reject'] as const).map(value=><Pressable key={value} accessibilityRole="button" disabled={locked||!capabilities} onPress={()=>edit(selection=>selection.setMotionPolicy(value))} style={s.button}><Text style={s.buttonText}>{(prepared?'motion_policy' in prepared.params&&prepared.params.motion_policy===value:draft?.motion_policy===value)?'✓ ':''}{t(value==='review'?'multi_motion_review':'multi_motion_reject')}</Text></Pressable>)}</View>}
  <Pressable accessibilityRole="button" disabled={busy||!journal.current||(!prepared&&frames.length<2)} onPress={()=>void submit()} style={s.button}><Text style={s.buttonText}>{t(ticket?'multi_open_run':prepared?'multi_retry':'multi_submit')}</Text></Pressable>
  {collection?<MultiApplyInbox key={JSON.stringify([origin,tenant,collection.project_id,id])} api={api} origin={origin} tenant={tenant} project={collection.project_id} collection={id} onApplied={()=>router.push({pathname:'/collections/[id]',params:{id,tenant}})}/>:null}
  {currentPrepared&&ticket&&prepared?.attempt?<MultiReviewView key={JSON.stringify([origin,tenant,id,operation,prepared.intent_key])} api={api} tenant={tenant} attempt={prepared.attempt} apply={{api,origin,collection:id,selection:applySelection,disabled:busy||!applySelection,onApplied:value=>{if(applySelection)router.push({pathname:'/editor/[version]',params:{version:value.asset_version_id,tenant,asset:applySelection.asset,collection:id,item:applySelection.item}});else router.push({pathname:'/collections/[id]',params:{id,tenant}});}}}/>:null}
  {ticket?<Pressable accessibilityRole="button" disabled={busy||!capabilities} onPress={()=>void startAnother()} style={s.button}><Text style={s.buttonText}>{t('multi_start_another')}</Text></Pressable>:null}
  {!prepared?<Text style={s.name}>{t('multi_collection_photos')}</Text>:null}
 </View>} renderItem={({item})=><MultiPhoto item={item} api={api} disabled={locked||!capabilities||frames.length>=(capabilities?.frame_limit??0)||frames.some(frame=>frame.asset_version_id===item.asset_version_id)} onAdd={()=>void add(item)}/>}/></SafeAreaView>;
}

function MultiPhoto({item,api,disabled,onAdd}:{item:CollectionItem;api:OwnerAPI;disabled:boolean;onAdd:()=>void}){
 const {t}=useLocale();
 const [url,setURL]=useState<string|null>(null);
 useEffect(()=>{
  const controller=new AbortController();setURL(null);
  void api.mediaLinks(item.asset_id,item.asset_version_id,['thumb640'],controller.signal).then(result=>{
   if(!controller.signal.aborted)setURL(result.items.find(link=>link.variant==='thumb640')?.url??null);
  }).catch(()=>{});
  return()=>controller.abort();
 },[api,item.asset_id,item.asset_version_id]);
 return <Pressable accessibilityRole="button" accessibilityLabel={t('multi_add_photo').replace('{n}',String(item.position+1))} disabled={disabled} onPress={onAdd} style={s.row}>
  {url?<Image source={{uri:url}} accessibilityLabel={t('multi_preview').replace('{n}',String(item.position+1))} style={{width:80,height:80,borderRadius:6}} resizeMode="cover"/>:<Text style={s.meta}>{t('multi_preview_pending')}</Text>}
  <Text style={s.name}>{t('multi_add_photo').replace('{n}',String(item.position+1))}</Text>
 </Pressable>;
}
