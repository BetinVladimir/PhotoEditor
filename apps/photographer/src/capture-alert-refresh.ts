import type {CaptureAlert} from './capture-types';
import type {Page} from './catalog-types';

export async function readCaptureAlertDelta(
 fetchPage:(cursor:string|undefined,signal:AbortSignal)=>Promise<Page<CaptureAlert>>,
 displayed:readonly CaptureAlert[],signal:AbortSignal,
):Promise<{items:CaptureAlert[];initialCursor:string|null}>{
 // Continue through the oldest loaded row, so updates on older pages are read.
 const oldest=displayed.length?displayed.reduce((a,b)=>a.created_at&&b.created_at?(a.created_at<b.created_at||a.created_at===b.created_at&&a.id<b.id?a:b):b).id:undefined;
 const items:CaptureAlert[]=[];const visited=new Set<string>();
 let next:string|undefined;let initialCursor:string|null=null;
 do{
  signal.throwIfAborted();
  const page=await fetchPage(next,signal);
  signal.throwIfAborted();
  items.push(...page.items);
  if(next===undefined)initialCursor=page.next_cursor??null;
  if(oldest&&page.items.some(alert=>alert.id===oldest))break;
  next=page.next_cursor??undefined;
  if(next){if(visited.has(next))throw new Error('Repeated capture cursor');visited.add(next)}
 }while(next&&displayed.length>0);
 return {items,initialCursor};
}
