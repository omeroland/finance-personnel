import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: 'import',
    loadComponent: () => import('./import/import-page').then((m) => m.ImportPage),
  },
  {
    path: ':symbol',
    loadComponent: () => import('./detail/detail').then((m) => m.Detail),
  },
  {
    path: '',
    loadComponent: () => import('./watchlist/watchlist').then((m) => m.Watchlist),
  },
];
