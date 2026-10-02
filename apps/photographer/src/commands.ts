/** Shared web command bindings. Native actions use the same command IDs. */
export const commands = {
 'capture.refresh': {label:'capture_refresh', key:'r', shift:true, allowInText:true},
 'capture.save': {label:'capture_save', key:'s', shift:false, allowInText:true},
 'compare.previous': {label:'compare_previous',key:'arrowleft',shift:false,allowInText:false},
 'compare.next': {label:'compare_next',key:'arrowright',shift:false,allowInText:false},
 'compare.accept': {label:'compare_accept',key:'enter',shift:false,allowInText:false},
 'compare.reject': {label:'compare_reject',key:'backspace',shift:false,allowInText:false},
} as const;
export type CommandID = keyof typeof commands;
export interface CommandKeyEvent {
 key:string;ctrlKey:boolean;metaKey:boolean;altKey:boolean;shiftKey:boolean;
 editing?:boolean;
 repeat:boolean;isComposing:boolean;defaultPrevented:boolean;
}
export function matchCommand(event:CommandKeyEvent):CommandID|undefined {
 if(event.defaultPrevented||event.repeat||event.isComposing||event.altKey||!(event.ctrlKey||event.metaKey))return;
 return (Object.keys(commands) as CommandID[]).find(id=>{
  const command=commands[id];if(event.editing&&!command.allowInText)return false;return event.key.toLowerCase()===command.key&&event.shiftKey===command.shift;
 });
}
