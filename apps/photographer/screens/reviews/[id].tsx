import {useLocale} from '../../src/locale-context';
import {Button,TouchableRipple,Text} from 'react-native-paper';
import {readDeadline} from '../../src/read-deadline';
import {NegativeApplyAction} from '../../src/negative-apply-action';
import {ReflectionApplyAction} from '../../src/reflection-apply-action';
import {LocalPortraitApplyAction} from '../../src/local-portrait-apply-action';
import {OpenEyesApplyAction} from '../../src/open-eyes-apply-action';
import {GlareApplyAction} from '../../src/glare-apply-action';
import {CullingReviewSelection} from '../../src/culling-review-selection';
import {ReviewDecisionAttemptStore,type ReviewDecisionAttempt} from '../../src/review-decision-attempt-store';
import {reviewDecisionIntent,reviewDecisionReceipt,recoverReviewDecision} from '../../src/review-decision';
import {APIError} from '@photoeditor/api-client';
import {useReviewForeground} from '../../src/use-review-foreground';
import {RelightCollectionApply} from '../../src/relight-collection-apply';
import {RelightApplyAction} from '../../src/relight-apply-action';
import {pendingReviewImageToken} from '../../src/relight-viewed-receipt';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {RelightReviewDecisionAttemptStore,type RelightReviewDecisionAttempt} from '../../src/relight-review-decision-attempt-store';
import {relightReviewDecisionIntent,relightReviewDecisionReceipt,recoverRelightReviewDecision} from '../../src/relight-review-decision';
import {VerifiedReviewComparison} from '../../src/verified-review-comparison';
import {MultiReplacementLinks} from "../../src/multi-replacement-links";
import {PerspectiveReplacementLinks} from "../../src/perspective-replacement-links";
import type {NegativeResultSelection} from '../../src/negative-result-selection';
import {isRestorationReview} from '../../src/review-binding';
import {loadReviewPreview} from '../../src/review-preview';
import {prepareReviewPreviewDisplay,prepareReviewPreviewPair} from '../../src/review-preview-display';
import{useEffect,useMemo,useRef,useState}from"react";
import{router,useLocalSearchParams}from'../../src/localized-router';
import {Image,Linking,SafeAreaView,ScrollView,View} from 'react-native';
import{useAuth}from"../../src/auth-context";
import{OwnerAPI}from"../../src/owner-api";
import type{Review,RunOutput}from"../../src/catalog-types";
import{catalogStyle as s}from"../../src/catalog-style";

const localPortraitReview=(kind:string)=>kind==='portrait.hair'||kind==='portrait.clothing';
const candidateReviewView=(value:Review)=>localPortraitReview(value.kind)||value.kind==='render.compose'||value.kind==='optical.negative'||value.kind==='portrait.open_eyes'||isRestorationReview(value.kind);
const selectedV28Review=(kind:string)=>localPortraitReview(kind)||kind==='portrait.open_eyes'||isRestorationReview(kind);

