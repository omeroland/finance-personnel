import { expect, test } from '@playwright/test';
import { connecter } from './aide-etf';

test('catalogue de FNB : filtre RFG en fraction et comparaison côte à côte', async ({ page }) => {
  await connecter(page, 'menu-fnb');
  await expect(page.getByTestId('catalogue')).toContainText('TSX:XEQT');
  await expect(page.getByTestId('catalogue')).toContainText('TSX:ZAG');

  // Saisie en pourcentage, envoi en fraction.
  await page.getByTestId('filtre-rfg').fill('0,5');
  const requete = page.waitForRequest((r) => r.url().includes('/api/v1/portfolio/etfs?'));
  await page.getByTestId('filtrer').click();
  expect(new URL((await requete).url()).searchParams.get('maxMer')).toBe('0.005');

  await expect(page.getByTestId('comparer')).toBeDisabled();
  await page.getByTestId('choix-TSX:XEQT').check();
  await page.getByTestId('choix-TSX:ZAG').check();
  await page.getByTestId('comparer').click();
  const comparaison = page.getByTestId('comparaison');
  await expect(comparaison).toBeVisible();
  await expect(comparaison.getByTestId('comp-TSX:XEQT')).toBeVisible();
  await expect(comparaison.getByTestId('comp-TSX:ZAG')).toBeVisible();
  await expect(comparaison.locator('[data-row="rfg"]').first()).toContainText(/0,20\s%/);
});
