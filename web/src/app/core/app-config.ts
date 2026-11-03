import { Injectable } from '@angular/core';

/** Configuration lue à l'exécution dans /config.json : la même image sert tous les environnements. */
export interface AppConfig {
  /** Origine de l'API ; vide = même origine que l'application. */
  apiUrl: string;
  oidc: { issuer: string; clientId: string };
}

export function validerConfig(brut: unknown): AppConfig {
  const c = brut as Partial<AppConfig> | null;
  if (
    !c ||
    typeof c.apiUrl !== 'string' ||
    typeof c.oidc?.issuer !== 'string' ||
    !c.oidc.issuer ||
    typeof c.oidc.clientId !== 'string' ||
    !c.oidc.clientId
  ) {
    throw new Error('config.json invalide');
  }
  return { apiUrl: c.apiUrl.replace(/\/+$/, ''), oidc: c.oidc };
}

@Injectable({ providedIn: 'root' })
export class AppConfigService {
  private promesse?: Promise<AppConfig>;
  private valeur?: AppConfig;

  /** Charge la configuration une seule fois. */
  load(): Promise<AppConfig> {
    this.promesse ??= fetch('config.json', { cache: 'no-store' })
      .then((r) => {
        if (!r.ok) {
          throw new Error(`config.json: HTTP ${r.status}`);
        }
        return r.json();
      })
      .then((brut) => (this.valeur = validerConfig(brut)));
    return this.promesse;
  }

  get config(): AppConfig {
    if (!this.valeur) {
      throw new Error('configuration non chargée');
    }
    return this.valeur;
  }

  /** Origine absolue de l'API (les routes sécurisées de l'intercepteur sont absolues). */
  get apiBase(): string {
    return this.config.apiUrl || globalThis.location.origin;
  }
}
