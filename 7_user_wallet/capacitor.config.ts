import { CapacitorConfig } from '@capacitor/cli';

const config: CapacitorConfig = {
  appId: 'com.yonacode.wallet',
  appName: 'YonaWallet',
  webDir: 'dist',
  server: {
    androidScheme: 'https'
  }
};

export default config;
