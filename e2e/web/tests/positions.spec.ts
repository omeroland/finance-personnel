import { expect, test } from '@playwright/test';
import { ouvrirCompte } from './aide-etf';

test('positions d’un compte : quantités et encaisse', async ({ page }) => {
  await ouvrirCompte(page);
  await page.getByRole('tab', { name: 'Positions' }).click();
  await expect(page.getByTestId('positions')).toContainText('TSX:XEQT');
  await expect(page.getByTestId('positions')).toContainText('120');
  await expect(page.getByTestId('pos-encaisse')).toContainText('120,4');
});
