import {LocaleProvider} from "../src/locale-context";
import { Stack } from "expo-router";
import { AuthProvider } from "../src/auth-context";
export default function RootLayout(){return <AuthProvider><LocaleProvider><Stack screenOptions={{headerShown:false,animation:"fade"}}/></LocaleProvider></AuthProvider>}
