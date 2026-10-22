import {Button,Text,TextInput} from 'react-native-paper';
import {useWorkspaceAccess} from '../../../src/workspace-access-context';
import {LensResultView} from '../../../src/lens-result-view';
import {useLocale} from '../../../src/locale-context';
import {useEffect,useMemo,useRef,useState} from 'react';
import {router,useLocalSearchParams} from '../../../src/localized-router';
import {ScrollView,View} from 'react-native';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {useAuth} from '../../../src/auth-context';
import {useReviewForeground} from '../../../src/use-review-foreground';
import {OwnerAPI} from '../../../src/owner-api';
import {LensClient,type LensChoice,type LensProfile,type LensTask} from '../../../src/lens-client';
import {LensPreparationStore,LensPreparation,beginLensPreparation,type LensPrepared} from '../../../src/lens-preparation';
import {loadCollectionPages} from '../../../src/collection-pages';
import {loadMultiFrameBinding} from '../../../src/multi-frame-binding';
import {multiUUID} from '../../../src/multi-intent';
import type {Collection,CollectionItem,Page} from '../../../src/catalog-types';
import {catalogStyle as s} from '../../../src/catalog-style';

export default function LensRoute(){
 const {id,tenant,request}=useLocalSearchParams<{id:string;tenant:string;request?:string}>(),auth=useAuth(),foreground=useReviewForeground();
 const client=useMemo(()=>auth.client(),[auth]);
 return foreground.active?<LensScope key={JSON.stringify([id,tenant,request,auth.origin,foreground.generation])} id={id} tenant={tenant} request={request} origin={auth.origin} client={client}/>:null;
}
function LensScope({id,tenant,request,origin,client}:{id:string;tenant:string;request?:string;origin:string;client:ReturnType<ReturnType<typeof useAuth>['client']>}){
 const __r2Value=useLocale().value;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const {t}=useLocale();
 const roleCanEdit=useWorkspaceAccess(tenant)?.canEdit===true;
 const [projectCanEdit,setProjectCanEdit]=useState(false);const canEdit=roleCanEdit&&projectCanEdit;
 const owner=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const abort=useRef(new AbortController()),lock=useRef(false),actor=useRef(''),store=useRef<LensPreparationStore|null>(null);
 const [collection,setCollection]=useState<Collection>(),[items,setItems]=useState<CollectionItem[]>([]),[profiles,setProfiles]=useState<LensProfile[]>([]),[choices,setChoices]=useState<Record<string,LensChoice>>({});
 const [loadRevision,setLoadRevision]=useState(0),[loading,setLoading]=useState(true);
 const [archives,setArchives]=useState<string[]>([]);
 const [prepared,setPrepared]=useState<LensPrepared|null>(null),[tasks,setTasks]=useState<LensTask[]>([]),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const lens=useMemo(()=>collection?new LensClient(client,tenant,collection.project_id):null,[client,tenant,collection?.project_id]);
 useEffect(()=>{const controller=new AbortController();abort.current=controller;setLoading(true);setProjectCanEdit(false);setError('');void(async()=>{try{
  if(!multiUUID(id)||!multiUUID(tenant))throw new Error('Invalid collection');
  const root=`/api/v1/tenants/${tenant}/collections/${id}`;
  const [value,user]=await Promise.all([client.request<Collection>(root,{signal:controller.signal}),owner.me(controller.signal)]);
  if(value.id!==id||value.tenant_id!==tenant||!multiUUID(value.project_id))throw new Error('Collection changed');
  const access=await owner.projectAccess(value.project_id,controller.signal);controller.signal.throwIfAborted();
  const journal=new LensPreparationStore(AsyncStorage,origin,user.id,tenant,value.project_id,id),saved=request?await journal.readArchived(request):await journal.read();const history=await journal.archives();
  const frames=await loadCollectionPages(value,cursor=>client.request<Page<CollectionItem>>(`${root}/items?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,{signal:controller.signal}),controller.signal);
  const catalog=new LensClient(client,tenant,value.project_id),all:LensProfile[]=[];let after:string|undefined;
  try{do{const page=await catalog.profiles(after,controller.signal);all.push(...page.items);after=page.next;if(all.length>5000)throw new Error('Profile catalogue too large');}while(after);}catch(e){if(!saved)throw e;}
  controller.signal.throwIfAborted();actor.current=user.id;setProjectCanEdit(access.can_edit);store.current=journal;setCollection(value);setItems(frames);setProfiles(all);setPrepared(saved);setArchives(history);
 }catch(e){if(!controller.signal.aborted)setError(t('lens_request_error'));}finally{if(!controller.signal.aborted)setLoading(false);}})();return()=>controller.abort();},[id,tenant,request,origin,owner,client,loadRevision,roleCanEdit]);
 async function authorize(){const user=await owner.me(abort.current.signal);if(user.id!==actor.current){abort.current.abort();throw new Error('Account changed');}}
 async function act(action:()=>Promise<void>){if(lock.current)return;lock.current=true;setBusy(true);setError('');try{await authorize();await action();}catch(e){if(!abort.current.signal.aborted)setError(t('lens_request_error'));}finally{lock.current=false;if(!abort.current.signal.aborted)setBusy(false);}}
 async function refresh(){if(!lens||!prepared?.ticket)return;await act(async()=>{const all:LensTask[]=[];let after=-1;do{const page=await lens.status(prepared.ticket!.run_id,after,abort.current.signal);all.push(...page.items);if(page.next===undefined)break;after=page.next;}while(true);abort.current.signal.throwIfAborted();const expected=new Set(prepared.sources.map(source=>source.asset_version_id));if(all.length!==expected.size||new Set(all.map(task=>task.asset_version_id)).size!==all.length||new Set(all.map(task=>task.task_id)).size!==all.length||all.some(task=>!expected.has(task.asset_version_id)))throw new Error('Lens task inventory changed');setTasks(all);});}
 async function submit(){if(!canEdit||request||!collection||!lens||!store.current)return;await act(async()=>{
  const saved=await store.current!.read();abort.current.signal.throwIfAborted();setPrepared(saved);
  if(!saved){const selected=Object.values(choices);if(!selected.length)throw new Error('Select photographs and lens profiles');const sources=[];for(const choice of selected){const item=items.find(i=>i.asset_version_id===choice.asset_version_id);if(!item)throw new Error('Source changed');sources.push(await loadMultiFrameBinding(owner,item,abort.current.signal));}await beginLensPreparation(store.current!,actor.current,collection.project_id,sources,selected,selected.map(choice=>{const matches=items.filter(i=>i.asset_version_id===choice.asset_version_id);if(matches.length!==1)throw new Error('Select an unambiguous collection occurrence');return {collection:collection.id,item:matches[0].id,asset:matches[0].asset_id,revision:collection.revision,expectedVersion:choice.asset_version_id};}));setPrepared(await store.current!.read());}
  try{const value=await new LensPreparation(store.current!,(project,request,key,signal)=>owner.createRenderInputSnapshot(project,request,key,signal),(body,versions,key,signal)=>lens.submit(body,versions,key,signal)).resume(abort.current.signal);setPrepared(value);}finally{if(!abort.current.signal.aborted){const latest=await store.current!.read();if(!abort.current.signal.aborted)setPrepared(latest);}}
 });}
 const versions=Array.from(new Map(items.map(i=>[i.asset_version_id,i])).values());
 return <ScrollView><Text>{t('lens_title')}</Text><Text>{t('lens_help')}</Text>{error?<Text accessibilityRole="alert">{__uiMessage(error,true)}</Text>:null}<Button accessibilityRole="button" disabled={loading||busy} onPress={()=>setLoadRevision(n=>n+1)}>{t('lens_reload_collection')}</Button>
 {!prepared?versions.map(item=><View key={item.asset_version_id}><Text>{item.asset_version_id}</Text>{profiles.map(profile=><Button key={profile.profile.artifact_id} disabled={!canEdit||loading||busy} onPress={()=>setChoices(old=>({...old,[item.asset_version_id]:{asset_version_id:item.asset_version_id,profile:profile.profile,strength:old[item.asset_version_id]?.strength??1}}))}>{choices[item.asset_version_id]?.profile.artifact_id===profile.profile.artifact_id?'✓ ':''}{profile.camera_model} / {profile.lens_model} · {profile.focal_mm} {__uiT('ui_4357edf4f080')}{profile.aperture}</Button>)}{choices[item.asset_version_id]?<><Text>{t('lens_strength')}</Text><TextInput editable={canEdit&&!busy} keyboardType="decimal-pad" value={String(choices[item.asset_version_id].strength)} onChangeText={text=>{const strength=Number(text);if(Number.isFinite(strength)&&strength>=0&&strength<=1)setChoices(old=>({...old,[item.asset_version_id]:{...old[item.asset_version_id],strength}}));}}/><Button onPress={()=>setChoices(old=>{const next={...old};delete next[item.asset_version_id];return next;})}>{t('lens_remove')}</Button></>:null}</View>):null}
 <Button  accessibilityRole="button" disabled={!canEdit||!!request||loading||busy||!!prepared?.ticket||!lens} onPress={()=>void submit()}  mode="contained" contentStyle={{minHeight:48}}>{t(prepared?'lens_resume':'lens_process')}</Button>
 {prepared?.ticket?<><Text>{t('lens_saved')}</Text><Button accessibilityRole="button" disabled={busy} onPress={()=>void refresh()}>{t('lens_refresh')}</Button></>:null}
 {!request&&prepared?.ticket&&tasks.length===prepared.choices.length&&tasks.every(task=>['succeeded','failed','cancelled','skipped'].includes(task.state))?<Button accessibilityRole="button" disabled={busy} onPress={()=>void act(async()=>{const root=`/api/v1/tenants/${tenant}/collections/${id}`;const value=await client.request<Collection>(root,{signal:abort.current.signal});if(value.id!==id||value.tenant_id!==tenant||value.project_id!==collection?.project_id)throw new Error('Collection changed');const frames=await loadCollectionPages(value,cursor=>client.request<Page<CollectionItem>>(`${root}/items?limit=100${cursor?`&cursor=${encodeURIComponent(cursor)}`:''}`,{signal:abort.current.signal}),abort.current.signal);await store.current!.archive();abort.current.signal.throwIfAborted();setCollection(value);setItems(frames);setArchives(await store.current!.archives());setPrepared(null);setTasks([]);setChoices({});})}>{t('lens_new_batch')}</Button>:null}
 {archives.map(run=><Button key={run} onPress={()=>router.push({pathname:'/collections/[id]/lens',params:{id,tenant,request:run}})}>{t('lens_archived_request')} {run}</Button>)}
 {request?<Button onPress={()=>router.push({pathname:'/collections/[id]/lens',params:{id,tenant}})}>{t('lens_current_request')}</Button>:null}
 {tasks.map(task=>{const base=prepared?.sources.find(source=>source.asset_version_id===task.asset_version_id),occurrence=prepared?.occurrences?.find(i=>i.expectedVersion===task.asset_version_id),item=items.find(i=>occurrence?i.id===occurrence.item:i.asset_version_id===task.asset_version_id);return <View key={task.task_id}><Text>{task.asset_version_id}: {__r2Value(task.state)}</Text>{task.result&&base&&item&&lens&&collection?<LensResultView canApply={canEdit} key={JSON.stringify([task.result,base])} owner={owner} lens={lens} tenant={tenant} project={collection.project_id} task={task} base={base} item={item} occurrence={occurrence} collection={collection} journalScope={JSON.stringify([origin,actor.current,tenant,id])} signal={abort.current.signal} authorize={authorize}/>:null}</View>;})}</ScrollView>;
}
