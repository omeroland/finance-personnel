import { inject } from '@angular/core';
import { CanActivateFn } from '@angular/router';
import {
  LogLevel,
  OidcSecurityService,
  OpenIdConfiguration,
  StsConfigHttpLoader,
  StsConfigLoader,
} from 'angular-auth-oidc-client';
import { from, map, take } from 'rxjs';
import { AppConfigService } from './app-config';

/**
 * Code d'autorisation + PKCE (S256 : la bibliothèque l'impose, le client Keycloak l'exige).
 * Les jetons restent dans sessionStorage (défaut de la bibliothèque), jamais dans localStorage.
 */
export async function oidcConfig(cfg: AppConfigService): Promise<OpenIdConfiguration> {
  const c = await cfg.load();
  const origine = globalThis.location.origin;
  return {
    authority: c.oidc.issuer,
    clientId: c.oidc.clientId,
    redirectUrl: origine,
    postLogoutRedirectUri: origine,
    responseType: 'code',
    scope: 'openid profile',
    silentRenew: true,
    useRefreshToken: true,
    disableRefreshTokenOfflineAccessScopeWarning: true,
    renewTimeBeforeTokenExpiresInSeconds: 30,
    secureRoutes: [`${cfg.apiBase}/api/`],
    logLevel: LogLevel.Warn,
  };
}

/** Fournisseur du chargeur de configuration OIDC, alimenté par config.json. */
export const stsLoaderProvider = {
  provide: StsConfigLoader,
  useFactory: () => {
    const cfg = inject(AppConfigService);
    return new StsConfigHttpLoader(from(oidcConfig(cfg)).pipe(map((c) => [c])));
  },
};

/** Redirige vers Keycloak quand la session n'est pas authentifiée. */
export const authGuard: CanActivateFn = () => {
  const oidc = inject(OidcSecurityService);
  return oidc.isAuthenticated$.pipe(
    take(1),
    map(({ isAuthenticated }) => {
      if (!isAuthenticated) {
        oidc.authorize();
      }
      return isAuthenticated;
    }),
  );
};
