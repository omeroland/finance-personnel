import { expect, test } from '@playwright/test';
import { ouvrirCompte } from './aide-etf';

test('import CSV : rapport ligne par ligne', async ({ page }) => {
  await ouvrirCompte(page);
  await page.getByRole('tab', { name: 'Import CSV' }).click();
  await page.getByTestId('csv-fichier').setInputFiles({
    name: 'transactions.csv',
    mimeType: 'text/csv',
    buffer: Buffer.from('date,compte,type,montant\n2026-01-10,CELI Questrade,contribution,5000\n'),
  });
  await page.getByTestId('csv-envoyer').click();
  // Prism répond avec l'exemple du contrat : 4 lignes lues, 3 créées, une ligne invalide.
  await expect(page.getByTestId('csv-rapport')).toBeVisible();
  await expect(page.getByTestId('csv-lues')).toContainText('4');
  await expect(page.getByTestId('csv-creees')).toContainText('3');
  await expect(page.getByTestId('csv-doublons')).toContainText('0');
  await expect(page.getByTestId('csv-refusees')).toContainText('0');
  const ligne = page.getByTestId('csv-lignes').locator('tbody tr').first();
  await expect(ligne).toContainText('4');
  await expect(ligne).toContainText('Erreur');
  await expect(ligne).toContainText('quantité');
  await expect(ligne).toContainText('Quantité absente.');
});

test('import CSV : un fichier de plus de 10 Mo est refusé sans envoi', async ({ page }) => {
  await ouvrirCompte(page);
  await page.getByRole('tab', { name: 'Import CSV' }).click();
  await page.getByTestId('csv-fichier').setInputFiles({
    name: 'gros.csv',
    mimeType: 'text/csv',
    buffer: Buffer.alloc(10 * 1024 * 1024 + 1, 'a'),
  });
  await expect(page.getByTestId('csv-erreur-fichier')).toContainText('10 Mo');
  await expect(page.getByTestId('csv-envoyer')).toBeDisabled();
});
