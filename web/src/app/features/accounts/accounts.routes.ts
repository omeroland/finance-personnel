import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: ':id',
    loadComponent: () => import('./account-page').then((m) => m.AccountPage),
  },
  {
    path: '',
    loadComponent: () => import('./accounts-list').then((m) => m.AccountsList),
  },
];
