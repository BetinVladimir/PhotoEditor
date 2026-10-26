import {ActivityIndicator,Button,Text,TouchableRipple} from 'react-native-paper';
import {useWorkspaceAccess} from '../../../src/workspace-access-context';
import {authorizeMultiOperation} from '../../../src/multi-operation-admission';
import {APIError} from '@photoeditor/api-client';
import {useReviewForeground} from '../../../src/use-review-foreground';
import {MultiApplyInbox} from '../../../src/multi-apply-inbox';
import {multiCollectionSelection} from '../../../src/multi-collection-selection';
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from '../../../src/localized-router';
import {FlatList,Image,SafeAreaView,View} from 'react-native';
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
 const auth=useAuth(),client=useMemo(()=>auth.client(),[auth]),foreground=useReviewForeground();
 return foreground.active?<MultiScope key={JSON.stringify([id,tenant,operation,auth.origin,foreground.generation])} id={id} tenant={tenant} operation={operation} origin={auth.origin} client={client}/>:null;
}
function MultiScope({id,tenant,operation,origin,client}:{id:string;tenant:string;operation:string;origin:string;client:ReturnType<ReturnType<typeof useAuth>['client']>}){
 const __uiT=useLocale().t;

 const {t}=useLocale();
 const roleCanEdit=useWorkspaceAccess(tenant)?.canEdit===true;
 const [projectCanEdit,setProjectCanEdit]=useState(false);const canEdit=roleCanEdit&&projectCanEdit;
 const api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const lifetime=useRef<AbortController|null>(null),mutating=useRef(false);
 const actor=useRef<string|null>(null);
 const selection=useRef<MultiSelection|null>(null),journal=useRef<MultiPreparationStore|null>(null);
 const [collection,setCollection]=useState<Collection>(),[items,setItems]=useState<CollectionItem[]>([]);
 const [capabilities,setCapabilities]=useState<MultiCapabilities|null>(null),[prepared,setPrepared]=useState<MultiPreparationRecord|null>(null);
 const [revision,setRevision]=useState(0),[reload,setReload]=useState(0),[busy,setBusy]=useState(true),[error,setError]=useState<Parameters<ReturnType<typeof useLocale>['t']>[0]|''>('');
 const supported=operation==='optical.panorama'||operation==='optical.focus_stack';
 useEffect(()=>{
  const controller=new AbortController();lifetime.current=controller;
  setProjectCanEdit(false);actor.current=null;selection.current=null;journal.current=null;setCollection(undefined);setItems([]);setCapabilities(null);setPrepared(null);setBusy(true);setError('');
  async function load(){
   if(!multiUUID(id)||!multiUUID(tenant)||!supported){setError('multi_scope_error');setBusy(false);return;}
   try{
    const root=`/api/v1/tenants/${encodeURIComponent(tenant)}/collections/${encodeURIComponent(id)}`;
    const [value,user]=await Promise.all([client.request<Collection>(root,{signal:controller.signal}),api.me(controller.signal)]);
    controller.signal.throwIfAborted();
    if(value.id!==id||value.tenant_id!==tenant||!multiUUID(value.project_id))throw new Error('MULTI_COLLECTION_INVALID');
    const access=await api.projectAccess(value.project_id,controller.signal);controller.signal.throwIfAborted();
    const store=new MultiPreparationStore(AsyncStorage,origin,user.id,tenant,value.project_id,operation as MultiOperation);
    const saved=await store.read();controller.signal.throwIfAborted();
    actor.current=user.id;setProjectCanEdit(access.can_edit);journal.current=store;setCollection(value);setPrepared(saved);
    // Recovery remains available when fresh authoring is unavailable. Server repeats authority on retry.
    const [caps,frames]=await Promise.allSettled([
     api.multiCapabilities(operation as MultiOperation,value.project_id,controller.signal),
     loadCollectionPages(value,cursor=>client.request<Page<CollectionItem>>(`${root}/items?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,{signal:controller.signal}),controller.signal),
    ]);
    controller.signal.throwIfAborted();
    for(const result of [caps,frames])if(result.status==='rejected'&&result.reason instanceof APIError&&[401,403,404,410].includes(result.reason.status)){withdrawAccess();return;}
    await assertActor(controller);
    // Existing acknowledged results still need the current collection CAS
    // binding when new producer admission is temporarily unavailable.
    if(frames.status==='fulfilled')setItems(frames.value);
    if(caps.status==='fulfilled'&&frames.status==='fulfilled'){
     setCapabilities(caps.value);selection.current=new MultiSelection(caps.value);setRevision(n=>n+1);
    }else setError(saved?'multi_recovery_only':'multi_unavailable');
   }catch(error){if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status))withdrawAccess();else setError('multi_load_error');}}
   finally{if(!controller.signal.aborted)setBusy(false);}
  }
  void load();return()=>{controller.abort();if(lifetime.current===controller)lifetime.current=null;};
 },[id,tenant,operation,origin,client,api,reload,supported,roleCanEdit]);
 function cancelWaiting(){lifetime.current?.abort();mutating.current=false;actor.current=null;journal.current=null;setCollection(undefined);setItems([]);setCapabilities(null);setBusy(false);setError('multi_load_error');}
 useEffect(()=>{if(!busy)return;const timer=setTimeout(cancelWaiting,collection?15000:8000);return()=>clearTimeout(timer);},[busy,collection]);
 const live=()=>{const controller=lifetime.current;if(!controller||controller.signal.aborted)throw new Error('MULTI_ATTEMPT_CANCELLED');return controller;};
 function withdrawAccess(){lifetime.current?.abort();actor.current=null;journal.current=null;selection.current=null;setCollection(undefined);setItems([]);setCapabilities(null);setPrepared(null);setBusy(false);setError('multi_load_error');}
 async function assertActor(controller:AbortController){controller.signal.throwIfAborted();const user=await api.me(controller.signal);controller.signal.throwIfAborted();if(lifetime.current!==controller||!actor.current||user.id!==actor.current){withdrawAccess();throw new Error('MULTI_ACTOR_CHANGED');}}
 async function add(item:CollectionItem){
  if(!canEdit||mutating.current||prepared||!selection.current)return;
  const controller=live(),current=selection.current;mutating.current=true;setBusy(true);setError('');
  try{
   const binding=await loadMultiFrameBinding(api,item,controller.signal);
   if(lifetime.current!==controller)return;current.add(binding);setRevision(n=>n+1);
  }catch{if(!controller.signal.aborted)setError('multi_frame_error');}
  finally{if(lifetime.current===controller&&!controller.signal.aborted){mutating.current=false;setBusy(false);}}
 }
 function edit(action:(value:MultiSelection)=>void){
  if(!canEdit||mutating.current||prepared||!selection.current)return;
  try{action(selection.current);setRevision(n=>n+1);setError('');}catch{setError('multi_edit_error');}
 }
 async function submit(){
  if(!canEdit&&!prepared?.attempt?.ticket||mutating.current||!journal.current||!collection)return;
  const controller=live(),store=journal.current;mutating.current=true;setBusy(true);setError('');
  try{
   await assertActor(controller);
   if(!prepared){
    if(!selection.current)throw new Error('MULTI_SELECTION_INVALID');
    const snapshotKey=crypto.randomUUID();
    await beginMultiPreparation(store,operation as MultiOperation,collection.project_id,selection.current.build(snapshotKey).params,{snapshot:snapshotKey,intent:crypto.randomUUID()});
   }
   controller.signal.throwIfAborted();
   const saved=await store.read();controller.signal.throwIfAborted();setPrepared(saved);
   const ticket=await new MultiPreparation(store,api,async(operation,project,signal)=>{await assertActor(controller);await authorizeMultiOperation(api,operation,project,()=>lifetime.current===controller,signal);}).submit(controller.signal);
   controller.signal.throwIfAborted();
   const accepted=await store.read();controller.signal.throwIfAborted();setPrepared(accepted);
   await assertActor(controller);
   router.push({pathname:'/runs/[id]',params:{id:ticket.run_id,tenant}});
  }catch(error){
   if(!controller.signal.aborted&&error instanceof APIError&&[401,403,404,410].includes(error.status)){withdrawAccess();return;}
   if(!controller.signal.aborted){
    try{const saved=await store.read();if(!controller.signal.aborted)setPrepared(saved);}catch{
     if(!controller.signal.aborted){journal.current=null;selection.current=null;setCapabilities(null);}
    }
    if(!controller.signal.aborted)setError('multi_unknown');
   }
  }finally{if(lifetime.current===controller&&!controller.signal.aborted){mutating.current=false;setBusy(false);}}
 }
 async function startAnother(){
  if(mutating.current||!prepared?.attempt?.ticket||!journal.current||!capabilities)return;
  const controller=live();mutating.current=true;setBusy(true);
  try{await journal.current.clear(prepared.intent_key);controller.signal.throwIfAborted();selection.current=new MultiSelection(capabilities);setPrepared(null);setRevision(n=>n+1);setError('');}
  catch{if(!controller.signal.aborted)setError('multi_clear_error');}
  finally{if(lifetime.current===controller&&!controller.signal.aborted){mutating.current=false;setBusy(false);}}
 }
 const draft=selection.current?.snapshot;
 const frames=prepared?.params.frames??draft?.frames??[];
 const reference=prepared?prepared.params.frames[prepared.params.reference_ordinal].asset_version_id:draft?.reference_version;
 const currentScope=collection?.id===id&&collection?.tenant_id===tenant;
 const currentPrepared=currentScope&&prepared?.project===collection?.project_id&&prepared?.operation===operation;
 const applySelection=currentScope&&currentPrepared&&collection&&reference?multiCollectionSelection(collection,items,reference):null;
 const ticket=prepared?.attempt?.ticket;
 const locked=!canEdit||busy||prepared!==null;
 // Revision rerenders the owned selection; no React state stores mutable frame bindings.
 void revision;
 useCommands({'multi.submit':()=>{if(!busy&&journal.current&&(prepared||frames.length>=2))void submit();}});
 return <SafeAreaView style={s.root}><FlatList data={prepared?[]:items} keyExtractor={item=>item.id} contentContainerStyle={s.list} ListHeaderComponent={<View style={s.header}>
  <Text accessibilityRole="header" style={s.title}>{t(operation==='optical.focus_stack'?'multi_focus':'multi_panorama')}</Text>
  <Text style={s.subtitle}>{collection?.name??t('multi_collection')}</Text>
  {busy?<ActivityIndicator accessibilityLabel={t('multi_busy')}/>:null}
  {error?<Text accessibilityRole="alert" style={s.error}>{t(error)}</Text>:null}
  {busy?<Button accessibilityRole="button" onPress={cancelWaiting}>{t('read_cancel')}</Button>:null}
  <Button  accessibilityRole="button" disabled={busy} onPress={()=>setReload(n=>n+1)}  mode="contained" contentStyle={{minHeight:48}}>{t('multi_reload')}</Button>
  <Text style={s.name}>{t('multi_frames')} · {frames.length}{capabilities?__uiT('ui_eec3e13813c1', {p0:(capabilities.frame_limit)}):''}</Text>
  {prepared?<Text style={s.subtitle}>{t(ticket?'multi_acknowledged':'multi_locked')}</Text>:<Text style={s.subtitle}>{t('multi_instructions')}</Text>}
  {frames.map((frame,index)=><View key={frame.asset_version_id} style={{gap:8,paddingVertical:8}}>
   <Text style={s.name}>{index+1}. {items.find(item=>item.asset_version_id===frame.asset_version_id)?__uiT('ui_8cbaa55a52ef', {p0:(t('multi_photo')),p1:(items.findIndex(item=>item.asset_version_id===frame.asset_version_id)+1)}):frame.asset_version_id}{reference===frame.asset_version_id?__uiT('ui_04420b9dfe3f', {p0:(t('multi_reference'))}):''}</Text>
   <View style={{flexDirection:'row',flexWrap:'wrap',gap:8}}>
    <Button  accessibilityRole="button" accessibilityLabel={t('multi_move_up').replace('{n}',String(index+1))} disabled={locked||index===0} onPress={()=>edit(value=>value.move(frame.asset_version_id,index-1))}  mode="contained" contentStyle={{minHeight:48}}>{t('multi_up')}</Button>
    <Button  accessibilityRole="button" accessibilityLabel={t('multi_move_down').replace('{n}',String(index+1))} disabled={locked||index===frames.length-1} onPress={()=>edit(value=>value.move(frame.asset_version_id,index+1))}  mode="contained" contentStyle={{minHeight:48}}>{t('multi_down')}</Button>
    <Button  accessibilityRole="button" accessibilityLabel={t('multi_use_reference').replace('{n}',String(index+1))} disabled={locked} onPress={()=>edit(value=>value.selectReference(frame.asset_version_id))}  mode="contained" contentStyle={{minHeight:48}}>{t('multi_reference')}</Button>
    <Button  accessibilityRole="button" accessibilityLabel={t('multi_remove_frame').replace('{n}',String(index+1))} disabled={locked} onPress={()=>edit(value=>value.remove(frame.asset_version_id))}  mode="contained" contentStyle={{minHeight:48}}>{t('multi_remove')}</Button>
   </View>
  </View>)}
  {operation==='optical.panorama'?<View style={{gap:8}}><Text style={s.name}>{t('multi_projection')}</Text>{(['planar','cylindrical'] as const).map(value=><Button  key={value} accessibilityRole="button" disabled={locked||!capabilities} onPress={()=>edit(selection=>selection.setProjection(value))}  mode="contained" contentStyle={{minHeight:48}}>{(prepared?'projection' in prepared.params&&prepared.params.projection===value:draft?.projection===value)?'✓ ':''}{t(value==='planar'?'multi_planar':'multi_cylindrical')}</Button>)}</View>:<View style={{gap:8}}><Text style={s.name}>{t('multi_motion')}</Text>{(['review','reject'] as const).map(value=><Button  key={value} accessibilityRole="button" disabled={locked||!capabilities} onPress={()=>edit(selection=>selection.setMotionPolicy(value))}  mode="contained" contentStyle={{minHeight:48}}>{(prepared?'motion_policy' in prepared.params&&prepared.params.motion_policy===value:draft?.motion_policy===value)?'✓ ':''}{t(value==='review'?'multi_motion_review':'multi_motion_reject')}</Button>)}</View>}
  <Button  accessibilityRole="button" disabled={!canEdit&&!ticket||busy||!journal.current||(!prepared&&frames.length<2)} onPress={()=>void submit()}  mode="contained" contentStyle={{minHeight:48}}>{t(ticket?'multi_open_run':prepared?'multi_retry':'multi_submit')}</Button>
  {canEdit&&collection?<MultiApplyInbox key={JSON.stringify([origin,tenant,collection.project_id,id])} api={api} origin={origin} tenant={tenant} project={collection.project_id} collection={id} onApplied={()=>router.push({pathname:'/collections/[id]',params:{id,tenant}})}/>:null}
  {currentPrepared&&ticket&&prepared?.attempt?<MultiReviewView key={JSON.stringify([origin,tenant,id,operation,prepared.intent_key])} api={api} tenant={tenant} onAccessDenied={withdrawAccess} attempt={prepared.attempt} apply={{api,origin,collection:id,selection:applySelection,disabled:busy||!applySelection,onApplied:value=>{if(applySelection)router.push({pathname:'/editor/[version]',params:{version:value.asset_version_id,tenant,asset:applySelection.asset,collection:id,item:applySelection.item}});else router.push({pathname:'/collections/[id]',params:{id,tenant}});}}}/>:null}
  {ticket?<Button  accessibilityRole="button" disabled={busy||!capabilities} onPress={()=>void startAnother()}  mode="contained" contentStyle={{minHeight:48}}>{t('multi_start_another')}</Button>:null}
  {!prepared?<Text style={s.name}>{t('multi_collection_photos')}</Text>:null}
 </View>} renderItem={({item,index})=><MultiPhoto item={item} ordinal={index+1} api={api} disabled={locked||!capabilities||frames.length>=(capabilities?.frame_limit??0)||frames.some(frame=>frame.asset_version_id===item.asset_version_id)} onAdd={()=>void add(item)}/>}/></SafeAreaView>;
}

function MultiPhoto({item,ordinal,api,disabled,onAdd}:{item:CollectionItem;ordinal:number;api:OwnerAPI;disabled:boolean;onAdd:()=>void}){
 const {t}=useLocale();
 const [url,setURL]=useState<string|null>(null);
 useEffect(()=>{
  const controller=new AbortController();setURL(null);
  void api.mediaLinks(item.asset_id,item.asset_version_id,['thumb640'],controller.signal).then(result=>{
   if(!controller.signal.aborted)setURL(result.items.find(link=>link.variant==='thumb640')?.url??null);
  }).catch(()=>{});
  return()=>controller.abort();
 },[api,item.asset_id,item.asset_version_id]);
 return <TouchableRipple accessibilityRole="button" accessibilityLabel={t('multi_add_photo').replace('{n}',String(ordinal))} disabled={disabled} onPress={onAdd} style={s.row}><View style={{flex:1,flexDirection:'row',alignItems:'center',justifyContent:'space-between'}}>
  {url?<Image source={{uri:url}} accessibilityLabel={t('multi_preview').replace('{n}',String(ordinal))} style={{width:80,height:80,borderRadius:6}} resizeMode="cover"/>:<Text style={s.meta}>{t('multi_preview_pending')}</Text>}
  <Text style={s.name}>{t('multi_add_photo').replace('{n}',String(ordinal))}</Text>
 </View></TouchableRipple>;
}
