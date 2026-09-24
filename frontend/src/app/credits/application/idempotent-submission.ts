import { signal } from '@angular/core';
import { EMPTY, Observable, defer, finalize, tap } from 'rxjs';

/**
 * Estado de envío de un formulario con su llave de idempotencia.
 *
 * La llave representa una intención ("registrar este pago"), no un clic:
 * - se genera al preparar el formulario;
 * - se reutiliza en todos los envíos hasta recibir una respuesta exitosa;
 * - se conserva ante error de red (no se sabe si se aplicó) o rechazo 4xx
 *   (el backend no la consumió);
 * - solo se renueva tras éxito, o con renew() cuando cambia la intención.
 *
 * Mientras hay un envío en curso, los envíos adicionales se ignoran.
 */
export class IdempotentSubmission {
  private readonly _key = signal('');
  private readonly _submitting = signal(false);

  readonly key = this._key.asReadonly();
  readonly submitting = this._submitting.asReadonly();

  constructor(private readonly newKey: () => string = generateUuid) {
    this._key.set(newKey());
  }

  /** Nueva intención (formulario reiniciado o cambio de operación). */
  renew(): void {
    if (!this._submitting()) this._key.set(this.newKey());
  }

  /** Envía con la llave vigente. Si ya hay un envío en curso, no hace nada. */
  submit<T>(send: (key: string) => Observable<T>): Observable<T> {
    return defer(() => {
      if (this._submitting()) return EMPTY;
      this._submitting.set(true);
      return send(this._key()).pipe(
        tap({ next: () => this._key.set(this.newKey()) }),
        finalize(() => this._submitting.set(false)),
      );
    });
  }
}

/** UUID v4; usa crypto.randomUUID si está disponible (contexto seguro). */
export function generateUuid(): string {
  if (typeof crypto.randomUUID === 'function') return crypto.randomUUID();
  const b = crypto.getRandomValues(new Uint8Array(16));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`;
}
