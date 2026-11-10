import { expect, test } from '@playwright/test';
import { ouvrirCompte } from './aide-etf';

test('saisie d’un relevé de valeur mensuel', async ({ page }) => {
  await ouvrirCompte(page);
  await page.getByRole('tab', { name: 'Relevés de valeur' }).click();
  await page.getByTestId('rel-mois').fill('2026-01');
  await page.getByTestId('rel-valeur').fill('12 345,67');
  await page.getByTestId('rel-cotisation').fill('-100');
  const requete = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/statements'));
  await page.getByTestId('rel-enregistrer').click();
  expect((await requete).postDataJSON()).toEqual({ month: '2026-01', endValue: '12345.67', contribution: '-100', currency: 'CAD' });
  await expect(page.getByTestId('rel-liste')).toBeVisible();
});

test('un mois manquant est refusé', async ({ page }) => {
  await ouvrirCompte(page);
  await page.getByRole('tab', { name: 'Relevés de valeur' }).click();
  await page.getByTestId('rel-valeur').fill('10');
  await page.getByTestId('rel-enregistrer').click();
  await expect(page.getByRole('alert').filter({ hasText: 'Choisissez un mois' })).toBeVisible();
});
