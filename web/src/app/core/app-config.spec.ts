import { validerConfig } from './app-config';

describe('validerConfig', () => {
  const valide = { apiUrl: 'https://api.exemple/', oidc: { issuer: 'https://kc/realms/finance', clientId: 'finance-web' } };

  it('accepte une configuration complète et retire la barre finale', () => {
    expect(validerConfig(valide).apiUrl).toBe('https://api.exemple');
  });

  it('accepte une URL d’API vide (même origine)', () => {
    expect(validerConfig({ ...valide, apiUrl: '' }).apiUrl).toBe('');
  });

  it.each([null, {}, { apiUrl: '' }, { ...valide, oidc: { issuer: '', clientId: 'x' } }, { ...valide, oidc: { issuer: 'x' } }])(
    'refuse %j',
    (brut) => {
      expect(() => validerConfig(brut)).toThrow();
    },
  );
});
