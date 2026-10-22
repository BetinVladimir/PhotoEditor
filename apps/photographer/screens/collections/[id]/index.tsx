import {ActivityIndicator,Button,Text,TouchableRipple} from 'react-native-paper';
import {CollectionCullingGroups} from '../../../src/collection-culling-groups';
import {CollectionGuestUploads} from '../../../src/collection-guest-uploads';
import {CollectionMetadata} from '../../../src/collection-metadata';
import {CollectionGallery} from '../../../src/collection-gallery';
import {readCreatedCollection} from '../../../src/collection-create-response';
import {readGalleryItem} from '../../../src/collection-gallery-api';
import {bulkUUID} from '../../../src/collection-bulk-api';
import {readContactSheetAccepted} from '../../../src/contact-sheet-response';
import {abandonContactSheet} from '../../../src/contact-sheet-abandon';
import {CollectionAccessLinks} from '../../../src/collection-access-links';
import {CollectionOrganization} from '../../../src/collection-organization';
import {CollectionBulkControls} from '../../../src/collection-bulk-controls';
import {useWorkspaceAccess} from '../../../src/workspace-access-context';
import {readDeadline} from '../../../src/read-deadline';
import {collectionPreviewQueue,type CollectionPreviewQueue} from '../../../src/collection-preview-queue';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {ContactSheetAttemptStore,type ContactSheetAttempt} from '../../../src/contact-sheet-attempt-store';
import {APIError} from '@photoeditor/api-client';
import {useReviewForeground} from '../../../src/use-review-foreground';
import {useEffect,useMemo,useRef,useState} from "react";
import {router,useLocalSearchParams} from '../../../src/localized-router';
import {FlatList,Image,SafeAreaView,StyleSheet,View} from 'react-native';
import {useLocale} from "../../../src/locale-context";
import {useAuth} from "../../../src/auth-context";
import type {Collection,CollectionItem} from "../../../src/catalog-types";
import {OwnerAPI} from "../../../src/owner-api";
import {loadCollectionPages} from "../../../src/collection-pages";
import {saveCompareDecisions} from "../../../src/compare-decisions-save";
import {GalleryComparison} from "../../../src/gallery-comparison";
import {catalogStyle as s} from "../../../src/catalog-style";

export default function CollectionDetail(){
  const{id,tenant}=useLocalSearchParams<{id:string;tenant:string}>();
  const foreground=useReviewForeground();
  const auth=useAuth();
  const client=useMemo(()=>auth.client(),[auth]);
  const generation=useMemo(()=>crypto.randomUUID(),[client]);
  if(!foreground.active)return null;
  return <CollectionScope key={JSON.stringify([generation,foreground.generation,auth.origin,tenant,id])} id={id} tenant={tenant} client={client} origin={auth.origin}/>;
}

