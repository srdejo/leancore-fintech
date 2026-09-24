import { Cents } from './money';

/** Fecha civil en formato AAAA-MM-DD. */
export type IsoDate = string;

export type CreditStatus = 'AL_DIA' | 'SOBREGIRADO' | 'SIN_SALDO';
export type EntryType = 'DESEMBOLSO' | 'CONSUMO' | 'INTERES' | 'PAGO';

export interface CreditSummary {
  id: string;
  accountNumber: string;
  holder: string;
  limitCents: Cents;
  rateEaBps: number;
  capitalCents: Cents;
  interestDueCents: Cents;
  balanceCents: Cents;
  availableCents: Cents;
  usedBps: number;
  status: CreditStatus;
  lastEntryDate: IsoDate;
}

export interface CreditDetail extends CreditSummary {
  projectionDate: IsoDate;
  accruedInterestCents: Cents;
  projectedBalanceCents: Cents;
}

export interface Allocation {
  useEntryId: string;
  useSeq: number;
  toCapitalCents: Cents;
}

export interface Entry {
  id: string;
  seq: number;
  type: EntryType;
  amountCents: Cents;
  date: IsoDate;
  description: string;
  detail: string;
  toInterestCents?: Cents;
  toCapitalCents?: Cents;
  allocations?: Allocation[];
  balanceAfterCents: Cents;
}

export interface Ledger {
  entries: Entry[];
  totalDebitCents: Cents;
  totalCreditCents: Cents;
}

export interface InterestPreview {
  date: IsoDate;
  capitalCents: Cents;
  interestDueCents: Cents;
  daysSinceLastLiquidation: number;
  interestToLiquidateCents: Cents;
  totalBalanceCents: Cents;
}

export interface OpenCreditCommand {
  holder: string;
  limitCents: Cents;
  rateEaBps: number;
  disbursement: { amountCents: Cents; date: IsoDate; description: string };
}

export interface OpenedCredit {
  id: string;
  accountNumber: string;
}

export interface MovementCommand {
  amountCents: Cents;
  date: IsoDate;
  description: string;
}

/** Rechazo de negocio o de validación reportado por el backend. */
export class CreditError extends Error {
  constructor(
    readonly code: string,
    message: string,
    readonly details: Readonly<Record<string, unknown>> = {},
  ) {
    super(message);
  }
}

/** Fallo de red o respuesta desconocida: no se sabe si la operación se aplicó. */
export class NetworkError extends Error {}
