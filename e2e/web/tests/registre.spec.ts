import { expect, test } from '@playwright/test';
import { ouvrirCompte } from './aide-etf';

test('registre : statuts en texte, ajout au clavier et écriture inverse', async ({ page }) => {
  await ouvrirCompte(page);
  // Prism : un achat annulé par son écriture inverse. Le statut est dit en texte, pas par la couleur seule.
  await expect(page.getByTestId('registre')).toBeVisible();
  await expect(page.getByTestId('tx-8c1d4e2f-6a3b-4c7d-9e0f-1a2b3c4d5e6f')).toContainText('Annulée');
  await expect(page.getByTestId('tx-9d8c7b6a-5f4e-4d3c-a2b1-0f9e8d7c6b5a')).toContainText('Écriture inverse');
  // Ni modification ni suppression : aucun bouton sur l'annulée et l'inverse.
  await expect(page.getByTestId('inverse-8c1d4e2f-6a3b-4c7d-9e0f-1a2b3c4d5e6f')).toHaveCount(0);

  // Saisie d'une cotisation entièrement au clavier.
  await page.getByTestId('tx-type').click();
  await page.getByRole('option', { name: 'Cotisation' }).click();
  await page.getByTestId('tx-date').fill('2026-02-01');
  await page.getByTestId('tx-amount').fill('500,25');
  const ajout = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/transactions'));
  await page.getByTestId('tx-amount').press('Enter');
  expect((await ajout).postDataJSON()).toMatchObject({ type: 'contribution', tradeDate: '2026-02-01', amount: '500.25', currency: 'CAD' });
  // Attend la fin de l'appel relayé vers Prism avant de clore le test.
  await expect(page.getByText('Transaction ajoutée.')).toBeVisible();
});

test('registre : un champ requis manquant est refusé avant l’envoi', async ({ page }) => {
  await ouvrirCompte(page);
  await page.getByTestId('tx-ajouter').click();
  await expect(page.getByRole('alert').filter({ hasText: 'Ce champ est requis' }).first()).toBeVisible();
});
