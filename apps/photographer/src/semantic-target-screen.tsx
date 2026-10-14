import {useLocale} from './locale-context';
import {SemanticChildProgress} from './semantic-child-progress';
import {SemanticReviewDecisions} from './semantic-review-decisions';
import {SemanticReviewCloseControl} from './semantic-review-close';
import {SemanticPreview} from './semantic-preview';
import {SemanticParentProgress} from './semantic-parent-progress';
import {useCallback,useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,FlatList,Pressable,SafeAreaView,Text,View} from 'react-native';
import type {ApiClient,SemanticTargetResource} from '@photoeditor/api-client';
import {catalogStyle as s} from './catalog-style';
import {mergeSemanticTargets,semanticTargetLabelKeys} from './semantic-targets';

type Props={client:Pick<ApiClient,'semanticTargets'>&Partial<Pick<ApiClient,'runOutputs'|'semanticReviewStatus'|'closeSemanticReview'|'applySemanticReview'|'semanticChildStatus'>>;tenant:string;intent:string};
export function SemanticTargetScreen(props:Props){const scope=useMemo(()=>crypto.randomUUID(),[props.client,props.tenant,props.intent]);return <SemanticTargetScope key={scope} {...props}/>;}
function SemanticTargetScope({client,tenant,intent}:Props){
 const {t}=useLocale();
 const[items,setItems]=useState<SemanticTargetResource[]>([]),[cursor,setCursor]=useState<string|null>(null),[busy,setBusy]=useState(false),[error,setError]=useState('');
 const[reviewEpoch,setReviewEpoch]=useState(0);
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
 async function load(next?:string){
  if(flight.current)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  if(!next){accumulated.current=[];consumed.current.clear();setItems([]);setCursor(null)}
  try{if(next&&(consumed.current.size>=5000||consumed.current.has(next)))throw new Error('SEMANTIC_CURSOR_REUSED');const page=await client.semanticTargets(tenant,intent,{limit:50,cursor:next,signal:controller.signal});if(controller.signal.aborted)return;if(page.next_cursor!==null&&(page.next_cursor.length>2048||!page.next_cursor||!page.items.length||page.next_cursor===next||consumed.current.has(page.next_cursor)))throw new Error('SEMANTIC_CURSOR_INVALID');const merged=mergeSemanticTargets(next?accumulated.current:[],page.items);if(next)consumed.current.add(next);accumulated.current=merged;setItems(merged);setCursor(page.next_cursor)}
  catch{if(!controller.signal.aborted){pendingRefresh.current=false;accumulated.current=[];consumed.current.clear();setItems([]);setCursor(null);setError('semantic_results_failed')}}
  finally{if(flight.current===controller){flight.current=null;if(!controller.signal.aborted){setBusy(false);if(pendingRefresh.current){pendingRefresh.current=false;void load()}}}}
 }
 useEffect(()=>{void load();return()=>{pendingRefresh.current=false;flight.current?.abort();flight.current=null}},[client,tenant,intent]);
 return <SafeAreaView style={s.root}><View style={s.header}><Text accessibilityRole="header" style={s.title}>{t('semantic_results_title')}</Text><Text style={s.subtitle}>{t('semantic_results_subtitle')}</Text><Pressable accessibilityRole="button" disabled={busy} onPress={()=>void load()} style={s.button}><Text style={s.buttonText}>{t('semantic_results_refresh')}</Text></Pressable>{client.semanticReviewStatus?<SemanticParentProgress client={client as Pick<ApiClient,'semanticReviewStatus'>} tenant={tenant} intent={intent} onReady={refreshAfterParentReady} onAccessLost={clearRevokedResults}/>:null}{client.semanticReviewStatus&&client.closeSemanticReview?<SemanticReviewCloseControl key={tenant+':'+intent+':'+reviewEpoch} client={client as Pick<ApiClient,'semanticReviewStatus'|'closeSemanticReview'>} tenant={tenant} intent={intent}/>:null}{error?<Text accessibilityRole="alert" style={s.error}>{t('semantic_results_failed')}</Text>:null}</View>{items.some(item=>item.data.state==='proposed')&&client.semanticReviewStatus&&client.applySemanticReview?<SemanticReviewDecisions key={tenant+':'+intent} client={client as Pick<ApiClient,'semanticReviewStatus'|'applySemanticReview'>} tenant={tenant} intent={intent} items={items} onSubmitted={()=>{setReviewEpoch(value=>value+1);void load()}}/>:null}{busy?<ActivityIndicator accessibilityLabel={t('semantic_results_loading')}/>:null}<FlatList data={items} initialNumToRender={10} maxToRenderPerBatch={10} windowSize={5} keyExtractor={item=>item.id} contentContainerStyle={s.list} ListEmptyComponent={!busy&&!error?<Text style={s.empty}>{t('semantic_results_empty')}</Text>:null} renderItem={({item})=><View style={s.row}><View><Text style={s.name}>{t(semanticTargetLabelKeys[item.data.state])}</Text><Text style={s.meta}>{t('semantic_results_photo')} {item.data.asset_version_id.slice(0,8)}</Text>{item.data.state==='queued'&&item.data.child_run_id&&client.semanticChildStatus?<SemanticChildProgress key={item.data.child_run_id} client={client as Pick<ApiClient,'semanticChildStatus'>} tenant={tenant} run={item.data.child_run_id} onTerminal={refreshAfterProcessing}/>:null}{item.data.state==='applied'&&item.data.child_run_id&&client.runOutputs?<SemanticPreview key={item.data.child_run_id} client={client as Pick<ApiClient,'runOutputs'>} tenant={tenant} run={item.data.child_run_id}/>:null}</View></View>}/>{cursor?<Pressable accessibilityRole="button" disabled={busy} onPress={()=>void load(cursor)} style={s.button}><Text style={s.buttonText}>{t('semantic_results_more')}</Text></Pressable>:null}</SafeAreaView>;
}
