import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, catchError, map, throwError } from 'rxjs';
import {
  CreditDetail,
  CreditError,
  CreditSummary,
  Entry,
  InterestPreview,
  IsoDate,
  Ledger,
  MovementCommand,
  NetworkError,
  OpenCreditCommand,
  OpenedCredit,
} from '../domain/credit';
import { CreditRepository } from '../domain/credit-repository';

const BASE = '/api/credits';

interface ApiErrorBody {
  code?: unknown;
  message?: unknown;
  details?: unknown;
}

/**
 * Adaptador HTTP del puerto CreditRepository. El contrato JSON del backend
 * coincide con el modelo de dominio (camelCase, centavos enteros, fechas
 * AAAA-MM-DD), así que no hay mapper: solo se traducen los errores.
 */
@Injectable()
export class CreditsHttpRepository implements CreditRepository {
  private readonly http = inject(HttpClient);

  list(): Observable<CreditSummary[]> {
    return this.http.get<CreditSummary[]>(BASE).pipe(catchError(toDomainError));
  }

  get(id: string): Observable<CreditDetail> {
    return this.http.get<CreditDetail>(`${BASE}/${id}`).pipe(catchError(toDomainError));
  }

  ledger(id: string): Observable<Ledger> {
    return this.http.get<Ledger>(`${BASE}/${id}/entries`).pipe(catchError(toDomainError));
  }

  preview(id: string, date: IsoDate): Observable<InterestPreview> {
    return this.http
      .get<InterestPreview>(`${BASE}/${id}/preview`, { params: { date } })
      .pipe(catchError(toDomainError));
  }

  open(cmd: OpenCreditCommand, idempotencyKey: string): Observable<OpenedCredit> {
    return this.http
      .post<OpenedCredit>(BASE, cmd, { headers: keyHeader(idempotencyKey) })
      .pipe(catchError(toDomainError));
  }

  registerPurchase(id: string, cmd: MovementCommand, idempotencyKey: string): Observable<Entry> {
    return this.command(`${BASE}/${id}/purchases`, cmd, idempotencyKey);
  }

  registerPayment(id: string, cmd: MovementCommand, idempotencyKey: string): Observable<Entry> {
    return this.command(`${BASE}/${id}/payments`, cmd, idempotencyKey);
  }

  liquidateInterest(id: string, date: IsoDate, idempotencyKey: string): Observable<Entry> {
    return this.command(`${BASE}/${id}/interest-liquidations`, { date }, idempotencyKey);
  }

  private command(url: string, body: object, idempotencyKey: string): Observable<Entry> {
    return this.http.post<{ entry: Entry }>(url, body, { headers: keyHeader(idempotencyKey) }).pipe(
      map((res) => res.entry),
      catchError(toDomainError),
    );
  }
}

function keyHeader(key: string): HttpHeaders {
  return new HttpHeaders({ 'Idempotency-Key': key });
}

/** 4xx con cuerpo de error -> CreditError; red, 5xx o desconocido -> NetworkError. */
export function toDomainError(err: unknown): Observable<never> {
  if (err instanceof HttpErrorResponse && err.status >= 400 && err.status < 500) {
    const body = (err.error ?? {}) as ApiErrorBody;
    if (typeof body.code === 'string') {
      const message = typeof body.message === 'string' ? body.message : 'La operación fue rechazada.';
      const details =
        body.details && typeof body.details === 'object' ? (body.details as Record<string, unknown>) : {};
      return throwError(() => new CreditError(body.code as string, message, details));
    }
  }
  return throwError(() => new NetworkError('No fue posible comunicarse con el servidor.'));
}
