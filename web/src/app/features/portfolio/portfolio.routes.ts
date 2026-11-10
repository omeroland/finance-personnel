import { Routes } from '@angular/router';

export const routes: Routes = [
  {
    path: '',
    loadComponent: () => import('./portfolio-layout').then((m) => m.PortfolioLayout),
    children: [
      {
        path: '',
        pathMatch: 'full',
        loadComponent: () => import('./dashboard').then((m) => m.Dashboard),
      },
      {
        path: 'modeles',
        loadComponent: () => import('./modeles').then((m) => m.Modeles),
      },
      {
        path: 'achat',
        loadComponent: () => import('./plan-achat').then((m) => m.PlanAchat),
      },
      {
        path: 'reequilibrage',
        loadComponent: () => import('./plan-reequilibrage').then((m) => m.PlanReequilibrage),
      },
      {
        path: 'exposition',
        loadComponent: () => import('./exposition').then((m) => m.Exposition),
      },
      {
        path: 'rendement',
        loadComponent: () => import('./rendement').then((m) => m.Rendement),
      },
    ],
  },
];
