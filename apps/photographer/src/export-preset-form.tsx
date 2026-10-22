import {useLocale} from './locale-context';
import {Button,Text,TextInput,TouchableRipple} from 'react-native-paper';
import {View} from 'react-native';
import type {ExportPresetConfig} from './catalog-types';

export function ExportPresetForm({value,onChange,disabled}:{value:ExportPresetConfig;onChange:(value:ExportPresetConfig)=>void;disabled:boolean}){
 const __displayT=useLocale().t;

 const __catalogT=useLocale().t;
 const __optionValue=useLocale().value;

 const __uiT=useLocale().t;

 function patch(fields:Partial<ExportPresetConfig>){if(!disabled)onChange({...value,...fields})}
 function choice<T extends string>(label:string,values:readonly T[],selected:T,change:(value:T)=>void){return <View><Text>{label}</Text>{values.map(item=><TouchableRipple key={item} accessibilityRole="radio" accessibilityLabel={__uiT('ui_07e3a63ea0ed', {p0:(label),p1:(__optionValue(item))})} accessibilityState={{checked:selected===item,disabled}} aria-checked={selected===item} disabled={disabled} onPress={()=>change(item)}><Text>{selected===item?'◉':'○'} {__optionValue(item)}</Text></TouchableRipple>)}</View>}
 function number(label:string,numberValue:number|undefined,update:(number:number)=>void,min:number,max:number){return <View><Text>{label}</Text><TextInput accessibilityLabel={label} editable={!disabled} keyboardType="decimal-pad" value={numberValue===undefined?'':String(numberValue)} onChangeText={raw=>{if(!raw.trim())return;const next=Number(raw);if(Number.isFinite(next)&&next>=min&&next<=max)update(next)}}/></View>}
 function toggle(label:string,enabled:boolean,update:()=>void){return <TouchableRipple accessibilityRole="checkbox" accessibilityLabel={label} accessibilityState={{checked:enabled,disabled}} aria-checked={enabled} disabled={disabled} onPress={update}><Text>{enabled?'☑':'☐'} {label}</Text></TouchableRipple>}
 return <View>
 {choice(__catalogT('ui_2f343666aaa8'),['jpeg','png','tiff'] as const,value.format,format=>patch({format,quality:format==='jpeg'?(value.quality??92):undefined,alpha:format==='jpeg'?'matte':value.alpha,matte_rgb:format==='jpeg'?(value.matte_rgb??[1,1,1]):value.matte_rgb}))}
 {choice(__catalogT('ui_dacfe1588e8d'),['srgb_v1','work_rec2020_gamma24_v1'] as const,value.icc_id,icc_id=>patch({icc_id}))}
 {value.format==='jpeg'?number(__catalogT('ui_9c642cd8d8d1'),value.quality,quality=>patch({quality:Math.round(quality)}),1,100):null}
 {value.format!=='jpeg'?choice(__catalogT('ui_8b773803e66e'),['preserve','matte'] as const,value.alpha,alpha=>patch({alpha,matte_rgb:alpha==='matte'?[1,1,1]:undefined})):null}
 {value.alpha==='matte'?[0,1,2].map(channel=><View key={channel}>{number([__displayT('ui_a105b4a80ab6'),__displayT('ui_424dbbc8c9ab'),__displayT('ui_80a194628af9')][channel],value.matte_rgb?.[channel]??1,next=>{const matte=[...(value.matte_rgb??[1,1,1])];matte[channel]=next;patch({matte_rgb:matte})},0,1)}</View>):null}
 {toggle(__catalogT('ui_2956e06ac065'),!!value.size,()=>patch({size:value.size?undefined:{long_edge:2400,fit:'contain',allow_upscale:false}}))}
 {value.size?<View>{choice(__catalogT('ui_c23ea3b40e80'),['long_edge','width_height'] as const,value.size.long_edge!==undefined?'long_edge':'width_height',mode=>patch({size:mode==='long_edge'?{long_edge:2400,fit:'contain',allow_upscale:value.size!.allow_upscale}:{width:2400,height:1600,fit:'contain',allow_upscale:value.size!.allow_upscale}}))}{value.size.long_edge!==undefined?number(__catalogT('ui_fa3a083dc63e'),value.size.long_edge,next=>patch({size:{long_edge:Math.round(next),fit:'contain',allow_upscale:value.size!.allow_upscale}}),1,100000):<View>{number(__catalogT('ui_1b484ac20b75'),value.size.width,next=>patch({size:{...value.size!,width:Math.round(next)}}),1,100000)}{number(__catalogT('ui_1bff65402604'),value.size.height,next=>patch({size:{...value.size!,height:Math.round(next)}}),1,100000)}</View>}{toggle(__catalogT('ui_afe6e61c553f'),value.size.allow_upscale,()=>patch({size:{...value.size!,allow_upscale:!value.size!.allow_upscale}}))}</View>:null}
 <Text>{__uiT('ui_82c1d898eb44')}</Text><TextInput accessibilityLabel={__uiT('ui_82c1d898eb44')} editable={!disabled} value={value.naming_template} onChangeText={naming_template=>patch({naming_template})}/><Text>{__uiT('ui_6138798db820')}</Text>
 {value.watermark?<View>{number(__catalogT('ui_d101b50a14cc'),value.watermark.opacity,opacity=>patch({watermark:{...value.watermark!,opacity}}),0,1)}{choice(__catalogT('ui_0bc19b1c6618'),['top_left','top_right','bottom_left','bottom_right','center'] as const,value.watermark.anchor,anchor=>patch({watermark:{...value.watermark!,anchor}}))}{number(__catalogT('ui_f0672d6f6cdd'),value.watermark.margin_x,margin_x=>patch({watermark:{...value.watermark!,margin_x:Math.round(margin_x)}}),0,100000)}{number(__catalogT('ui_5cd03d2fd4c5'),value.watermark.margin_y,margin_y=>patch({watermark:{...value.watermark!,margin_y:Math.round(margin_y)}}),0,100000)}</View>:null}
 {toggle(__catalogT('ui_f5925b3e7ff4'),!!value.sharpen,()=>patch({sharpen:value.sharpen?undefined:{radius:1,strength:.2,threshold:.01}}))}
 {value.sharpen?<View>{number(__catalogT('ui_eb90acdba9ee'),value.sharpen.radius,radius=>patch({sharpen:{...value.sharpen!,radius}}),.1,5)}{number(__catalogT('ui_d5cff67aeaba'),value.sharpen.strength,strength=>patch({sharpen:{...value.sharpen!,strength}}),0,1)}{number(__catalogT('ui_6cf9bda86bc1'),value.sharpen.threshold,threshold=>patch({sharpen:{...value.sharpen!,threshold}}),0,.1)}</View>:null}
 </View>;
}
