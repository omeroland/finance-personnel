import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: '',
    loadComponent: () => import('./watchlist/watchlist').then((m) => m.Watchlist),
  },
];
