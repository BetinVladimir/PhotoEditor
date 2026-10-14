import {MultiReplacementLinks} from "../../src/multi-replacement-links";
import {PerspectiveReplacementLinks} from "../../src/perspective-replacement-links";
import type {NegativeResultSelection} from '../../src/negative-result-selection';
import {isRestorationReview} from '../../src/review-binding';
import {loadReviewPreview} from '../../src/review-preview';
import {prepareReviewPreviewDisplay,prepareReviewPreviewPair} from '../../src/review-preview-display';
import{useEffect,useMemo,useRef,useState}from"react";
import{router,useLocalSearchParams}from"expo-router";
import{Image,Linking,Pressable,SafeAreaView,ScrollView,Text,View}from"react-native";
import{useAuth}from"../../src/auth-context";
import{OwnerAPI}from"../../src/owner-api";
import type{Review,RunOutput}from"../../src/catalog-types";
import{catalogStyle as s}from"../../src/catalog-style";

const candidateReviewView=(value:Review)=>value.kind==='render.compose'||value.kind==='optical.negative'||value.kind==='portrait.open_eyes'||isRestorationReview(value.kind);
const selectedV28Review=(kind:string)=>kind==='portrait.open_eyes'||isRestorationReview(kind);

export default function ReviewDetail(){
 const{id,tenant}=useLocalSearchParams<{id:string;tenant:string}>();
 return <ReviewDetailScope key={`${tenant}:${id}`} id={id} tenant={tenant}/>;
}
function ReviewDetailScope({id,tenant}:{id:string;tenant:string}){
 const auth=useAuth(),api=useMemo(()=>new OwnerAPI(auth.client(),tenant||""),[auth,tenant]);
 const[item,setItem]=useState<Review|undefined>(),[outputs,setOutputs]=useState<RunOutput[]>([]),[selected,setSelected]=useState<string[]>([]),[busy,setBusy]=useState(false),[error,setError]=useState("");
 const[resultSelection,setResultSelection]=useState<NegativeResultSelection|undefined>();
 const[reload,setReload]=useState(0);
 const[pair,setPair]=useState<Array<{uri:string;width:number;height:number}>>([]);
 const[selectedPreview,setSelectedPreview]=useState<{uri:string;width:number;height:number;side:'before'|'after'}|undefined>();
 const active=useRef(true),submitting=useRef(false);
 useEffect(()=>{active.current=true;return()=>{active.current=false}},[]);
 useEffect(()=>{const controller=new AbortController();setItem(undefined);setResultSelection(undefined);setOutputs([]);setPair([]);setSelectedPreview(undefined);setSelected([]);setError("");if(!id)return()=>controller.abort();
 async function load(){try{const value=await api.review(id,controller.signal);if(controller.signal.aborted)return;if(value.id!==id)throw new Error("Review scope mismatch");const candidateReview=candidateReviewView(value);const artifacts=candidateReview?{items:[] as RunOutput[]}:await api.runOutputs(value.run_id,controller.signal);if(controller.signal.aborted)return;
 if(value.state==='pending'&&candidateReview){
  const decode=(uri:string)=>new Promise<{width:number;height:number}>((resolve,reject)=>Image.getSize(uri,(width,height)=>resolve({width,height}),reject));
  const [before,after]=await Promise.all((['before','after']as const).map(side=>loadReviewPreview(api,value,side,controller.signal)));
  const displays=await prepareReviewPreviewPair(before,after,controller.signal,decode);
  if(controller.signal.aborted)return;
  const current=await api.review(id,controller.signal);
  if(controller.signal.aborted)return;
  if(current.id!==value.id||current.state!=='pending'||current.kind!==value.kind||current.run_id!==value.run_id||current.revision!==value.revision||current.candidate_hash!==value.candidate_hash||JSON.stringify(current.candidate)!==JSON.stringify(value.candidate)||JSON.stringify(current.base)!==JSON.stringify(value.base))throw new Error('Review changed');
  setPair(displays);
 }
 if((value.kind==='optical.negative'||value.kind==='render.compose'||selectedV28Review(value.kind))&&value.state==='decided'){
  const result=value.kind==='render.compose'?await api.renderResultSelection(value,controller.signal):selectedV28Review(value.kind)?await api.restorationResultSelection(value,controller.signal):await api.negativeResultSelection(value,controller.signal);if(controller.signal.aborted)return;setResultSelection(result);
  if((value.kind==='render.compose'||selectedV28Review(value.kind))&&result.image){
   const side=result.route==='base'?'before':'after';
   let display:{uri:string;width:number;height:number}|undefined;
   try{
    const decode=(uri:string)=>new Promise<{width:number;height:number}>((resolve,reject)=>Image.getSize(uri,(width,height)=>resolve({width,height}),reject));
    const preview=await loadReviewPreview({reviewPreview:(_id,_side,signal)=>selectedV28Review(value.kind)?api.restorationSelectedPreview(value,signal):api.renderSelectedPreview(value,signal)},value,side,controller.signal);
    if(controller.signal.aborted)return;
    if(preview.source.artifact_id!==result.image.artifact_id||preview.source.sha256!==result.image.sha256)throw new Error('Selected preview source changed');
    display=await prepareReviewPreviewDisplay(preview,controller.signal,decode);
   }catch{if(controller.signal.aborted)return}
   const current=selectedV28Review(value.kind)?await api.restorationResultSelection(value,controller.signal):await api.renderResultSelection(value,controller.signal);
   if(controller.signal.aborted)return;
   if(current.decision_hash!==result.decision_hash||current.route!==result.route||current.image?.artifact_id!==result.image.artifact_id||current.image?.sha256!==result.image.sha256)throw new Error('Selected preview decision changed');
   if(display)setSelectedPreview({...display,side});
  }
 }
 setItem(value);setSelected(value.candidate_ids||[]);setOutputs(artifacts.items)}catch{if(!controller.signal.aborted)setError("Review or artifacts are unavailable; access may have changed.")}}
 void load();return()=>controller.abort()},[api,id,reload]);
 async function decide(value:"accept"|"reject"|"skip"){if(!item||item.id!==id||item.state!=="pending"||submitting.current)return;submitting.current=true;setBusy(true);try{const all=item.candidate_ids||[],accepted=value==="accept"?selected:[],rejected=value==="accept"?all.filter(x=>!selected.includes(x)):value==="reject"?all:[];await api.decide(item,value,accepted,rejected);if(!active.current)return;router.replace({pathname:"/reviews",params:{tenant}})}catch{if(active.current)setError("Decision failed because the review changed or its candidate hash no longer matches.")}finally{submitting.current=false;if(active.current)setBusy(false)}}
 if(!item)return <SafeAreaView style={s.root}><View style={s.header}><Text accessibilityRole={error?"alert":undefined} style={error?s.error:s.subtitle}>{error||"Loading review and its before/after previews…"}</Text>{error?<Pressable accessibilityRole="button" onPress={()=>setReload(value=>value+1)} style={s.button}><Text style={s.buttonText}>Retry review previews</Text></Pressable>:null}</View></SafeAreaView>;
 const images=candidateReviewView(item)?[]:outputs.filter(value=>value.media_type.startsWith("image/"));
 return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={s.header}><Text style={s.title}>{item.kind} review</Text><Text style={s.subtitle}>Revision {item.revision}</Text><View style={s.row}><Text style={s.meta}>Candidate hash</Text><Text selectable style={s.meta}>{item.candidate_hash.slice(0,16)}…</Text></View>{resultSelection?<View accessibilityLabel="Selected review result"><Text style={s.name}>{resultSelection.route==='candidate'?'Candidate accepted':resultSelection.route==='base'?'Original retained':'Result skipped'}</Text>{resultSelection.image?<Text selectable style={s.meta}>{resultSelection.image.artifact_id} · {resultSelection.image.sha256.slice(0,16)}…</Text>:null}</View>:null}{selectedPreview?<View><Text style={s.name}>{selectedPreview.side==='before'?'Selected original':'Selected result'}</Text><Image accessibilityLabel={selectedPreview.side==='before'?'Selected original preview':'Selected result preview'} source={{uri:selectedPreview.uri}} resizeMode="contain" style={{width:"100%",aspectRatio:selectedPreview.width/selectedPreview.height}}/></View>:null}{item.kind==='optical.negative'&&resultSelection?.route==='base'?<PerspectiveReplacementLinks api={api} tenant={tenant} review={id} decisionHash={resultSelection.decision_hash} onOpen={(project,original)=>router.push({pathname:"/perspective-replacements/[original]",params:{tenant,project,review:id,original,decisionHash:resultSelection.decision_hash}})}/>:null}{item.kind==='optical.negative'&&resultSelection?.route==='base'?<MultiReplacementLinks api={api} tenant={tenant} review={id} decisionHash={resultSelection.decision_hash} onOpen={(project,original,operation)=>router.push({pathname:"/multi-replacements/[original]",params:{tenant,project,review:id,original,operation,decisionHash:resultSelection.decision_hash}})}/>:null}{selectedV28Review(item.kind)?<Text style={s.subtitle}>Review the verified before/after pair, then accept the result, retain the original, or skip it. This decision records the selected result; it does not start a separate Apply job.</Text>:null}<Text style={s.name}>Reasons</Text><Text selectable style={s.subtitle}>{JSON.stringify(item.reasons,null,2)}</Text>{(item.candidate_ids||[]).map((candidate,index)=>{const checked=selected.includes(candidate);return <Pressable accessibilityRole="checkbox" accessibilityState={{checked}} key={candidate} onPress={()=>setSelected(old=>checked?old.filter(x=>x!==candidate):[...old,candidate])} style={s.row}><Text style={s.name}>{checked?"☑":"☐"} Candidate {index+1}</Text><Text style={s.meta}>{candidate.slice(0,12)}…</Text></Pressable>})}{pair.map((image,index)=><View key={index}><Text style={s.name}>{index===0?"Before":"After"}</Text><Image accessibilityLabel={index===0?"Review before":"Review after"} source={{uri:image.uri}} resizeMode="contain" style={{width:"100%",aspectRatio:image.width/image.height}}/></View>)}{images.map((output,index)=><Image key={output.artifact_id} accessibilityLabel={`${output.role} candidate ${index+1}`} source={{uri:output.url}} resizeMode="contain" style={{width:"100%",aspectRatio:1,borderRadius:10}}/>)}{!candidateReviewView(item)?<><Text style={s.name}>Canonical run artifacts</Text>{outputs.map(output=><Pressable key={output.artifact_id} accessibilityHint="Opens the short-lived signed artifact" onPress={()=>void Linking.openURL(output.url)} style={s.row}><View><Text style={s.name}>{output.role}</Text><Text style={s.meta}>{output.media_type} · {output.bytes} bytes · {output.sha256.slice(0,12)}…</Text></View><Text style={s.chevron}>›</Text></Pressable>)}{outputs.length===0?<Text style={s.subtitle}>No signed candidate artifacts are currently available.</Text>:null}</>:null}{(["accept","reject","skip"]as const).map(x=><Pressable key={x} disabled={busy||item.state!=="pending"||item.id!==id} onPress={()=>void decide(x)} style={s.button}><Text style={s.buttonText}>{x[0].toUpperCase()+x.slice(1)}</Text></Pressable>)}{error?<Text accessibilityRole="alert" style={s.error}>{error}</Text>:null}</ScrollView></SafeAreaView>;
}
