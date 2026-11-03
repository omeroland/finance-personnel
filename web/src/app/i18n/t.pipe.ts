import { Pipe, PipeTransform } from '@angular/core';
import frCA from './fr-CA.json';

export type CleTraduction = keyof typeof frCA;

/** Traduit une clé du fichier fr-CA ; `{{nom}}` est remplacé par les paramètres. */
export function traduire(cle: CleTraduction, params: Record<string, string> = {}): string {
  return Object.entries(params).reduce(
    (texte, [nom, valeur]) => texte.replaceAll(`{{${nom}}}`, valeur),
    frCA[cle] as string,
  );
}

@Pipe({ name: 't' })
export class TraductionPipe implements PipeTransform {
  transform(cle: CleTraduction, params?: Record<string, string>): string {
    return traduire(cle, params);
  }
}
