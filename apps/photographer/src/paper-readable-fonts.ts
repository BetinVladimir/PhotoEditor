import type {MD3Theme} from 'react-native-paper';
type Fonts=MD3Theme['fonts'];
type ReadableFonts=Fonts & {default:Fonts['default'] & {fontSize:number;lineHeight:number}};
/** Preserve platform font families/weights/spacing; do not disable OS font scaling. */
export function photographerReadableFonts(fonts:Fonts):ReadableFonts {
 const role=(font:Fonts['bodyLarge'],size:number,height:number)=>({...font,fontSize:Math.max(size,font.fontSize),lineHeight:Math.max(height,font.lineHeight)});
 return {...fonts,
  default:{...fonts.default,fontSize:16,lineHeight:24},
  bodyLarge:role(fonts.bodyLarge,16,24),bodyMedium:role(fonts.bodyMedium,16,24),
  bodySmall:role(fonts.bodySmall,14,20),
  labelLarge:role(fonts.labelLarge,14,20),labelMedium:role(fonts.labelMedium,14,20),labelSmall:role(fonts.labelSmall,14,20),
 };
}
