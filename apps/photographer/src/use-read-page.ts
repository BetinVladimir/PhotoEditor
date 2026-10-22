import {useEffect,useRef,useState} from 'react';
import type {Page} from './catalog-types';
import {useReadResource} from './use-read-resource';
import {readDeadline} from './read-deadline';
export function useReadPage<T extends {id:string}>(scope:string,connection:unknown,read:(cursor:string|undefined,signal:AbortSignal)=>Promise<Page<T>>){
 const reader=useRef(read);reader.current=read;
 function validate(page:Page<T>){if(!page||(page.revision!==undefined&&(typeof page.revision!=='string'||!page.revision))||!Array.isArray(page.items)||page.items.some(item=>!item||typeof item.id!=='string'||!item.id)||new Set(page.items.map(item=>item.id)).size!==page.items.length||(page.next_cursor!==undefined&&page.next_cursor!==null&&(typeof page.next_cursor!=='string'||!page.next_cursor||page.next_cursor.length>4096)))throw new Error('READ_PAGE_INVALID');return page;}
 const resource=useReadResource(scope,connection,async signal=>validate(await reader.current(undefined,signal)));
 const cursors=useRef(new Set<string>());
 const latest=useRef(resource.data);latest.current=resource.data;
 const flight=useRef<AbortController|null>(null),[morePhase,setMorePhase]=useState<'idle'|'loading'|'error'|'cancelled'>('idle');
 const ownership=useRef({scope,connection});ownership.current={scope,connection};
 useEffect(()=>{flight.current?.abort();flight.current=null;setMorePhase('idle');cursors.current.clear();return()=>{flight.current?.abort();flight.current=null;};},[scope,connection,resource.phase]);
 async function more(){
  const page=resource.data,cursor=page?.next_cursor;if(!page||!cursor||resource.phase!=='ready'||flight.current||!resource.current()||ownership.current.scope!==scope||ownership.current.connection!==connection||latest.current!==page)return;
  const controller=new AbortController();flight.current=controller;setMorePhase('loading');
  const own=()=>!controller.signal.aborted&&flight.current===controller&&resource.current()&&ownership.current.scope===scope&&ownership.current.connection===connection&&latest.current===page;
  try{
   const next=validate(await readDeadline(controller.signal,signal=>reader.current(cursor,signal)));if(!own())return;
   const ids=new Set(page.items.map(item=>item.id));
   if(next.revision!==page.revision||cursors.current.has(cursor)||next.next_cursor===cursor||(next.next_cursor&&cursors.current.has(next.next_cursor))||next.items.some(item=>ids.has(item.id))||new Set(next.items.map(item=>item.id)).size!==next.items.length)throw new Error('READ_PAGE_INVALID');
   cursors.current.add(cursor);resource.commit({...next,items:[...page.items,...next.items]});setMorePhase('idle');
  }catch{if(own())setMorePhase('error');}
  finally{if(flight.current===controller){flight.current=null;if(!controller.signal.aborted&&resource.current())setMorePhase(previous=>previous==='loading'?'idle':previous);}}
 }
 return{...resource,more,morePhase,hasMore:!!resource.data?.next_cursor,cancelMore:()=>{if(latest.current!==resource.data||!resource.current()||ownership.current.scope!==scope||ownership.current.connection!==connection)return;flight.current?.abort();flight.current=null;if(resource.current())setMorePhase('cancelled');}};
}
