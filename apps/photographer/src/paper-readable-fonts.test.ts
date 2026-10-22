import {describe, expect, it} from 'vitest';
import {MD3DarkTheme, MD3LightTheme} from 'react-native-paper';
import {photographerReadableFonts} from './paper-readable-fonts';

describe('readable Paper typography',()=>{
 for(const [name,theme] of [['dark',MD3DarkTheme],['light',MD3LightTheme]] as const){
  it(`keeps ${name} platform typography while enforcing readable minima`,()=>{
   const before=JSON.stringify(theme.fonts);
   const result=photographerReadableFonts(theme.fonts);
   expect(JSON.stringify(theme.fonts)).toBe(before);
   expect(result.default.fontSize).toBeGreaterThanOrEqual(16);
   for(const role of ['bodyLarge','bodyMedium','bodySmall','labelLarge','labelMedium','labelSmall'] as const){
    expect(result[role].fontSize).toBeGreaterThanOrEqual(role==='bodyLarge'||role==='bodyMedium'?16:14);
    expect(result[role].lineHeight).toBeGreaterThanOrEqual(result[role].fontSize);
    expect(result[role].fontFamily).toBe(theme.fonts[role].fontFamily);
    expect(result[role].fontWeight).toBe(theme.fonts[role].fontWeight);
    expect(result[role].letterSpacing).toBe(theme.fonts[role].letterSpacing);
   }
   expect(result.headlineLarge).toBe(theme.fonts.headlineLarge);
   expect(result.displayLarge).toBe(theme.fonts.displayLarge);
  });
 }
 it('preserves an already larger body role',()=>{
  const fonts={...MD3DarkTheme.fonts,bodyMedium:{...MD3DarkTheme.fonts.bodyMedium,fontSize:22,lineHeight:32}};
  const result=photographerReadableFonts(fonts);
  expect(result.bodyMedium.fontSize).toBe(22);
  expect(result.bodyMedium.lineHeight).toBe(32);
 });
});
