import {photographerReadableFonts} from './paper-readable-fonts';
import {MD3DarkTheme, MD3LightTheme, type MD3Theme} from 'react-native-paper';

// Neutral image-surround colors keep photographs prominent. All ordinary
// controls use the same Material 3 roles rather than per-screen button colors.
export const photographerDarkTheme: MD3Theme = {
 ...MD3DarkTheme,
 fonts:photographerReadableFonts(MD3DarkTheme.fonts),
 roundness: 3,
 colors: {...MD3DarkTheme.colors, primary:'#d0bcff', onPrimary:'#381e72',
  background:'#222222', surface:'#292929', surfaceVariant:'#404047',
  onBackground:'#eeeeee', onSurface:'#eeeeee', outline:'#938f99'},
};
export const photographerLightTheme: MD3Theme = {
 ...MD3LightTheme,
 fonts:photographerReadableFonts(MD3LightTheme.fonts),
 roundness: 3,
 colors: {...MD3LightTheme.colors, background:'#f7f7fa', surface:'#ffffff'},
};
