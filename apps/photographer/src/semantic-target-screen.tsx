import {Button,ActivityIndicator,Text} from 'react-native-paper';
import {readDeadline} from './read-deadline';
import {useWorkspaceAccess} from './workspace-access-context';
import type {SemanticActionAvailability} from './semantic-kind-availability';
import {SemanticShownVersionAction} from './semantic-shown-version-action';
import type {SemanticApplyAttemptStore} from './semantic-apply-attempt-store';
import {useLocale} from './locale-context';
import {SemanticChildProgress} from './semantic-child-progress';
import {SemanticReviewDecisions} from './semantic-review-decisions';
import {SemanticReviewCloseControl} from './semantic-review-close';
import {SemanticPreview} from './semantic-preview';
import {SemanticParentProgress} from './semantic-parent-progress';
import {useCallback,useEffect,useMemo,useRef,useState} from 'react';
import {AppState,FlatList,SafeAreaView,View} from 'react-native';
import type {ApiClient,SemanticTargetResource} from '@photoeditor/api-client';
import {catalogStyle as s} from './catalog-style';
import {mergeSemanticTargets,semanticTargetLabelKeys} from './semantic-targets';

type Props={client:Pick<ApiClient,'semanticTargets'>&Partial<Pick<ApiClient,'materializeSemanticShownVersion'|'semanticReviewPreview'|'semanticAppliedPreview'|'semanticReviewStatus'|'closeSemanticReview'|'applySemanticReview'|'semanticChildStatus'>>;tenant:string;intent:string;actionAvailable?:SemanticActionAvailability;onOpenVersion?:(version:string,asset:string,project:string)=>void;journalFactory?:(signal:AbortSignal)=>Promise<SemanticApplyAttemptStore>};
export function SemanticTargetScreen(props:Props){const access=useWorkspaceAccess(props.tenant);const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.intent,props.journalFactory,access?.actor,access?.canEdit]);return <SemanticTargetScope key={scope} {...props}/>;}
function SemanticTargetScope({client,tenant,intent,journalFactory,onOpenVersion,actionAvailable}:Props){
 const canEdit=useWorkspaceAccess(tenant)?.canEdit===true;
 const {t}=useLocale();
 const[items,setItems]=useState<SemanticTargetResource[]>([]),[cursor,setCursor]=useState<string|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const[reviewEpoch,setReviewEpoch]=useState(0);
 const[decisionPending,setDecisionPending]=useState(!!journalFactory);
 const foreground=useRef((AppState.currentState??'active')==='active');
 const[active,setActive]=useState(foreground.current);
 const flight=useRef<AbortController|null>(null);
 const pendingRefresh=useRef(false),accumulated=useRef<SemanticTargetResource[]>([]),consumed=useRef(new Set<string>());
 const clearRevokedResults=useCallback(()=>{
  pendingRefresh.current=false;flight.current?.abort();flight.current=null;accumulated.current=[];consumed.current.clear();setItems([]);setCursor(null);setBusy(false);setError('semantic_results_failed');setReviewEpoch(value=>value+1);
 },[]);
 const refreshAfterProcessing=useCallback(()=>{
  if(flight.current){pendingRefresh.current=true;return}
  void load();
 },[client,tenant,intent]);
 const refreshAfterParentReady=useCallback(()=>{setReviewEpoch(value=>value+1);refreshAfterProcessing()},[refreshAfterProcessing]);
 function cancelRead(){const controller=flight.current;if(!controller)return;controller.abort();flight.current=null;pendingRefresh.current=false;setBusy(false);}
 async function load(next?:string){
  if(!foreground.current||flight.current)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  if(!next){accumulated.current=[];consumed.current.clear();setItems([]);setCursor(null)}
  try{if(next&&(consumed.current.size>=5000||consumed.current.has(next)))throw new Error('SEMANTIC_CURSOR_REUSED');const page=await readDeadline(controller.signal,signal=>client.semanticTargets(tenant,intent,{limit:50,cursor:next,signal}));if(controller.signal.aborted)return;if(page.next_cursor!==null&&(page.next_cursor.length>2048||!page.next_cursor||!page.items.length||page.next_cursor===next||consumed.current.has(page.next_cursor)))throw new Error('SEMANTIC_CURSOR_INVALID');const merged=mergeSemanticTargets(next?accumulated.current:[],page.items);if(next)consumed.current.add(next);accumulated.current=merged;setItems(merged);setCursor(page.next_cursor)}
  catch{if(!controller.signal.aborted){pendingRefresh.current=false;accumulated.current=[];consumed.current.clear();setItems([]);setCursor(null);setError('semantic_results_failed')}}
  finally{if(flight.current===controller){flight.current=null;if(!controller.signal.aborted){setBusy(false);if(pendingRefresh.current){pendingRefresh.current=false;void load()}}}}
 }
 useEffect(()=>{
  const listener=AppState.addEventListener('change',state=>{
   foreground.current=state==='active';setActive(foreground.current);
   if(!foreground.current){pendingRefresh.current=false;flight.current?.abort();flight.current=null;setBusy(false)}
   else void load();
  });
  void load();return()=>{listener.remove();pendingRefresh.current=false;flight.current?.abort();flight.current=null};
 },[client,tenant,intent]);
 return <SafeAreaView style={s.root}><View style={s.header}><Text accessibilityRole="header" style={s.title}>{t('semantic_results_title')}</Text><Text style={s.subtitle}>{t('semantic_results_subtitle')}</Text><Button accessibilityRole="button" disabled={busy||!active} onPress={()=>void load()}    mode="contained" contentStyle={{minHeight:48}}>{t('semantic_results_refresh')}</Button>{canEdit&&client.semanticReviewStatus?<SemanticParentProgress client={client as Pick<ApiClient,'semanticReviewStatus'>} tenant={tenant} intent={intent} onReady={refreshAfterParentReady} onAccessLost={clearRevokedResults}/>:null}{canEdit&&client.semanticReviewStatus&&client.closeSemanticReview?<SemanticReviewCloseControl blocked={decisionPending} key={tenant+':'+intent+':'+reviewEpoch} client={client as Pick<ApiClient,'semanticReviewStatus'|'closeSemanticReview'>} tenant={tenant} intent={intent}/>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t('semantic_results_failed')}</Text>:null}</View>{canEdit&&(journalFactory||decisionPending||items.some(item=>item.data.state==='proposed'))&&client.semanticReviewStatus&&client.applySemanticReview?<SemanticReviewDecisions onPendingChange={setDecisionPending} actionAvailable={actionAvailable} key={tenant+':'+intent} client={client as Pick<ApiClient,'semanticReviewStatus'|'applySemanticReview'>&Partial<Pick<ApiClient,'semanticReviewPreview'>>} tenant={tenant} intent={intent} items={items} journalFactory={journalFactory} onSubmitted={()=>{setDecisionPending(false);setReviewEpoch(value=>value+1);void load()}}/>:null}{busy?<><ActivityIndicator accessibilityLabel={t('semantic_results_loading')}/><Button accessibilityRole="button" onPress={cancelRead} mode="outlined">{t('read_cancel')}</Button></>:null}<FlatList data={items} initialNumToRender={10} maxToRenderPerBatch={10} windowSize={5} keyExtractor={item=>item.id} contentContainerStyle={s.list} ListEmptyComponent={!busy&&!error?<Text style={s.empty}>{t('semantic_results_empty')}</Text>:null} renderItem={({item})=><View style={s.row}><View><Text style={s.name}>{t(semanticTargetLabelKeys[item.data.state])}</Text><Text style={s.meta}>{t('semantic_results_photo')} {item.data.asset_version_id.slice(0,8)}</Text>{item.data.state==='queued'&&item.data.child_run_id&&client.semanticChildStatus?<SemanticChildProgress key={item.data.child_run_id} client={client as Pick<ApiClient,'semanticChildStatus'>} tenant={tenant} run={item.data.child_run_id} onTerminal={refreshAfterProcessing}/>:null}{canEdit&&item.data.state==='applied'&&client.materializeSemanticShownVersion&&onOpenVersion?<SemanticShownVersionAction onAccessLost={clearRevokedResults} client={client as Pick<ApiClient,'materializeSemanticShownVersion'>} tenant={tenant} target={item.id} onOpen={(version,asset)=>onOpenVersion(version,asset,item.data.project_id)}/>:null}{item.data.state==='applied'&&item.data.child_run_id&&client.semanticAppliedPreview?<SemanticPreview key={item.id+':'+item.data.child_run_id} onAccessLost={clearRevokedResults} target={item.id} client={client as Pick<ApiClient,'semanticAppliedPreview'>} tenant={tenant} run={item.data.child_run_id}/>:null}</View></View>}/>{cursor?<Button accessibilityRole="button" disabled={busy||!active} onPress={()=>void load(cursor)}    mode="contained" contentStyle={{minHeight:48}}>{t('semantic_results_more')}</Button>:null}</SafeAreaView>;
}
