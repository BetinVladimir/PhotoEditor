import {useEffect,useRef,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {Review} from './catalog-types';
import {APIError} from '@photoeditor/api-client';
import {readRelightAnalysisWorkflow} from './relight-analysis-progress';

type Props={api:OwnerAPI;run:string;workflow:string;onOpen:(review:string)=>void};
type Page={items:Review[];next_cursor?:string|null};
const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;

export function readRelightRunReviewPage(value:unknown,run:string,seen:ReadonlySet<string>,usedCursors:ReadonlySet<string>,requested?:string):Page{
 if(!value||typeof value!=='object'||Array.isArray(value))throw new Error('RELIGHT_REVIEWS_INVALID');
 const page=value as Record<string,unknown>;
 if(!Array.isArray(page.items)||page.items.length>100||!(page.next_cursor===undefined||page.next_cursor===null||typeof page.next_cursor==='string'&&page.next_cursor.length>0&&page.next_cursor.length<=4096&&page.items.length>0&&page.next_cursor!==requested&&!usedCursors.has(page.next_cursor)))throw new Error('RELIGHT_REVIEWS_INVALID');
 const ids=new Set(seen);
 for(const item of page.items){
  if(!item||typeof item!=='object'||Array.isArray(item))throw new Error('RELIGHT_REVIEWS_INVALID');
  const review=item as Record<string,unknown>;
  if(!uuid.test(String(review.id))||review.run_id!==run||ids.has(String(review.id))||typeof review.kind!=='string'||typeof review.state!=='string')throw new Error('RELIGHT_REVIEWS_SCOPE_INVALID');
  ids.add(String(review.id));
 }
 return {items:page.items as Review[],next_cursor:page.next_cursor as string|null|undefined};
}

export function RelightRunReviews({api,run,workflow,onOpen}:Props){
 const[items,setItems]=useState<Review[]>([]),[cursor,setCursor]=useState<string|null>(null),[busy,setBusy]=useState(true),[failed,setFailed]=useState(false),[epoch,setEpoch]=useState(0);
 const loadPage=useRef<((cursor?:string)=>Promise<void>)|null>(null);
 useEffect(()=>{
  let active=true,controller:AbortController|null=null;
  const seen=new Set<string>(),usedCursors=new Set<string>();
  setItems([]);setCursor(null);setBusy(true);setFailed(false);
  async function load(next?:string){
   if(!active||controller)return;
   controller=new AbortController();const signal=controller.signal;
   setBusy(true);
   try{
    if(!next){
     if(!uuid.test(run)||!uuid.test(workflow))throw new Error('RELIGHT_REVIEW_SCOPE_INVALID');
     const status=await api.get<unknown>(`/api/v1/tenants/{t}/v2-workflows/${encodeURIComponent(workflow)}`,signal);
     readRelightAnalysisWorkflow(status,{run_id:run,workflow_id:workflow,state:'queued'},null);
    }
    const page=readRelightRunReviewPage(await api.reviewsForRun(run,next,signal),run,seen,usedCursors,next);
    if(!active||signal.aborted)return;
    if(next)usedCursors.add(next);
    for(const item of page.items)seen.add(item.id);
    setItems(old=>next?[...old,...page.items]:page.items);
    setCursor(page.next_cursor||null);setFailed(false);
   }catch(error){if(active&&!signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status))setItems([]);setFailed(true);setCursor(null)}}
   finally{controller=null;if(active)setBusy(false)}
  }
  void load();
  loadPage.current=load;
  return()=>{active=false;controller?.abort();loadPage.current=null};
 },[api,run,workflow,epoch]);
 return <View>{items.map(item=><Pressable key={item.id} accessibilityRole="button" onPress={()=>onOpen(item.id)}><Text>{item.kind} · {item.state}</Text></Pressable>)}
  {cursor?<Pressable accessibilityRole="button" disabled={busy} onPress={()=>void loadPage.current?.(cursor)}><Text>Load more reviews</Text></Pressable>:null}
  {failed?<Pressable accessibilityRole="button" onPress={()=>setEpoch(value=>value+1)}><Text>Unable to load reviews. Retry</Text></Pressable>:null}
  {!failed&&!busy?<Pressable accessibilityRole="button" onPress={()=>setEpoch(value=>value+1)}><Text>Refresh reviews</Text></Pressable>:null}
  {busy?<Text accessibilityLiveRegion="polite">Loading reviews…</Text>:null}</View>;
}
