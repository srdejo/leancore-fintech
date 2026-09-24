import { Observable, of } from 'rxjs';
import type { Mock } from 'vitest';
import { CreditDetail, CreditSummary, Entry, InterestPreview, Ledger, OpenedCredit } from '../domain/credit';
import { CreditRepository } from '../domain/credit-repository';

/** Fake del puerto para tests de componentes: cada método es un vi.fn(). */
export type FakeRepository = Record<keyof CreditRepository, Mock>;

export function fakeRepository(overrides: Partial<FakeRepository> = {}): FakeRepository {
  return {
    list: vi.fn((): Observable<CreditSummary[]> => of([])),
    get: vi.fn((): Observable<CreditDetail> => of(detail())),
    ledger: vi.fn((): Observable<Ledger> => of({ entries: [], totalDebitCents: 0, totalCreditCents: 0 })),
    preview: vi.fn((): Observable<InterestPreview> => of(preview())),
    open: vi.fn((): Observable<OpenedCredit> => of({ id: 'c1', accountNumber: '0042-7781' })),
    registerPurchase: vi.fn((): Observable<Entry> => of(entry())),
    registerPayment: vi.fn((): Observable<Entry> => of(entry({ type: 'PAGO' }))),
    liquidateInterest: vi.fn((): Observable<Entry> => of(entry({ type: 'INTERES' }))),
    ...overrides,
  };
}

export function summary(over: Partial<CreditSummary> = {}): CreditSummary {
  return {
    id: 'c1',
    accountNumber: '0042-7781',
    holder: 'María Fernanda Ruiz',
    limitCents: 1_000_000_000,
    rateEaBps: 2400,
    capitalCents: 615_000_000,
    interestDueCents: 6_438_000,
    balanceCents: 621_438_000,
    availableCents: 378_562_000,
    usedBps: 6214,
    status: 'AL_DIA',
    lastEntryDate: '2026-08-05',
    ...over,
  };
}

export function detail(over: Partial<CreditDetail> = {}): CreditDetail {
  return {
    ...summary(),
    projectionDate: '2026-09-01',
    accruedInterestCents: 3_890_000,
    projectedBalanceCents: 625_328_000,
    ...over,
  };
}

export function entry(over: Partial<Entry> = {}): Entry {
  return {
    id: 'e1',
    seq: 1,
    type: 'CONSUMO',
    amountCents: 100,
    date: '2026-08-05',
    description: 'Consumo',
    detail: '',
    balanceAfterCents: 100,
    ...over,
  };
}

export function preview(over: Partial<InterestPreview> = {}): InterestPreview {
  return {
    date: '2026-09-01',
    capitalCents: 615_000_000,
    interestDueCents: 6_438_000,
    daysSinceLastLiquidation: 31,
    interestToLiquidateCents: 3_890_000,
    totalBalanceCents: 625_328_000,
    ...over,
  };
}
