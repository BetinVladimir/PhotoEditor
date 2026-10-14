import {useEffect,useRef,useState} from 'react';
import {Pressable,Text,View} from 'react-native';
import type {OwnerAPI} from './owner-api';
import type {Review} from './catalog-types';
import {APIError} from '@photoeditor/api-client';

const uuid=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/;
type Page={items:Review[];next_cursor?:string|null};
type Props={api:OwnerAPI;run:string;onOpen:(review:string)=>void};

// The catalog applies tenant authorization; the client also refuses a page
// containing another run's reviews or a repeated cursor before navigation.
export function readRunReviewPage(value:unknown,run:string,seen:ReadonlySet<string>,usedCursors:ReadonlySet<string>,requested?:string):Page{
 if(!uuid.test(run)||!value||typeof value!=='object'||Array.isArray(value))throw new Error('RUN_REVIEWS_INVALID');
 const page=value as Record<string,unknown>;
 if(!Array.isArray(page.items)||page.items.length>100||!(page.next_cursor===undefined||page.next_cursor===null||typeof page.next_cursor==='string'&&page.next_cursor.length>0&&page.next_cursor.length<=4096&&page.items.length>0&&page.next_cursor!==requested&&!usedCursors.has(page.next_cursor)))throw new Error('RUN_REVIEWS_INVALID');
 const ids=new Set(seen);
 for(const item of page.items){
  if(!item||typeof item!=='object'||Array.isArray(item))throw new Error('RUN_REVIEWS_INVALID');
  const review=item as Record<string,unknown>;
  if(!uuid.test(String(review.id))||review.run_id!==run||ids.has(String(review.id))||typeof review.kind!=='string'||typeof review.state!=='string')throw new Error('RUN_REVIEWS_SCOPE_INVALID');
  ids.add(String(review.id));
 }
 return {items:page.items as Review[],next_cursor:page.next_cursor as string|null|undefined};
}

export function RunReviews({api,run,onOpen}:Props){
 const[items,setItems]=useState<Review[]>([]),[cursor,setCursor]=useState<string|null>(null),[busy,setBusy]=useState(true),[failed,setFailed]=useState(false),[epoch,setEpoch]=useState(0);
 const loadPage=useRef<((cursor?:string)=>Promise<void>)|null>(null);
 useEffect(()=>{
  let active=true,controller:AbortController|null=null;
  const seen=new Set<string>(),usedCursors=new Set<string>();
  setItems([]);setCursor(null);setBusy(true);setFailed(false);
  async function load(next?:string){
   if(!active||controller)return;
   controller=new AbortController();const signal=controller.signal;setBusy(true);
   try{
    const page=readRunReviewPage(await api.reviewsForRun(run,next,signal),run,seen,usedCursors,next);
    if(!active||signal.aborted)return;
    if(next)usedCursors.add(next);
    for(const item of page.items)seen.add(item.id);
    setItems(old=>next?[...old,...page.items]:page.items);
    setCursor(page.next_cursor||null);setFailed(false);
   }catch(error){if(active&&!signal.aborted){if(error instanceof APIError&&[401,403,404,410].includes(error.status))setItems([]);setFailed(true);setCursor(null);}}
   finally{controller=null;if(active)setBusy(false);}
  }
  void load();loadPage.current=load;
  return()=>{active=false;controller?.abort();loadPage.current=null;};
 },[api,run,epoch]);
 return <View><Text accessibilityRole="header">Run reviews</Text>
  {items.map(item=><Pressable key={item.id} accessibilityRole="button" onPress={()=>onOpen(item.id)}><Text>{item.kind} · {item.state}</Text></Pressable>)}
  {cursor?<Pressable accessibilityRole="button" disabled={busy} onPress={()=>void loadPage.current?.(cursor)}><Text>Load more reviews</Text></Pressable>:null}
  {failed?<Pressable accessibilityRole="button" onPress={()=>setEpoch(value=>value+1)}><Text>Unable to load reviews. Retry</Text></Pressable>:null}
  {!failed&&!busy?<Pressable accessibilityRole="button" onPress={()=>setEpoch(value=>value+1)}><Text>Refresh reviews</Text></Pressable>:null}
  {busy?<Text accessibilityLiveRegion="polite">Loading reviews…</Text>:null}
 </View>;
}
