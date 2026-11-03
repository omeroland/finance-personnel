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
    ],
  },
  { path: '**', redirectTo: '' },
];
