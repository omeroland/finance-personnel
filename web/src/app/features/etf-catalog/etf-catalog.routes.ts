import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: '',
    loadComponent: () => import('./etf-catalog').then((m) => m.EtfCatalog),
  },
];
