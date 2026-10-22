import {Button,Text,TextInput,TouchableRipple} from 'react-native-paper';
import {readDeadline} from '../../../src/read-deadline';
import {ReadResourceState} from '../../../src/read-resource-state';
import type {ReadPhase} from '../../../src/use-read-resource';
import {collectionApplyDefinitelyRejected} from '../../../src/collection-apply-rejection';
import {APIError} from '@photoeditor/api-client';
import {useReviewForeground} from '../../../src/use-review-foreground';
import {loadApplyCatalogPages} from "../../../src/collection-apply-pages";
import {useLocale} from "../../../src/locale-context";
import AsyncStorage from "@react-native-async-storage/async-storage";
import {CollectionApplyStore,type CollectionApplyRecord} from "../../../src/collection-apply-store";
import{useEffect,useMemo,useRef,useState}from"react";import{router,useLocalSearchParams}from'../../../src/localized-router';import {SafeAreaView,ScrollView,View} from 'react-native';import{useAuth}from"../../../src/auth-context";import{OwnerAPI}from"../../../src/owner-api";import type{Collection,StyleProfile,StyleVersion}from"../../../src/catalog-types";import{catalogStyle as s}from"../../../src/catalog-style";
function ApplyScope(){
 const __displayValue=useLocale().value;

 const __uiMessage=useLocale().message;
const{t}=useLocale();const[versionPhase,setVersionPhase]=useState<ReadPhase>("ready"),[chosenProfile,setChosenProfile]=useState<StyleProfile|null>(null);const{id,tenant}=useLocalSearchParams<{id:string;tenant:string}>();const auth=useAuth();const api=useMemo(()=>new OwnerAPI(auth.client(),tenant||""),[auth,tenant]);const[canEdit,setCanEdit]=useState(false),[reloadEpoch,setReloadEpoch]=useState(0);const[collection,setCollection]=useState<Collection>(),[styles,setStyles]=useState<StyleProfile[]>([]),[versions,setVersions]=useState<StyleVersion[]>([]),[selected,setSelected]=useState(""),[strength,setStrength]=useState("1"),[name,setName]=useState(""),[error,setError]=useState(""),[busy,setBusy]=useState(false);const action=useRef<AbortController|null>(null);const life=useRef<AbortController|null>(null),versionRead=useRef<AbortController|null>(null);const scope=useRef(0),choice=useRef(0),submitting=useRef(false),attempt=useRef<CollectionApplyRecord|null>(null),journal=useRef<CollectionApplyStore|null>(null);const[locked,setLocked]=useState(false),[confirmed,setConfirmed]=useState(false),[loadPhase,setLoadPhase]=useState<ReadPhase>("loading");
function revoke(error:unknown){
 if(!(error instanceof APIError)||![401,403,404].includes(error.status))return false;
 life.current?.abort();action.current?.abort();action.current=null;versionRead.current?.abort();scope.current++;choice.current++;
 setCanEdit(false);setCollection(undefined);setStyles([]);setVersions([]);setChosenProfile(null);setVersionPhase('ready');setSelected('');setName('');setStrength('1');attempt.current=null;journal.current=null;setLocked(false);setConfirmed(false);submitting.current=false;setBusy(false);return true;
}
useEffect(()=>{
 const controller=new AbortController();life.current=controller;const generation=++scope.current;choice.current++;
 attempt.current=null;journal.current=null;setConfirmed(false);submitting.current=false;setLocked(false);setCanEdit(false);setCollection(undefined);setStyles([]);setVersions([]);setSelected("");setName("");setError("");setBusy(false);setLoadPhase('loading');setChosenProfile(null);setVersionPhase('ready');
 const current=()=>scope.current===generation&&!controller.signal.aborted;
 void readDeadline(controller.signal,async signal=>{
  if(!id)throw new Error('COLLECTION_SCOPE_INVALID');
  const c=await api.collection(id,signal);signal.throwIfAborted();
  const access=await api.projectAccess(c.project_id,signal);signal.throwIfAborted();
  const user=await api.me(signal);signal.throwIfAborted();
  const store=new CollectionApplyStore(AsyncStorage,auth.origin||"same-origin",user.id,tenant,c.project_id,id);
  const saved=await store.read();signal.throwIfAborted();
  const profiles=saved?[]:await loadApplyCatalogPages(cursor=>api.styles(c.project_id,cursor,signal),()=>current()&&!signal.aborted);
  signal.throwIfAborted();return {c,access,store,saved,profiles};
 }).then(({c,access,store,saved,profiles})=>{
  if(!current())return;
  journal.current=store;setCollection(c);setCanEdit(access.can_edit&&c.state!=="archived");setStyles(profiles);
  if(saved){attempt.current=saved;setLocked(true);setConfirmed(saved.accepted!==null);setName(saved.name);setStrength(String(saved.strength));setSelected(saved.version);}
  else setName(`${c.name} edited`);
  setLoadPhase('ready');
 }).catch(error=>{if(current()){revoke(error);setLoadPhase('error');setError(t("collection_apply_load_error"));}});
 return()=>{controller.abort();action.current?.abort();action.current=null;versionRead.current?.abort();scope.current++;choice.current++};
},[id,api,reloadEpoch]);
function cancelLoad(){life.current?.abort();action.current?.abort();action.current=null;versionRead.current?.abort();scope.current++;choice.current++;setCanEdit(false);setLoadPhase('cancelled');}
async function choose(x:StyleProfile){
 if(!canEdit||busy||attempt.current)return;
 versionRead.current?.abort();const controller=new AbortController();versionRead.current=controller;
 const generation=scope.current,request=++choice.current;
 const current=()=>scope.current===generation&&choice.current===request&&versionRead.current===controller&&!controller.signal.aborted;
 setChosenProfile(x);setVersionPhase('loading');setVersions([]);setSelected('');setError('');
 try{
  const page=await readDeadline(controller.signal,signal=>loadApplyCatalogPages(cursor=>api.styleVersions(x.id,cursor,signal),()=>current()&&!signal.aborted));
  if(!current())return;setVersions(page);setVersionPhase('ready');
 }catch(error){if(current()){setVersionPhase('error');revoke(error);setError(t('collection_apply_versions_error'));}}
 finally{if(versionRead.current===controller)versionRead.current=null;}
}
function cancelVersions(){versionRead.current?.abort();versionRead.current=null;choice.current++;setVersionPhase('cancelled');}
function stopWaiting(){
 action.current?.abort();action.current=null;submitting.current=false;setBusy(false);setError(t('collection_apply_uncertain'));
}
async function run(){
 if(submitting.current||!journal.current||!life.current||life.current.signal.aborted)return;
 const generation=scope.current,store=journal.current;
 const controller=new AbortController();action.current=controller;const signal=controller.signal;
 const current=()=>scope.current===generation&&action.current===controller&&!signal.aborted;
 submitting.current=true;setBusy(true);setError('');
 let pending=attempt.current;
 try{
  // Only a brand new request asks live capability. Durable pending/accepted
  // attempts must reach receipt recovery even after workers disappear.
  if(!attempt.current){
   const version=versions.find(x=>x.id===selected&&x.approval_status==='accepted');
   if(!canEdit||!collection||!version||!name.trim())return;
   if(name.trim().length>240||Array.from(name.trim()).length>120){setError(t('collection_apply_name_error'));return;}
   const value=Number(strength);if(!Number.isFinite(value)||value<0||value>1){setError(t('collection_apply_strength_error'));return;}
   const frozen={schema_version:1 as const,collection:{id:collection.id,tenant_id:tenant,project_id:collection.project_id,revision:collection.revision},version:selected,strength:value,name:name.trim(),key:crypto.randomUUID(),accepted:null};
   const capability=await readDeadline(signal,transport=>api.collectionApplyCapabilities(collection,version,transport));
   if(!current())return;if(!capability.available){setError(t('capability_unsupported'));return;}
   attempt.current=frozen;setLocked(true);
  }
  pending=attempt.current;if(!pending||!pending.accepted&&!canEdit)return;
  const frozenPending=pending;
  const accepted=await readDeadline(signal,transport=>store.resume(frozenPending,async frozen=>{
   if(!current()||transport.aborted)throw new Error('APPLY_SCOPE_CHANGED');
   return api.apply(frozen.collection,frozen.version,frozen.strength,frozen.name,frozen.key,transport);
  }),30000);
  if(!current())return;
  attempt.current=accepted;setConfirmed(true);
  const target=await readDeadline(signal,transport=>api.run(accepted.accepted!.run_id,transport));
  if(!current())return;
  if(target.id!==accepted.accepted!.run_id)throw new Error('APPLY_RECEIPT_INVALID');
  router.replace({pathname:'/runs/[id]',params:{id:accepted.accepted!.run_id,tenant}});
 }catch(error){
  if(!current())return;
  if(pending&&attempt.current?.accepted===null&&collectionApplyDefinitelyRejected(error)){
   try{await store.discardRejected(pending.key);if(!current())return;attempt.current=null;setLocked(false);setConfirmed(false);setReloadEpoch(value=>value+1);}
   catch{/* Preserve immutable request if storage cannot release it. */}
  }
  if(current()){revoke(error);setError(t('collection_apply_uncertain'));}
 }finally{
  if(scope.current===generation&&action.current===controller){action.current=null;submitting.current=false;setBusy(false);}
 }
}
async function startAnother(){
 if(submitting.current||busy||!confirmed||!journal.current||!attempt.current)return;
 const generation=scope.current,store=journal.current,key=attempt.current.key,controller=new AbortController();
 action.current=controller;submitting.current=true;setBusy(true);setError('');
 const current=()=>scope.current===generation&&action.current===controller&&!controller.signal.aborted;
 try{
  // Cancelling this wait cannot roll back a storage removal. The journal owns
  // exact-key serialization; a later retry reconciles the same acknowledged key.
  await readDeadline(controller.signal,async signal=>{await store.clear(key);signal.throwIfAborted();});
  if(!current())return;
  attempt.current=null;setLocked(false);setConfirmed(false);setSelected('');setVersions([]);setError('');setReloadEpoch(value=>value+1);
 }catch{if(current())setError(t('collection_apply_clear_error'));}
 finally{if(scope.current===generation&&action.current===controller){action.current=null;submitting.current=false;setBusy(false);}}
}return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={s.header}><Text style={s.title}>{t("collection_apply_title")}</Text><ReadResourceState phase={loadPhase} retry={()=>setReloadEpoch(value=>value+1)} cancel={cancelLoad}/><Text style={s.subtitle}>{t("collection_apply_source").replace("{revision}",String(attempt.current?.collection.revision??collection?.revision??""))}</Text><Text style={s.name}>{t("collection_apply_profile")}</Text>{loadPhase==='ready'&&!locked&&styles.length===0?<Text style={s.meta}>{t('collection_apply_profiles_empty')}</Text>:null}{styles.map(x=><Button labelStyle={s.name} accessibilityRole="button" key={x.id}disabled={!canEdit||busy||locked}style={s.row}onPress={()=>void choose(x)}>{x.name}</Button>)}{chosenProfile?<ReadResourceState phase={versionPhase} retry={()=>void choose(chosenProfile)} cancel={cancelVersions}/>:null}{chosenProfile&&versionPhase==='ready'&&versions.length===0?<Text style={s.meta}>{t('collection_apply_versions_empty')}</Text>:null}{versions.map(x=><TouchableRipple accessibilityRole="button" key={x.id}disabled={!canEdit||busy||locked||x.approval_status!=="accepted"}style={s.row}onPress={()=>setSelected(x.id)}><View style={{flex:1,flexDirection:'row',alignItems:'center',justifyContent:'space-between'}}><Text style={s.name}>{selected===x.id?"✓ ":""}{t("collection_apply_version").replace("{number}",String(x.version_no))}</Text><Text style={s.meta}>{__displayValue(x.approval_status)}</Text></View></TouchableRipple>)}<TextInput editable={canEdit&&!locked&&!busy} value={strength}onChangeText={setStrength}keyboardType="decimal-pad"accessibilityLabel={t("collection_apply_strength")} placeholder={t("collection_apply_strength")}placeholderTextColor="#888"style={s.input}/><TextInput editable={canEdit&&!locked&&!busy} value={name}onChangeText={setName}accessibilityLabel={t("collection_apply_name")} placeholder={t("collection_apply_name")}placeholderTextColor="#888"style={s.input}/><Button  accessibilityRole="button" disabled={busy||!journal.current||!selected||!name.trim()||!canEdit&&!attempt.current?.accepted}onPress={run} mode="contained" contentStyle={{minHeight:48}}>{confirmed?t("collection_apply_open"):t("collection_apply_submit")}</Button>{busy&&action.current?<Button  accessibilityRole="button" onPress={stopWaiting}  mode="contained" contentStyle={{minHeight:48}}>{t('read_cancel')}</Button>:null}{confirmed?<Button  accessibilityRole="button" disabled={busy} onPress={()=>void startAnother()}  mode="contained" contentStyle={{minHeight:48}}>{t("collection_apply_another")}</Button>:null}{error?<Text style={s.error}>{__uiMessage(error,true)}</Text>:null}</ScrollView></SafeAreaView>}

export default function Apply(){
 const params=useLocalSearchParams(),auth=useAuth(),foreground=useReviewForeground();
 const client=useMemo(()=>auth.client(),[auth]),generation=useMemo(()=>crypto.randomUUID(),[client]);
 if(!foreground.active)return null;
 return <ApplyScope key={JSON.stringify([generation,foreground.generation,auth.origin,params])}/>;
}
