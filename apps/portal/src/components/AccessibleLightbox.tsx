"use client";
import {useLayoutEffect,useRef,type ReactNode} from 'react';

function usable(element:HTMLElement){
 if(!element.isConnected||element.closest('[inert],[hidden]')||element.matches(':disabled'))return false;
 for(let node:HTMLElement|null=element;node;node=node.parentElement){const style=getComputedStyle(node);if(style.display==='none'||style.visibility==='hidden'||style.visibility==='collapse')return false;}
 return true;
}
export function AccessibleLightbox({name,onClose,restoreFallback,children}:{name:string;onClose:()=>void;restoreFallback:()=>HTMLElement|null;children:ReactNode}){
 const ref=useRef<HTMLDivElement>(null);const latest=useRef({onClose,restoreFallback});latest.current={onClose,restoreFallback};
 useLayoutEffect(()=>{
  const dialog=ref.current;if(!dialog)return;
  const opener=document.activeElement instanceof HTMLElement?document.activeElement:null;
  const hidden=new Map<Element,{inert:string|null;aria:string|null}>();
  const isolate=()=>{
   let branch:Element=dialog;
   while(branch.parentElement){
    for(const sibling of Array.from(branch.parentElement.children)){
     if(sibling===branch||hidden.has(sibling))continue;
     hidden.set(sibling,{inert:sibling.getAttribute('inert'),aria:sibling.getAttribute('aria-hidden')});
     sibling.setAttribute('inert','');sibling.setAttribute('aria-hidden','true');
    }
    branch=branch.parentElement;if(branch===document.body)break;
   }
  };
  const controls=()=>Array.from(dialog.querySelectorAll<HTMLElement>('button,a[href],input,select,textarea,[tabindex]')).filter(el=>el.tabIndex>=0&&usable(el));
  const enter=()=>{const first=controls()[0];(first??dialog).focus();};
  isolate();enter();
  const mutations=new MutationObserver(isolate);mutations.observe(document.body,{childList:true,subtree:true});
  const key=(event:KeyboardEvent)=>{
   if(event.isComposing)return;
   if(event.key==='Escape'){event.preventDefault();event.stopPropagation();latest.current.onClose();return;}
   if(event.key!=='Tab'||event.ctrlKey||event.metaKey||event.altKey)return;
   const list=controls();const index=list.indexOf(document.activeElement as HTMLElement);
   event.preventDefault();if(!list.length){dialog.focus();return;}
   const next=index<0?(event.shiftKey?list.length-1:0):(index+(event.shiftKey?-1:1)+list.length)%list.length;list[next].focus();
  };
  const contain=(event:FocusEvent)=>{if(event.target instanceof Node&&!dialog.contains(event.target))enter();};
  document.addEventListener('keydown',key,true);document.addEventListener('focusin',contain,true);
  return()=>{
   mutations.disconnect();document.removeEventListener('keydown',key,true);document.removeEventListener('focusin',contain,true);
   for(const [element,prior]of hidden){for(const [attr,value]of [['inert',prior.inert],['aria-hidden',prior.aria]]as const){if(value===null)element.removeAttribute(attr);else element.setAttribute(attr,value);}}
   const target=opener&&usable(opener)?opener:latest.current.restoreFallback();if(target&&usable(target))target.focus();
  };
 },[]);
 return <div ref={ref} className="lightbox" role="dialog" aria-modal="true" aria-label={name} tabIndex={-1}>{children}</div>;
}