function CollectionScope({id,tenant,client,origin}:{id:string;tenant:string;origin:string;client:ReturnType<ReturnType<typeof useAuth>["client"]>}){
 const __displayValue=useLocale().value;

 const __r2T=useLocale().t;const __r2Value=useLocale().value;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;
 const __colorValue=useLocale().value;

  const {t,locale='en'}=useLocale();
  const api=useMemo(()=>new OwnerAPI(client,tenant||""),[client,tenant]);
  const lifetime=useRef<AbortController|null>(null);
  const mutation=useRef(false),mutationOwner=useRef<symbol|null>(null);
  const [childBusy,setChildBusy]=useState(false);
  const acquireChild=()=>{if(mutation.current||!lifetime.current||lifetime.current.signal.aborted)return null;const token=Symbol();mutation.current=true;mutationOwner.current=token;setChildBusy(true);return token};
  const releaseChild=(token:symbol)=>{if(mutationOwner.current===token){mutationOwner.current=null;mutation.current=false;setChildBusy(false)}};
  const sheetFlight=useRef<AbortController|null>(null);
  const decisionFlight=useRef<AbortController|null>(null);
  const previewRead=useRef<((item:CollectionItem)=>Promise<void>)|null>(null);
  const previewVisible=useRef(new Set<string>());
  const visibleItemsChanged=useRef(({viewableItems}:{viewableItems:{item:CollectionItem}[]})=>{previewVisible.current=new Set(viewableItems.map(x=>x.item.id));for(const {item} of viewableItems)void previewRead.current?.(item);}).current;
  const sheetJournal=useRef<ContactSheetAttemptStore|null>(null);
  const[sheetAttempt,setSheetAttempt]=useState<ContactSheetAttempt|null>(null);
  const[projectCanEdit,setCanEdit]=useState(false);
  const workspace=useWorkspaceAccess(tenant);
  const roleCanEdit=workspace?.canEdit===true&&projectCanEdit;
  const[collection,setCollection]=useState<Collection>();
  const canEdit=roleCanEdit&&!!collection&&["draft","published"].includes(collection.state);
  const[items,setItems]=useState<CollectionItem[]>([]);
  const[links,setLinks]=useState<Record<string,string>>({});
  const[moderating,setModerating]=useState<string>();
  const[updatingLabel,setUpdatingLabel]=useState<string>();
  const[savingComparison,setSavingComparison]=useState(false);
  const[creatingSheet,setCreatingSheet]=useState(false);
  const[busy,setBusy]=useState(true);
  const[reloadEpoch,setReloadEpoch]=useState(0);
  const[error,setError]=useState("");

  function accessDenied(error:unknown,controller:AbortController,includeMissing=true){
    if(!(error instanceof APIError)||!(includeMissing?[401,403,404]:[401,403]).includes(error.status)||lifetime.current!==controller)return false;
    controller.abort();setCollection(undefined);setItems([]);setLinks({});setCanEdit(false);setBusy(false);setSheetAttempt(null);sheetJournal.current=null;setError("Collection access is unavailable");return true;
  }
  useEffect(()=>{
    const controller=new AbortController();
    const previewTimers=new Set<ReturnType<typeof setTimeout>>();
    controller.signal.addEventListener("abort",()=>{for(const timer of previewTimers)clearTimeout(timer);previewTimers.clear();},{once:true});
    lifetime.current=controller;setChildBusy(false);mutation.current=false;mutationOwner.current=null;setModerating(undefined);setUpdatingLabel(undefined);setSavingComparison(false);setCreatingSheet(false);previewRead.current=null;sheetJournal.current=null;setSheetAttempt(null);
    setCanEdit(false);setCollection(undefined);setItems([]);setLinks({});setError("");setBusy(true);
    async function load(){
      if(!id||!tenant){setError("Collection is required");setBusy(false);return;}
      const root=`/api/v1/tenants/${encodeURIComponent(tenant)}/collections/${encodeURIComponent(id)}`;
      try{
        const user=await readDeadline(controller.signal,signal=>api.me(signal));if(controller.signal.aborted)return;
        const journal=new ContactSheetAttemptStore(AsyncStorage,origin,user.id,tenant,id),saved=await readDeadline(controller.signal,()=>journal.read());if(controller.signal.aborted)return;
        sheetJournal.current=journal;setSheetAttempt(saved);
        if(saved?.ticket){await readDeadline(controller.signal,signal=>acknowledgeSheet(journal,saved,controller,signal));return;}
        const {c,access,loaded}=await readDeadline(controller.signal,async signal=>{
          const raw=await client.request<unknown>(root,{signal});signal.throwIfAborted();
          if(!raw||typeof raw!=='object'||Array.isArray(raw)||!bulkUUID((raw as Record<string,unknown>).project_id))throw Error('COLLECTION_INVALID');
          const c=readCreatedCollection(raw,tenant,(raw as Record<string,string>).project_id);if(c.id!==id)throw Error('COLLECTION_INVALID');
          const access=await api.projectAccess(c.project_id,signal);signal.throwIfAborted();
          const loaded=await loadCollectionPages(c,async cursor=>{
            const page=await client.request<unknown>(`${root}/items?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:""}`,{signal});signal.throwIfAborted();
            if(!page||typeof page!=='object'||Array.isArray(page)||Object.keys(page).some(k=>!['items','next_cursor','revision'].includes(k))||!Array.isArray((page as Record<string,unknown>).items))throw Error('COLLECTION_PAGE_INVALID');
            const v=page as {items:unknown[];next_cursor?:string|null;revision:string};return {...v,items:v.items.map(item=>readGalleryItem(item,c.id))};
          },signal);
          signal.throwIfAborted();return {c,access,loaded};
        });
        if(controller.signal.aborted||lifetime.current!==controller)return;
        setCanEdit(access.can_edit);setCollection(c);setItems(loaded);
        previewVisible.current=new Set(loaded.slice(0,24).map(item=>item.id));
        const readPreview:CollectionPreviewQueue<CollectionItem>=collectionPreviewQueue<CollectionItem>(controller.signal,async item=>{
          if(controller.signal.aborted)return;
          try{
            const out=await readDeadline(controller.signal,signal=>api.mediaLinks(item.asset_id,item.asset_version_id,["thumb640"],signal));
            const link=out.items.find(x=>x.variant==="thumb640"&&x.asset_version_id===item.asset_version_id);
            const remaining=link?Date.parse(link.expires_at)-Date.now():NaN;
            if(link?.url&&Number.isFinite(remaining)&&remaining>0&&!controller.signal.aborted){
              const url=link.url;setLinks(old=>({...old,[item.id]:url}));
              const timer=setTimeout(()=>{
                previewTimers.delete(timer);if(controller.signal.aborted)return;
                setLinks(old=>{if(old[item.id]!==url)return old;const next={...old};delete next[item.id];return next;});
                readPreview.invalidate(item.id);
                if(previewVisible.current.has(item.id))void readPreview(item);
              },Math.min(remaining,2147483647));previewTimers.add(timer);
            }
            else if(!controller.signal.aborted)return false;
          }catch(error){
            if(accessDenied(error,controller,false)||controller.signal.aborted)return;
            if(!(error instanceof APIError)||error.status===408||error.status===429||error.status>=500||(error.status===409&&error.code==="MEDIA_NOT_READY"))return false;
          }
        },4,{shouldRetry:item=>previewVisible.current.has(item.id)});
        previewRead.current=readPreview;
        // Preview transport must not keep authoritative metadata in a loading state.
        void Promise.all(loaded.slice(0,24).map(readPreview));
      }catch(error){if(!controller.signal.aborted&&!accessDenied(error,controller))setError("Unable to load collection");}
      finally{if(!controller.signal.aborted)setBusy(false);}
    }
    void load();return()=>{controller.abort();if(lifetime.current===controller){lifetime.current=null;previewRead.current=null;}};
  },[id,tenant,client,api,reloadEpoch,origin,workspace?.actor,workspace?.canEdit]);

  async function moderate(item:CollectionItem,decision:"accept"|"reject"){
    if(!canEdit||mutation.current)return;
    const controller=lifetime.current;setModerating(item.id);
    try{await saveComparison({[item.asset_version_id]:decision});}
    finally{if(controller&&lifetime.current===controller&&!controller.signal.aborted)setModerating(undefined);}
  }
  async function collectionDecision<T>(controller:AbortController,action:(signal:AbortSignal)=>Promise<T>){
    const flight=new AbortController();decisionFlight.current=flight;
    const abort=()=>flight.abort();controller.signal.addEventListener('abort',abort,{once:true});
    try{return await readDeadline(flight.signal,action,15000);}
    finally{controller.signal.removeEventListener('abort',abort);if(decisionFlight.current===flight)decisionFlight.current=null;}
  }
  async function setColorLabel(item:CollectionItem,label:CollectionItem["color_label"]|null){
    const controller=lifetime.current;
    if(!canEdit||!collection||mutation.current||!controller||controller.signal.aborted)return;
    const owner=Symbol();mutationOwner.current=owner;mutation.current=true;setUpdatingLabel(item.id);setError("");
    try{
      await collectionDecision(controller,signal=>api.setItemColorLabel(collection,item,label,signal));
      if(controller.signal.aborted||lifetime.current!==controller)return;
      setCollection(undefined);setItems([]);setBusy(true);setUpdatingLabel(undefined);setReloadEpoch(value=>value+1);
    }catch(error){
      if(!controller.signal.aborted&&lifetime.current===controller&&!accessDenied(error,controller)){
        setError("Color label could not be confirmed. Reload the collection before retrying.");setCollection(undefined);
      }
    }finally{if(mutationOwner.current===owner){mutationOwner.current=null;mutation.current=false;if(!controller.signal.aborted&&lifetime.current===controller)setUpdatingLabel(undefined);}}
  }
  async function saveComparison(decisions:Record<string,"accept"|"reject">){
    const controller=lifetime.current;
    if(!canEdit||!collection||mutation.current||!controller||controller.signal.aborted)return;
    const owner=Symbol();mutationOwner.current=owner;mutation.current=true;setSavingComparison(true);setError("");
    try{
      const result=await collectionDecision(controller,signal=>saveCompareDecisions(collection,items,decisions,(value,item,decision)=>api.moderateApplyItem(value,item,decision,signal),signal));
      if(controller.signal.aborted||lifetime.current!==controller)return;
      setItems(values=>values.map(item=>result.items.find(updated=>updated.id===item.id)||item));
      const incomplete=result.results.some(value=>value.status!=="saved");setCollection(incomplete?undefined:result.collection);
      if(incomplete)setError("Some decisions were not saved. Reload the collection before retrying.");
    }catch(error){if(!controller.signal.aborted&&lifetime.current===controller&&!accessDenied(error,controller)){
      setCollection(undefined);setError("Decisions could not be confirmed. Reload the collection before retrying.");
    }}finally{if(mutationOwner.current===owner){mutationOwner.current=null;mutation.current=false;if(!controller.signal.aborted&&lifetime.current===controller)setSavingComparison(false);}}
  }
  async function acknowledgeSheet(store:ContactSheetAttemptStore,record:ContactSheetAttempt,controller:AbortController,signal=controller.signal){
    if(!record.ticket)throw new Error('CONTACT_SHEET_RECEIPT_MISSING');
    const run=await api.run(record.ticket.run_id,signal);signal.throwIfAborted();if(controller.signal.aborted||lifetime.current!==controller)return;
    if(run.id!==record.ticket.run_id)throw new Error('CONTACT_SHEET_RECEIPT_INVALID');
    router.push({pathname:'/runs/[id]',params:{id:run.id,tenant}});
    // A prior accepted-receipt write may have failed. Upgrade the exact saved
    // attempt after publishing its verified destination, before acknowledging it.
    await store.write(record);signal.throwIfAborted();
    await store.clearAccepted(record.key,run.id);signal.throwIfAborted();if(!controller.signal.aborted&&lifetime.current===controller)setSheetAttempt(null);
  }
  async function createContactSheet(){
    const controller=lifetime.current,store=sheetJournal.current;
    if((!canEdit&&!sheetAttempt?.ticket)||(!sheetAttempt&&!collection)||mutation.current||!store||!controller||controller.signal.aborted)return;
    const flight=new AbortController();sheetFlight.current=flight;
    const abort=()=>flight.abort();controller.signal.addEventListener('abort',abort,{once:true});
    const owner=Symbol();mutationOwner.current=owner;mutation.current=true;setCreatingSheet(true);setError('');let current=sheetAttempt;
    const own=()=>lifetime.current===controller&&!controller.signal.aborted&&sheetFlight.current===flight;
    try{
      await readDeadline(flight.signal,async signal=>{
        const check=()=>{signal.throwIfAborted();if(!own())throw new Error('CONTACT_SHEET_SCOPE_CHANGED');};
        const saved=await store.read();check();
        if(saved){if(current&&(saved.key!==current.key||JSON.stringify(saved.request)!==JSON.stringify(current.request)||saved.ticket&&current.ticket&&saved.ticket.run_id!==current.ticket.run_id))throw new Error('CONTACT_SHEET_JOURNAL_CONFLICT');if(!current?.ticket)current=saved;}
        else if(current?.ticket){current=null;setSheetAttempt(null);throw new Error('CONTACT_SHEET_ALREADY_ACKNOWLEDGED');}
        if(!current){if(!collection)throw new Error('COLLECTION_MISSING');const snapshot=await api.selectionSnapshot(collection,undefined,signal);check();current={schema_version:1,collection_id:id,key:crypto.randomUUID(),request:{selection_snapshot_id:snapshot.id,locale,columns:4,rows:5},ticket:null};}
        setSheetAttempt(current);
        if(current.ticket){await acknowledgeSheet(store,current,controller,signal);check();return;}
        await store.write(current);check();
        const run=await api.contactSheet(current.request.selection_snapshot_id,current.request.locale,current.request.columns,current.request.rows,signal,current.key);
        check();const receipt=readContactSheetAccepted(run);
        current={...current,ticket:{run_id:receipt.run_id}};
        // Keep the validated accepted identity even when local receipt storage
        // fails. This mounted attempt must recover through run GET, never POST.
        setSheetAttempt(current);await store.write(current);check();
        setSheetAttempt(current);await acknowledgeSheet(store,current,controller,signal);check();
      },15000);
    }catch(error){if(own()){
      // A generic refusal cannot prove that an earlier concurrent admission
      // did not commit. Keep its immutable key/body until receipt-aware closure.
      if(!accessDenied(error,controller))setError(flight.signal.aborted?'Contact sheet confirmation cancelled. Retry uses the saved request.':'Contact sheet could not be confirmed. Retry uses the saved request.');
    }}finally{
      controller.signal.removeEventListener('abort',abort);
      if(sheetFlight.current===flight){sheetFlight.current=null;if(mutationOwner.current===owner){mutationOwner.current=null;mutation.current=false;}if(lifetime.current===controller&&!controller.signal.aborted)setCreatingSheet(false);}
    }
  }

  async function closeContactSheet(){
    const controller=lifetime.current,store=sheetJournal.current,record=sheetAttempt;
    if(!roleCanEdit||!record||record.ticket||!store||!controller||controller.signal.aborted||mutation.current)return;
    const flight=new AbortController(),owner=Symbol();sheetFlight.current=flight;mutation.current=true;mutationOwner.current=owner;setCreatingSheet(true);setError('');
    const abort=()=>flight.abort();controller.signal.addEventListener('abort',abort,{once:true});
    const own=()=>lifetime.current===controller&&!controller.signal.aborted&&sheetFlight.current===flight;
    try{await readDeadline(flight.signal,async signal=>{
      const check=()=>{signal.throwIfAborted();if(!own())throw Error('CONTACT_SHEET_SCOPE_CHANGED');};check();
      const resolution=await abandonContactSheet(client,tenant,record,signal);check();
      if(resolution.status==='accepted'){
        const accepted={...record,ticket:{run_id:resolution.ticket.run_id}};setSheetAttempt(accepted);
        await store.write(accepted);check();await acknowledgeSheet(store,accepted,controller,signal);return;
      }
      await store.clearAbandoned(record.key);check();setSheetAttempt(null);
    },15000);}catch(error){if(own()&&!accessDenied(error,controller))setError(locale==='ru'?'Закрытие контактного листа не подтверждено. Сохраните запрос и повторите.':locale==='bg'?'Затварянето на контактния лист не е потвърдено. Запазете заявката и опитайте отново.':'Contact sheet closure could not be confirmed. Keep the saved request and retry.');}
    finally{controller.signal.removeEventListener('abort',abort);if(sheetFlight.current===flight){sheetFlight.current=null;if(mutationOwner.current===owner){mutationOwner.current=null;mutation.current=false;}if(lifetime.current===controller&&!controller.signal.aborted)setCreatingSheet(false);}}
  }

  const childLifetime=lifetime.current;
  const childCurrent=()=>!!childLifetime&&lifetime.current===childLifetime&&!childLifetime.signal.aborted;
  const displayedSheetFlight=sheetFlight.current,displayedDecisionFlight=decisionFlight.current;
  if(busy&&!collection)return <SafeAreaView style={s.root}><ActivityIndicator style={s.loader}/>
    <Button  accessibilityRole="button" onPress={()=>{if(!childCurrent())return;childLifetime!.abort();setBusy(false);setError('Collection loading cancelled. Reload to retry.');}}  mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_6984f4bae3c9')}</Button>
  </SafeAreaView>;
  return <SafeAreaView style={s.root}>
    <View style={s.header}>
      <Text accessibilityRole="header" style={s.title}>{collection?.name||__r2T('ui_7b790708ff1b')}</Text>
      <Button  accessibilityRole="button" disabled={childBusy||busy||savingComparison||creatingSheet||!!moderating} onPress={()=>{if(!mutation.current)setReloadEpoch(value=>value+1);}}  mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_e35f1239a64e')}</Button>
      <Text style={s.subtitle}>{items.length} {__uiT('ui_7a4b4249996e')}{' '}{__r2Value(collection?.state)}</Text>
      <Button  accessibilityRole="button" disabled={childBusy||!canEdit||!collection||busy||savingComparison||creatingSheet||!!moderating||!!updatingLabel} onPress={()=>router.push({pathname:"/collections/[id]/apply",params:{id,tenant}})}  mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_1745c4124fdc')}</Button>
      <Button  accessibilityRole="button" disabled={childBusy||!canEdit||busy||!collection} onPress={()=>router.push({pathname:'/collections/[id]/lens',params:{id,tenant}})}  mode="contained" contentStyle={{minHeight:48}}>{t('lens_title')}</Button>
      {([['optical.panorama','multi_create_panorama'],['optical.focus_stack','multi_create_focus']] as const).map(([operation,label])=><Button  key={operation} accessibilityRole="button" disabled={childBusy||!canEdit||busy||savingComparison||creatingSheet||!!moderating||!collection} onPress={()=>router.push({pathname:'/collections/[id]/multi',params:{id,tenant,operation}})}  mode="contained" contentStyle={{minHeight:48}}>{t(label)}</Button>)}
      <Button  accessibilityRole="button" disabled={childBusy||busy||creatingSheet||savingComparison||!!moderating||!!updatingLabel||!canEdit&&!sheetAttempt?.ticket||!sheetAttempt&&(!collection||items.length===0)} onPress={()=>void createContactSheet()}  mode="contained" contentStyle={{minHeight:48}}>{creatingSheet?__uiT('ui_5fd4ee689a09'):sheetAttempt?__uiT('ui_22830cb662bb'):__uiT('ui_b41ecf4029c1')}</Button>
      {sheetAttempt&&!sheetAttempt.ticket&&roleCanEdit?<Button  accessibilityRole="button" disabled={childBusy||busy||creatingSheet||savingComparison||!!moderating||!!updatingLabel} onPress={()=>void closeContactSheet()}  mode="contained" contentStyle={{minHeight:48}}>{locale==='ru'?__uiT('ui_d4d536b97271'):locale==='bg'?__uiT('ui_d8b44d192c3f'):__uiT('ui_28082bbfbcd1')}</Button>:null}
      {(savingComparison||!!updatingLabel)?<Button  accessibilityRole="button" onPress={()=>{if(childCurrent()&&displayedDecisionFlight&&decisionFlight.current===displayedDecisionFlight)displayedDecisionFlight.abort();}}  mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_7e7e6e25bf71')}</Button>:null}
      {creatingSheet?<Button  accessibilityRole="button" onPress={()=>{if(childCurrent()&&displayedSheetFlight&&sheetFlight.current===displayedSheetFlight)displayedSheetFlight.abort();}}  mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_b4181f9c6cdd')}</Button>:null}
      {error?<Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text>:null}
    </View>
    {collection&&!busy?<CollectionBulkControls key={JSON.stringify([collection.id,collection.revision,reloadEpoch])} client={client} origin={origin} tenant={tenant} collection={collection} items={items} canEdit={roleCanEdit} current={childCurrent} acquire={acquireChild} release={releaseChild} onChanged={()=>{if(childCurrent())setReloadEpoch(value=>value+1)}}/>:null}
    {collection&&!busy?<CollectionOrganization key={JSON.stringify(['organization',collection.id,collection.revision,reloadEpoch])} client={client} origin={origin} tenant={tenant} collectionId={collection.id} canEdit={roleCanEdit} current={childCurrent} acquire={acquireChild} release={releaseChild} onChanged={()=>{if(childCurrent())setReloadEpoch(value=>value+1)}}/>:null}
    {collection&&!busy?<CollectionMetadata client={client} tenant={tenant} collection={collection} items={items} canEdit={roleCanEdit} current={childCurrent} acquire={acquireChild} release={releaseChild} onChanged={()=>{if(childCurrent())setReloadEpoch(value=>value+1)}}/>:null}
    {collection&&!busy&&roleCanEdit?<CollectionAccessLinks key={JSON.stringify(['access',collection.id,collection.revision,reloadEpoch])} client={client} origin={origin} tenant={tenant} collectionId={collection.id} canEdit={canEdit} canRecoverEdit={roleCanEdit} current={childCurrent} acquire={acquireChild} release={releaseChild} onChanged={()=>{if(childCurrent())setReloadEpoch(value=>value+1)}}/>:null}
    {collection&&!busy&&roleCanEdit?<CollectionGuestUploads client={client} origin={origin} tenant={tenant} collectionId={collection.id} state={collection.state} canEdit={roleCanEdit} current={childCurrent} acquire={acquireChild} release={releaseChild} onChanged={()=>{if(childCurrent())setReloadEpoch(value=>value+1)}}/>:null}
    {collection&&!busy&&roleCanEdit?<CollectionCullingGroups client={client} tenant={tenant} collection={collection} items={items} current={childCurrent}/>:null}
    {collection&&!busy?<CollectionGallery client={client} tenant={tenant} collection={collection} items={items} current={childCurrent}>{visibleItems=><FlatList onViewableItemsChanged={visibleItemsChanged} ListHeaderComponent={<GalleryComparison key={reloadEpoch} items={visibleItems} api={api} onAccessDenied={error=>{const controller=lifetime.current;if(controller)accessDenied(error,controller);}} onSave={canEdit&&collection?.source_run_id?decisions=>void saveComparison(decisions):undefined} saving={savingComparison}/>} data={visibleItems} numColumns={2} keyExtractor={x=>x.id} contentContainerStyle={grid.list} columnWrapperStyle={grid.columns}
      ListEmptyComponent={<Text style={s.empty}>{__uiT('ui_6797d1f91ac2')}</Text>}
      renderItem={({item})=><View style={grid.card}>
        <TouchableRipple accessibilityRole="button" accessibilityLabel={__uiT('ui_7cf2f6908046', {p0:(item.position+1)})} onPress={()=>router.push({pathname:"/editor/[version]",params:{version:item.asset_version_id,asset:item.asset_id,tenant,collection:id,item:item.id}})}><View style={{alignSelf:'stretch'}}>
          {links[item.id]?<Image source={{uri:links[item.id]}} style={grid.image} resizeMode="cover"/>:<View style={[grid.image,grid.pending]}><Text style={s.meta}>{__uiT('ui_469f06e7353d')}</Text></View>}
          <Text numberOfLines={1} style={s.name}>{__uiT('ui_d84eebada93e')}{' '}{item.position+1}</Text>
          <Text style={s.meta}>{item.rating?__uiT('ui_efa952cf61d2', {p0:(item.rating)}):item.moderation_state}</Text>
          {item.color_label?<Text style={s.meta} accessibilityLabel={__uiT('ui_7695c3bb9159', {p0:__displayValue((item.color_label))})}>{__displayValue(item.color_label)}</Text>:null}
        </View></TouchableRipple>
        <View style={grid.labels}>
          {([null,"Red","Yellow","Green","Blue","Purple"] as const).map(label=><Button labelStyle={grid.labelText} key={label??"clear"} accessibilityRole="button" accessibilityLabel={label?__uiT('ui_4ab1c60a54ef', {p0:(__colorValue(label)),p1:(item.position+1)}):__uiT('ui_8ba1e6ce84f2', {p0:(item.position+1)})} disabled={childBusy||!canEdit||!collection||!!updatingLabel||mutation.current||item.color_label===(label??undefined)} onPress={()=>void setColorLabel(item,label)} style={[grid.labelButton,item.color_label===(label??undefined)&&grid.labelSelected]}>{label?__colorValue(label):"×"}</Button>)}
        </View>
        {canEdit&&item.moderation_state==="pending_review"&&collection?.source_run_id?<View style={grid.actions}>
          <Button labelStyle={grid.actionText} accessibilityRole="button" disabled={childBusy||!canEdit||!!moderating||savingComparison||creatingSheet} accessibilityLabel={__uiT('ui_f0b0facb0e22', {p0:(item.position+1)})} onPress={()=>void moderate(item,"accept")} style={[grid.action,grid.accept]}>{__uiT('ui_89713b9c9c1b')}</Button>
          <Button labelStyle={grid.actionText} accessibilityRole="button" disabled={childBusy||!canEdit||!!moderating||savingComparison||creatingSheet} accessibilityLabel={__uiT('ui_cb5f978a476b', {p0:(item.position+1)})} onPress={()=>void moderate(item,"reject")} style={[grid.action,grid.reject]}>{__uiT('ui_ab604a360777')}</Button>
        </View>:null}
      </View>}/>}</CollectionGallery>:null}
  </SafeAreaView>;
}

const grid=StyleSheet.create({
  list:{padding:12,gap:12},columns:{gap:12},card:{flex:1,minWidth:0,backgroundColor:"#303030",padding:8,borderRadius:10,gap:8},
  image:{width:"100%",aspectRatio:1,borderRadius:7,backgroundColor:"#444"},pending:{alignItems:"center",justifyContent:"center"},
  actions:{flexDirection:"row",gap:6},action:{flex:1,paddingVertical:8,borderRadius:6,alignItems:"center"},accept:{backgroundColor:"#287a45"},reject:{backgroundColor:"#8b3434"},actionText:{color:"white",fontWeight:"700"},
  labels:{flexDirection:"row",flexWrap:"wrap",gap:4},labelButton:{paddingHorizontal:6,paddingVertical:4,borderRadius:5,backgroundColor:"#454545"},labelSelected:{borderWidth:1,borderColor:"white"},labelText:{color:"white",fontSize:14,lineHeight:20},
});
