import { CreditError, CreditStatus, EntryType, IsoDate } from '../domain/credit';
import { formatCop } from '../domain/money';

export const STATUS_LABELS: Record<CreditStatus, string> = {
  AL_DIA: 'Al día',
  SOBREGIRADO: 'Sobregirado',
  SIN_SALDO: 'Sin saldo',
};

export const ENTRY_LABELS: Record<EntryType, string> = {
  DESEMBOLSO: 'Desembolso',
  CONSUMO: 'Consumo',
  INTERES: 'Interés',
  PAGO: 'Pago',
};

/** Fecha de hoy (local) en AAAA-MM-DD. */
export function todayIso(now: Date = new Date()): IsoDate {
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${now.getFullYear()}-${pad(now.getMonth() + 1)}-${pad(now.getDate())}`;
}

const MONTHS = ['ene', 'feb', 'mar', 'abr', 'may', 'jun', 'jul', 'ago', 'sep', 'oct', 'nov', 'dic'];

/** "2026-08-05" -> "05 ago 2026" (sin pasar por Date para evitar husos). */
export function formatDate(iso: IsoDate): string {
  const [y, m, d] = iso.split('-');
  return `${d} ${MONTHS[Number(m) - 1] ?? m} ${y}`;
}

/** Mensaje para el usuario a partir de un error del puerto. */
export function errorMessage(err: unknown): string {
  if (!(err instanceof CreditError)) {
    return 'No fue posible comunicarse con el servidor. Intenta de nuevo: la operación no se duplicará.';
  }
  const d = err.details;
  switch (err.code) {
    case 'INSUFFICIENT_AVAILABLE':
      return `Fondos insuficientes. Cupo disponible: ${formatCop(Number(d['availableCents']))}.`;
    case 'PAYMENT_EXCEEDS_BALANCE':
      return `El pago supera el saldo pendiente (${formatCop(Number(d['balanceCents']))}).`;
    case 'DATE_BEFORE_LAST_ENTRY':
      return `La fecha no puede ser anterior al último movimiento (${formatDate(String(d['lastEntryDate']))}).`;
    case 'DISBURSEMENT_EXCEEDS_LIMIT':
      return `El desembolso supera el cupo aprobado (${formatCop(Number(d['limitCents']))}).`;
    default:
      return err.message;
  }
}
