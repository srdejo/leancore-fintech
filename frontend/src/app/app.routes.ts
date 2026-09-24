import { Routes } from '@angular/router';

export const routes: Routes = [
  { path: '', pathMatch: 'full', redirectTo: 'creditos' },
  { path: 'creditos', loadChildren: () => import('./credits/credits.routes').then((m) => m.CREDITS_ROUTES) },
  { path: '**', redirectTo: 'creditos' },
];
