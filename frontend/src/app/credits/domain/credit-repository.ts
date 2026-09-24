import { Observable } from 'rxjs';
import {
  CreditDetail,
  CreditSummary,
  Entry,
  InterestPreview,
  IsoDate,
  Ledger,
  MovementCommand,
  OpenCreditCommand,
  OpenedCredit,
} from './credit';

/**
 * Puerto: lo que la aplicación necesita de los créditos. Los comandos reciben
 * la llave de idempotencia explícitamente: la genera quien prepara el
 * formulario (una por intención), no el adaptador.
 */
export interface CreditRepository {
  list(): Observable<CreditSummary[]>;
  get(id: string): Observable<CreditDetail>;
  ledger(id: string): Observable<Ledger>;
  preview(id: string, date: IsoDate): Observable<InterestPreview>;
  open(cmd: OpenCreditCommand, idempotencyKey: string): Observable<OpenedCredit>;
  registerPurchase(id: string, cmd: MovementCommand, idempotencyKey: string): Observable<Entry>;
  registerPayment(id: string, cmd: MovementCommand, idempotencyKey: string): Observable<Entry>;
  liquidateInterest(id: string, date: IsoDate, idempotencyKey: string): Observable<Entry>;
}
