import { Routes } from '@angular/router';
import { CREDIT_REPOSITORY } from './domain/credit-repository.token';
import { CreditsHttpRepository } from './infrastructure/credits-http.repository';

export const CREDITS_ROUTES: Routes = [
  {
    path: '',
    providers: [{ provide: CREDIT_REPOSITORY, useClass: CreditsHttpRepository }],
    children: [
      { path: '', loadComponent: () => import('./ui/pages/credit-list.page').then((m) => m.CreditListPage) },
      { path: 'nuevo', loadComponent: () => import('./ui/pages/credit-new.page').then((m) => m.CreditNewPage) },
      { path: ':id', loadComponent: () => import('./ui/pages/credit-ledger.page').then((m) => m.CreditLedgerPage) },
    ],
  },
];
