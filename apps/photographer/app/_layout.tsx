import {View} from 'react-native';
import {LocalePicker} from '../src/locale-picker';
import {photographerDarkTheme} from '../src/paper-theme';
import {PhotographerPaperProvider} from '../src/paper-provider';
import {WorkspaceAccessBoundary} from '../src/workspace-access-context';
import {LocaleProvider} from "../src/locale-context";
import { Stack } from "expo-router";
import { AuthProvider } from "../src/auth-context";
export default function RootLayout(){return <PhotographerPaperProvider><LocaleProvider><View style={{flex:1}}><LocalePicker/><AuthProvider><Stack screenOptions={{headerShown:false,animation:"fade",contentStyle:{backgroundColor:photographerDarkTheme.colors.background}}} screenLayout={({children,route})=>route.name==='[locale]'?children:<WorkspaceAccessBoundary routeName={route.name} params={route.params ? Object.fromEntries(Object.entries(route.params)) : undefined}>{children}</WorkspaceAccessBoundary>}/></AuthProvider></View></LocaleProvider></PhotographerPaperProvider>}
