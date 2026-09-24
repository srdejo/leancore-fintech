import { InjectionToken } from '@angular/core';
import { CreditRepository } from './credit-repository';

// Token en archivo aparte para que el resto del dominio no dependa de Angular.
export const CREDIT_REPOSITORY = new InjectionToken<CreditRepository>('CREDIT_REPOSITORY');