export default function ReviewDetail(){
 const __uiT=useLocale().t;

 const{id,tenant}=useLocalSearchParams<{id:string;tenant:string}>();
 const auth=useAuth(),foreground=useReviewForeground();
 const identity=useRef({auth,generation:0});
 if(identity.current.auth!==auth)identity.current={auth,generation:identity.current.generation+1};
 if(!foreground.active)return <SafeAreaView style={s.root}><Text>{__uiT('ui_4a918b388907')}</Text></SafeAreaView>;
 return <ReviewDetailScope key={`${tenant}:${id}:${auth.origin}:${identity.current.generation}:${foreground.generation}`} id={id} tenant={tenant}/>;
}
function ReviewDetailScope({id,tenant}:{id:string;tenant:string}){
 const __displayValue=useLocale().value;

 const __r2Message=useLocale().message;const __r2T=useLocale().t;const __r2Value=useLocale().value;

 const __uiMessage=useLocale().message;

 const __uiT=useLocale().t;

 const auth=useAuth(),api=useMemo(()=>new OwnerAPI(auth.client(),tenant||""),[auth,tenant]);
 const[item,setItem]=useState<Review|undefined>(),[outputs,setOutputs]=useState<RunOutput[]>([]),[selected,setSelected]=useState<string[]>([]),[busy,setBusy]=useState(false),[error,setError]=useState("");
 const[resultSelection,setResultSelection]=useState<NegativeResultSelection|undefined>();
 const[reload,setReload]=useState(0);
 const[pair,setPair]=useState<Array<{uri:string;width:number;height:number}>>([]);
 const[relightViewed,setRelightViewed]=useState(false);
 const relightDisplay=useRef<{viewed:boolean;tokens:[string,string]|null}>({viewed:false,tokens:null});
 relightDisplay.current.viewed=relightViewed;
 const[selectedPreview,setSelectedPreview]=useState<{uri:string;width:number;height:number;side:'before'|'after'}|undefined>();
 const [decisionAttempt,setDecisionAttempt]=useState<RelightReviewDecisionAttempt|null>(null);
 const [otherAttempt,setOtherAttempt]=useState<ReviewDecisionAttempt|null>(null);
 const otherJournal=useRef<{store:ReviewDecisionAttemptStore;actor:string}|null>(null);
 const journal=useRef<{store:RelightReviewDecisionAttemptStore;actor:string}|null>(null);
 const decisionAbort=useRef<AbortController|null>(null),initialRead=useRef<AbortController|null>(null);
 const active=useRef(true),submitting=useRef(false);
 function revokeAccess(){
  decisionAbort.current?.abort();decisionAbort.current=null;submitting.current=false;journal.current=null;otherJournal.current=null;setOtherAttempt(null);relightDisplay.current={viewed:false,tokens:null};
  setItem(undefined);setOutputs([]);setPair([]);setSelectedPreview(undefined);setResultSelection(undefined);
  setSelected([]);setRelightViewed(false);setDecisionAttempt(null);setBusy(false);
  setError("Review or artifacts are unavailable; access may have changed.");
 }
 const accessLost=(error:unknown)=>error instanceof APIError&&[401,403,404,410].includes(error.status);
 useEffect(()=>{active.current=true;return()=>{active.current=false;decisionAbort.current?.abort()}},[]);
 useEffect(()=>{const controller=new AbortController();initialRead.current=controller;journal.current=null;otherJournal.current=null;setOtherAttempt(null);relightDisplay.current={viewed:false,tokens:null};setDecisionAttempt(null);decisionAbort.current?.abort();decisionAbort.current=null;submitting.current=false;setBusy(false);setItem(undefined);setResultSelection(undefined);setOutputs([]);setPair([]);setRelightViewed(false);setSelectedPreview(undefined);setSelected([]);setError("");if(!id)return()=>controller.abort();
 async function load(signal:AbortSignal){let pendingSelection:string[]|undefined;const value=await api.review(id,signal);if(signal.aborted)return;if(value.id!==id)throw new Error("Review scope mismatch");if(value.kind==='image.relight'){
  const user=await api.me(signal);if(signal.aborted)return;
  const store=new RelightReviewDecisionAttemptStore(AsyncStorage,auth.origin,user.id,tenant,id);
  const attempt=await store.read();if(signal.aborted)return;
  if(attempt){
   const receipt=relightReviewDecisionReceipt(value,attempt.body);
   if(receipt){await store.clear(attempt.key,receipt);if(signal.aborted)return;setDecisionAttempt(null)}
   else setDecisionAttempt(attempt);
  }else setDecisionAttempt(null);
  journal.current={store,actor:user.id};
 }else if(value.kind!=='culling'){
  const user=await api.me(signal);signal.throwIfAborted();
  const store=new ReviewDecisionAttemptStore(AsyncStorage,auth.origin,user.id,tenant,id);
  const attempt=await store.read();signal.throwIfAborted();
  if(attempt){const receipt=reviewDecisionReceipt(value,attempt.body);if(receipt){await store.clear(attempt.key,receipt);signal.throwIfAborted()}else{setOtherAttempt(attempt);pendingSelection=attempt.body.accepted}}
  otherJournal.current={store,actor:user.id};
 }
 const candidateReview=candidateReviewView(value);const artifacts=candidateReview||value.kind==='culling'?{items:[] as RunOutput[]}:await api.runOutputs(value.run_id,signal);if(signal.aborted)return;
 if(value.state==='pending'&&candidateReview){
  const decode=(uri:string)=>new Promise<{width:number;height:number}>((resolve,reject)=>Image.getSize(uri,(width,height)=>resolve({width,height}),reject));
  const [before,after]=await Promise.all((['before','after']as const).map(side=>loadReviewPreview(api,value,side,signal)));
  const displays=await prepareReviewPreviewPair(before,after,signal,decode);
  if(signal.aborted)return;
  const current=await api.review(id,signal);
  if(signal.aborted)return;
  if(current.id!==value.id||current.state!=='pending'||current.kind!==value.kind||current.run_id!==value.run_id||current.revision!==value.revision||current.candidate_hash!==value.candidate_hash||JSON.stringify(current.candidate)!==JSON.stringify(value.candidate)||JSON.stringify(current.base)!==JSON.stringify(value.base))throw new Error('Review changed');
  if(value.kind==='image.relight')relightDisplay.current.tokens=[pendingReviewImageToken(before.image.url,auth.origin),pendingReviewImageToken(after.image.url,auth.origin)];
  setPair(displays);
 }
 if((value.kind==='optical.negative'||value.kind==='render.compose'||selectedV28Review(value.kind))&&value.state==='decided'){
  const result=value.kind==='render.compose'?await api.renderResultSelection(value,signal):selectedV28Review(value.kind)?await api.restorationResultSelection(value,signal):await api.negativeResultSelection(value,signal);if(signal.aborted)return;setResultSelection(result);
  if((value.kind==='render.compose'||selectedV28Review(value.kind))&&result.image){
   const side=result.route==='base'?'before':'after';
   let display:{uri:string;width:number;height:number}|undefined;
   try{
    const decode=(uri:string)=>new Promise<{width:number;height:number}>((resolve,reject)=>Image.getSize(uri,(width,height)=>resolve({width,height}),reject));
    const preview=await loadReviewPreview({reviewPreview:(_id,_side,signal)=>selectedV28Review(value.kind)?api.restorationSelectedPreview(value,signal):api.renderSelectedPreview(value,signal)},value,side,signal);
    if(signal.aborted)return;
    if(preview.source.artifact_id!==result.image.artifact_id||preview.source.sha256!==result.image.sha256)throw new Error('Selected preview source changed');
    display=await prepareReviewPreviewDisplay(preview,signal,decode);
   }catch(error){if(signal.aborted)return;if(accessLost(error))throw error}
   const current=selectedV28Review(value.kind)?await api.restorationResultSelection(value,signal):await api.renderResultSelection(value,signal);
   if(signal.aborted)return;
   if(current.decision_hash!==result.decision_hash||current.route!==result.route||current.image?.artifact_id!==result.image.artifact_id||current.image?.sha256!==result.image.sha256)throw new Error('Selected preview decision changed');
   if(display)setSelectedPreview({...display,side});
  }
 }
 setItem(value);setSelected(pendingSelection??value.candidate_ids??[]);setOutputs(artifacts.items)}
 void readDeadline(controller.signal,load,15000).catch(()=>{if(!controller.signal.aborted)setError("Review or artifacts are unavailable; access may have changed.")}).finally(()=>{if(initialRead.current===controller)initialRead.current=null;});return()=>{controller.abort();if(initialRead.current===controller)initialRead.current=null;decisionAbort.current?.abort();journal.current=null;otherJournal.current=null}},[api,id,tenant,auth.origin,reload]);
 useEffect(()=>{if(!busy)return;const timer=setTimeout(cancelDecision,15000);return()=>clearTimeout(timer);},[busy]);
 function cancelDecision(){decisionAbort.current?.abort();decisionAbort.current=null;submitting.current=false;setBusy(false);setError('Decision is unresolved. Recover the saved choice; it cannot be replaced until the server confirms it.');}
 // Signed run artifacts must leave the UI at expiry even while the route stays open.
 useEffect(()=>{
  if(outputs.length===0)return;
  const deadlines=outputs.map(output=>Date.parse(output.expires_at));
  const expire=()=>{setOutputs([]);setError('Review artifact links expired. Reload the review to refresh access.')};
  if(deadlines.some(value=>!Number.isFinite(value))){expire();return}
  const remaining=Math.min(...deadlines)-Date.now();if(remaining<=0){expire();return}
  const timer=setTimeout(expire,Math.min(remaining,2147483647));return()=>clearTimeout(timer);
 },[outputs]);
 async function decide(value:"accept"|"reject"|"skip"){
  if(!item||item.kind==='culling'||item.id!==id||item.state!=="pending"||submitting.current||candidateReviewView(item)&&!relightViewed&&!decisionAttempt&&!otherAttempt)return;
  submitting.current=true;setBusy(true);setError("");const controller=new AbortController();decisionAbort.current=controller;
  try{
   if(item.kind==='image.relight'){
    const scope=journal.current;if(!scope)throw new Error('Decision journal unavailable');
    const assertActor=async()=>{
     controller.signal.throwIfAborted();
     const user=await api.me(controller.signal);controller.signal.throwIfAborted();
     if(journal.current!==scope||user.id!==scope.actor)throw new Error('Decision actor changed');
    };
    await assertActor();
    // Preserve the authored intent even if durable storage rejects its first write.
    const stored=await scope.store.read();controller.signal.throwIfAborted();
    const attempt=stored??decisionAttempt??{version:1 as const,key:globalThis.crypto.randomUUID(),body:relightReviewDecisionIntent(tenant,scope.actor,item,value)};
    if(active.current)setDecisionAttempt(attempt);
    if(attempt.body.decision!==value)throw new Error('Recover the saved decision first');
    await scope.store.write(attempt);controller.signal.throwIfAborted();
    const scopedAPI={
     review:async(reviewID:string,signal?:AbortSignal)=>{await assertActor();const result=await api.review(reviewID,signal);await assertActor();return result},
     decide:async(...args:Parameters<OwnerAPI['decide']>)=>{
      await assertActor();const display=relightDisplay.current;
      if(!display.viewed||!display.tokens)throw new Error('Relight pair must be displayed');
      const receipt=await api.acknowledgeRelightViewed(item,display.tokens[0],display.tokens[1],controller.signal);
      await assertActor();
      if(relightDisplay.current!==display||!display.viewed)throw new Error('Relight display changed');
      return api.decide(args[0],args[1],args[2],args[3],args[4],args[5],receipt.token);
     },
    };
    const receipt=await recoverRelightReviewDecision(scopedAPI,attempt.body,attempt.key,controller.signal);
    await assertActor();await scope.store.clear(attempt.key,receipt);controller.signal.throwIfAborted();
    if(active.current)setDecisionAttempt(null);
   }else{
    const scope=otherJournal.current;if(!scope)throw new Error('Decision journal unavailable');
    const assertActor=async()=>{controller.signal.throwIfAborted();const user=await api.me(controller.signal);controller.signal.throwIfAborted();if(otherJournal.current!==scope||user.id!==scope.actor)throw new Error('Decision actor changed')};
    await assertActor();const stored=await scope.store.read();controller.signal.throwIfAborted();
    const all=item.candidate_ids||[],accepted=value==='accept'?selected:[],rejected=value==='accept'?all.filter(x=>!selected.includes(x)):[];
    const attempt=stored??otherAttempt??{version:1 as const,key:globalThis.crypto.randomUUID(),body:reviewDecisionIntent(tenant,scope.actor,item,value,accepted,rejected)};
    if(active.current)setOtherAttempt(attempt);if(attempt.body.decision!==value)throw new Error('Recover saved decision first');
    await scope.store.write(attempt);controller.signal.throwIfAborted();
    const scopedAPI={review:async(reviewID:string,signal?:AbortSignal)=>{await assertActor();const result=await api.review(reviewID,signal);await assertActor();return result},decide:async(...args:Parameters<OwnerAPI['decide']>)=>{await assertActor();if(candidateReviewView(item)&&!relightDisplay.current.viewed)throw new Error('Review pair must be displayed');const result=await api.decide(...args);await assertActor();return result}};
    const receipt=await recoverReviewDecision(scopedAPI,attempt.body,attempt.key,controller.signal);await assertActor();await scope.store.clear(attempt.key,receipt);controller.signal.throwIfAborted();if(active.current)setOtherAttempt(null);
   }
   if(!active.current||controller.signal.aborted)return;router.replace({pathname:"/reviews",params:{tenant}});
  }catch(error){if(active.current&&accessLost(error)){revokeAccess();return}if(active.current&&!controller.signal.aborted)setError("Decision is unresolved. Recover the saved choice; it cannot be replaced until the server confirms it.")}
  finally{if(decisionAbort.current===controller){decisionAbort.current=null;submitting.current=false;if(active.current)setBusy(false)}}
 }

 const savedAttempt=decisionAttempt??otherAttempt;
 if(!item)return <SafeAreaView style={s.root}><View style={s.header}><Text accessibilityRole={error?"alert":undefined} style={error?s.error:s.subtitle}>{__r2Message(error,true)||__r2T('ui_59b0268eca29')}</Text>{!error?<Button accessibilityRole="button" onPress={()=>{initialRead.current?.abort();initialRead.current=null;setError("Review loading cancelled. Retry the review.");}}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_4ee3cfcc9e00')}</Button>:null}{error?<Button accessibilityRole="button" onPress={()=>setReload(value=>value+1)}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_5cb494a18c89')}</Button>:null}</View></SafeAreaView>;
 const images=candidateReviewView(item)?[]:outputs.filter(value=>value.media_type.startsWith("image/"));
 return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={s.header}><Text style={s.title}>{__r2Value(item.kind)} {__uiT('ui_c97ace4c8fef')}</Text><Text style={s.subtitle}>{__uiT('ui_2e516d68f42f')}{' '}{item.revision}</Text><View style={s.row}><Text style={s.meta}>{__uiT('ui_6f791e6285c3')}</Text><Text selectable style={s.meta}>{item.candidate_hash.slice(0,16)}…</Text></View>{resultSelection?<View accessibilityLabel={__uiT('ui_ce26887a2158')}><Text style={s.name}>{resultSelection.route==='candidate'?__uiT('ui_8774a3cd8231'):resultSelection.route==='base'?__uiT('ui_64a02ef188bb'):__uiT('ui_86045f41387f')}</Text>{resultSelection.image?<Text selectable style={s.meta}>{resultSelection.image.artifact_id} · {resultSelection.image.sha256.slice(0,16)}…</Text>:null}</View>:null}{selectedPreview?<View><Text style={s.name}>{selectedPreview.side==='before'?__uiT('ui_5809be22eb57'):__uiT('ui_4e8d54120368')}</Text><Image accessibilityLabel={selectedPreview.side==='before'?__uiT('ui_39bcf810237c'):__uiT('ui_4c311fa1950b')} source={{uri:selectedPreview.uri}} resizeMode="contain" style={{width:"100%",aspectRatio:selectedPreview.width/selectedPreview.height}}/></View>:null}{item.kind==='optical.negative'&&resultSelection?.route==='base'?<PerspectiveReplacementLinks api={api} tenant={tenant} review={id} decisionHash={resultSelection.decision_hash} onOpen={(project,original)=>router.push({pathname:"/perspective-replacements/[original]",params:{tenant,project,review:id,original,decisionHash:resultSelection.decision_hash}})}/>:null}{item.kind==='optical.negative'&&resultSelection?.route==='base'?<MultiReplacementLinks api={api} tenant={tenant} review={id} decisionHash={resultSelection.decision_hash} onOpen={(project,original,operation)=>router.push({pathname:"/multi-replacements/[original]",params:{tenant,project,review:id,original,operation,decisionHash:resultSelection.decision_hash}})}/>:null}{selectedV28Review(item.kind)?<Text style={s.subtitle}>{__uiT('ui_5b4731c1f342')}{' '}{(localPortraitReview(item.kind)||item.kind==='image.relight'||item.kind==='portrait.open_eyes'||item.kind==='portrait.glare'||item.kind==='image.reflection'||item.kind==='optical.negative')?__uiT('ui_630e9c01d259'):__uiT('ui_74d6f7d8ef63')}</Text>:null}{localPortraitReview(item.kind)&&resultSelection?.image?<LocalPortraitApplyAction api={api} review={item} onAccessDenied={revokeAccess} onOpen={value=>router.push({pathname:'/editor/[version]',params:{tenant,asset:value.asset_id,version:value.asset_version_id}})}/>:null}{item.kind==='portrait.open_eyes'&&resultSelection?.image?<OpenEyesApplyAction api={api} review={item} onAccessDenied={revokeAccess} onOpen={value=>router.push({pathname:'/editor/[version]',params:{tenant,asset:value.asset_id,version:value.asset_version_id}})}/>:null}{item.kind==='portrait.glare'&&resultSelection?.image?<GlareApplyAction api={api} review={item} onAccessDenied={revokeAccess} onOpen={value=>router.push({pathname:'/editor/[version]',params:{tenant,asset:value.asset_id,version:value.asset_version_id}})}/>:null}{item.kind==='image.relight'&&resultSelection?.image?<RelightApplyAction api={api} review={item} onAccessDenied={revokeAccess} onOpen={value=>router.push({pathname:'/editor/[version]',params:{tenant,asset:value.asset_id,version:value.asset_version_id}})}/>:null}{item.kind==='image.reflection'&&resultSelection?.image?<ReflectionApplyAction api={api} review={item} onAccessDenied={revokeAccess} onOpen={value=>router.push({pathname:'/editor/[version]',params:{tenant,asset:value.asset_id,version:value.asset_version_id}})}/>:null}{item.kind==='optical.negative'&&resultSelection?.image?<NegativeApplyAction api={api} review={item} onAccessDenied={revokeAccess} onOpen={value=>router.push({pathname:'/editor/[version]',params:{tenant,asset:value.asset_id,version:value.asset_version_id}})}/>:null}{item.kind==='image.relight'&&item.decision==='accept'&&resultSelection?.image?<RelightCollectionApply onAccessDenied={revokeAccess} api={api} origin={auth.origin} tenant={tenant} review={item} onOpen={value=>router.push({pathname:'/editor/[version]',params:{tenant,asset:value.asset,version:value.version,collection:value.collection,item:value.item}})}/>:null}{item.kind==='culling'?<CullingReviewSelection api={api} origin={auth.origin} tenant={tenant} review={item} onDecided={()=>setReload(value=>value+1)} onAccessDenied={revokeAccess}/>:null}<Text style={s.name}>{__uiT('ui_72ed245d2cf1')}</Text><Text selectable style={s.subtitle}>{JSON.stringify(item.reasons,null,2)}</Text>{(item.candidate_ids||[]).map((candidate,index)=>{const checked=selected.includes(candidate);return <TouchableRipple aria-checked={checked} accessibilityRole="checkbox" accessibilityState={{checked,disabled:busy||savedAttempt!==null}} disabled={busy||savedAttempt!==null} key={candidate} onPress={()=>setSelected(old=>checked?old.filter(x=>x!==candidate):[...old,candidate])} style={s.row}><View style={{flex:1,flexDirection:'row',alignItems:'center',justifyContent:'space-between'}}><Text style={s.name}>{checked?"☑":"☐"} {__uiT('ui_b2452d1eba32')}{' '}{index+1}</Text><Text style={s.meta}>{candidate.slice(0,12)}…</Text></View></TouchableRipple>})}{candidateReviewView(item)?<VerifiedReviewComparison images={pair} onViewed={setRelightViewed} onError={()=>setError("Unable to display review previews. Reload the review to refresh them.")}/>:(pair.map((image,index)=><View key={index}><Text style={s.name}>{index===0?__uiT('ui_9bb725005055'):__uiT('ui_7b68fe551085')}</Text><Image accessibilityLabel={index===0?__uiT('ui_201c26954a24'):__uiT('ui_a6b5ec20f096')} source={{uri:image.uri}} resizeMode="contain" style={{width:"100%",aspectRatio:image.width/image.height}}/></View>))}{images.map((output,index)=><Image key={output.artifact_id} accessibilityLabel={__uiT('ui_ba9ec0c48954', {p0:__displayValue((output.role)),p1:(index+1)})} source={{uri:output.url}} resizeMode="contain" style={{width:"100%",aspectRatio:1,borderRadius:10}}/>)}{!candidateReviewView(item)&&item.kind!=='culling'?<><Text style={s.name}>{__uiT('ui_edf122936d9d')}</Text>{outputs.map(output=><TouchableRipple key={output.artifact_id} accessibilityHint={__uiT('ui_7be36747f0dc')} onPress={()=>{if(Date.parse(output.expires_at)>Date.now())void Linking.openURL(output.url);else{setOutputs([]);setError('Review artifact links expired. Reload the review to refresh access.')}}} style={s.row}><View style={{flex:1,flexDirection:'row',alignItems:'center',justifyContent:'space-between'}}><View><Text style={s.name}>{__r2Value(output.role)}</Text><Text style={s.meta}>{output.media_type} · {output.bytes} {__uiT('ui_9ec239356ce9')}{' '}{output.sha256.slice(0,12)}…</Text></View><Text style={s.chevron}>›</Text></View></TouchableRipple>)}{outputs.length===0?<Text style={s.subtitle}>{__uiT('ui_7bdf66d0c0c9')}</Text>:null}</>:null}{savedAttempt&&item.state==='pending'?<View><Text style={s.subtitle}>{__uiT('ui_0fb4c94db809')}{' '}{__r2Value(savedAttempt.body.decision)}{__uiT('ui_d2baa0b0536b')}</Text><Button accessibilityRole="button" disabled={busy} onPress={()=>void decide(savedAttempt.body.decision)}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_8de77cf0b359')}</Button></View>:null}{item.kind!=='culling'&&(["accept","reject","skip"]as const).map(x=><Button accessibilityRole="button" key={x} disabled={busy||savedAttempt!==null||item.state!=="pending"||item.id!==id||candidateReviewView(item)&&!relightViewed} onPress={()=>void decide(x)}    mode="contained" contentStyle={{minHeight:48}}>{__r2Value(x)}</Button>)}{busy?<Button accessibilityRole="button" onPress={cancelDecision}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_39186190cc1c')}</Button>:null}{error?<View><Text accessibilityRole="alert" style={s.error}>{__uiMessage(error,true)}</Text><Button accessibilityRole="button" disabled={busy} onPress={()=>setReload(value=>value+1)}    mode="contained" contentStyle={{minHeight:48}}>{__uiT('ui_8a7df350c3ed')}</Button></View>:null}</ScrollView></SafeAreaView>;
}
