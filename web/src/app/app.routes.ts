import { Routes } from '@angular/router';
import { authGuard } from './core/auth';
import { Shell } from './shell/shell';

export const routes: Routes = [
  {
    path: '',
    component: Shell,
    canActivate: [authGuard],
    children: [
      { path: '', pathMatch: 'full', redirectTo: 'stocks' },
      {
        path: 'stocks',
        loadChildren: () => import('./features/stockeval/stockeval.routes').then((m) => m.routes),
      },
      {
        path: 'comptes',
        loadChildren: () => import('./features/accounts/accounts.routes').then((m) => m.routes),
      },
      {
        path: 'fnb',
        loadChildren: () => import('./features/etf-catalog/etf-catalog.routes').then((m) => m.routes),
      },
      {
        path: 'portefeuille',
        loadChildren: () => import('./features/portfolio/portfolio.routes').then((m) => m.routes),
      },
    ],
  },
  { path: '**', redirectTo: '' },
];
