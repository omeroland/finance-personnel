import { registerLocaleData } from '@angular/common';
import { provideHttpClient, withInterceptors } from '@angular/common/http';
import localeFrCA from '@angular/common/locales/fr-CA';
import {
  ApplicationConfig,
  LOCALE_ID,
  inject,
  provideAppInitializer,
  provideBrowserGlobalErrorListeners,
} from '@angular/core';
import { provideRouter } from '@angular/router';
import { authInterceptor, provideAuth, withAppInitializerAuthCheck } from 'angular-auth-oidc-client';
import { BASE_PATH } from './api';
import { AppConfigService } from './core/app-config';
import { stsLoaderProvider } from './core/auth';
import { routes } from './app.routes';

registerLocaleData(localeFrCA);

export const appConfig: ApplicationConfig = {
  providers: [
    provideBrowserGlobalErrorListeners(),
    provideRouter(routes),
    { provide: LOCALE_ID, useValue: 'fr-CA' },
    // La configuration d'exécution est chargée avant tout le reste (OIDC, URL de l'API).
    provideAppInitializer(() => inject(AppConfigService).load().then(() => undefined)),
    { provide: BASE_PATH, useFactory: () => inject(AppConfigService).apiBase },
    provideHttpClient(withInterceptors([authInterceptor()])),
    provideAuth({ loader: stsLoaderProvider }, withAppInitializerAuthCheck()),
  ],
};
