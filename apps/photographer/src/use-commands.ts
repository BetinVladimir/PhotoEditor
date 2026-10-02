import {useEffect,useRef} from 'react';
import {Platform} from 'react-native';
import {matchCommand,type CommandID} from './commands';
export function useCommands(handlers:Partial<Record<CommandID,()=>void>>){
 const current=useRef(handlers);current.current=handlers;
 useEffect(()=>{
  if(Platform.OS!=='web'||typeof document==='undefined')return;
  const handle=(event:KeyboardEvent)=>{
   const target=event.target;
   const editing=target instanceof HTMLElement&&(target.isContentEditable||!!target.closest('input,textarea,select,[contenteditable="true"],[role="textbox"]'));
   const id=matchCommand({key:event.key,ctrlKey:event.ctrlKey,metaKey:event.metaKey,altKey:event.altKey,shiftKey:event.shiftKey,repeat:event.repeat,isComposing:event.isComposing,defaultPrevented:event.defaultPrevented,editing});const action=id&&current.current[id];
   if(!action)return;
   event.preventDefault();action();
  };
  document.addEventListener('keydown',handle);
  return()=>document.removeEventListener('keydown',handle);
 },[]);
}
