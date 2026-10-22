// Native icon source is square; iOS and Android apply their own masks.
module.exports = ({ config }) => {
  const iosId = process.env.FRAMEVELA_IOS_BUNDLE_IDENTIFIER || config.ios?.bundleIdentifier || 'com.framevela.photographer';
  const androidId = process.env.FRAMEVELA_ANDROID_PACKAGE || config.android?.package || 'com.framevela.photographer';
  const projectId = process.env.FRAMEVELA_EAS_PROJECT_ID || config.extra?.eas?.projectId || 'dde18c43-5867-4de5-b023-f9a1b843d9dc';
  if (process.env.EAS_BUILD_PROFILE === 'store-beta') {
    if (!iosId || !androidId || !projectId) {
      throw new Error('Store beta requires confirmed FRAMEVELA_IOS_BUNDLE_IDENTIFIER, FRAMEVELA_ANDROID_PACKAGE and FRAMEVELA_EAS_PROJECT_ID.');
    }
    if (!/^[A-Za-z][A-Za-z0-9-]*(\.[A-Za-z0-9-]+)+$/.test(iosId) ||
        !/^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$/.test(androidId) ||
        !/^[0-9a-f]{8}(-[0-9a-f]{4}){3}-[0-9a-f]{12}$/i.test(projectId)) {
      throw new Error('Invalid store application or EAS project identifier.');
    }
  }
  return ({
  ...config,
  name: 'Framevela',
  slug: 'framevela',
  version: config.version || '1.0.0',
  owner: process.env.FRAMEVELA_EXPO_OWNER || 'beeloy',
  ...(projectId ? { extra: { ...config.extra, eas: { ...config.extra?.eas, projectId } } } : {}),
  icon: './assets/brand/icon.png',
  ios: {
    ...config.ios,
    appleTeamId: 'U6TTUVHSL6',
    ...(iosId ? { bundleIdentifier: iosId } : {}),
    icon: './assets/brand/icon.png',
  },
  android: {
    ...config.android,
    ...(androidId ? { package: androidId } : {}),
    icon: './assets/brand/icon.png',
    adaptiveIcon: {
      ...config.android?.adaptiveIcon,
      foregroundImage: './assets/brand/adaptive-foreground.png',
      monochromeImage: './assets/brand/adaptive-monochrome.png',
      backgroundColor: '#292C29',
    },
  },
  web: {
    ...config.web,
    name: 'Framevela',
    shortName: 'Framevela',
    favicon: './assets/brand/favicon.png',
  },
  });
};
