import { traduire } from './t.pipe';
import frCA from './fr-CA.json';

describe('traduire', () => {
  it('renvoie le texte fr-CA de la clé', () => {
    expect(traduire('menu.stockeval')).toBe("Évaluation d'actions");
  });

  it('remplace les paramètres', () => {
    expect(traduire('auth.connecte', { nom: 'Test User' })).toBe('Connecté en tant que Test User');
  });

  it('ne laisse aucune valeur vide dans le fichier', () => {
    for (const valeur of Object.values(frCA)) {
      expect(valeur.trim()).not.toBe('');
    }
  });
});
