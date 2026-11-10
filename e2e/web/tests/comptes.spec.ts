import { expect, test } from '@playwright/test';
import { COMPTE_ID, connecter } from './aide-etf';

test('liste des comptes et création d’un compte', async ({ page }) => {
  await connecter(page, 'menu-comptes');
  await expect(page.getByRole('heading', { name: 'Comptes' })).toBeVisible();
  const tableau = page.getByTestId('comptes');
  await expect(tableau).toContainText('CELI Questrade');
  await expect(tableau).toContainText('REER Desjardins');
  await expect(page.getByTestId(`compte-${COMPTE_ID}`)).toBeVisible();

  await page.getByTestId('creer-compte').click();
  await page.getByTestId('compte-nom').fill('Non enregistré test');
  await page.getByTestId('compte-courtier').fill('Questrade');
  await page.getByTestId('compte-type').click();
  await page.getByRole('option', { name: 'Non enregistré' }).click();
  const requete = page.waitForRequest((r) => r.method() === 'POST' && r.url().endsWith('/api/v1/accounts'));
  await page.getByTestId('compte-confirmer').click();
  const corps = (await requete).postDataJSON();
  expect(corps).toMatchObject({ name: 'Non enregistré test', type: 'taxable', broker: 'Questrade', currency: 'CAD' });
  await expect(page.getByText('Compte créé : ')).toBeVisible();
});
