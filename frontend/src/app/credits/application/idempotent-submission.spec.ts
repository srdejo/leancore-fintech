import { Subject, of, throwError } from 'rxjs';
import { CreditError, NetworkError } from '../domain/credit';
import { IdempotentSubmission, generateUuid } from './idempotent-submission';

function counterKeys(): () => string {
  let n = 0;
  return () => `k${++n}`;
}

describe('IdempotentSubmission', () => {
  it('genera la llave al preparar el formulario, antes de cualquier clic', () => {
    const s = new IdempotentSubmission(counterKeys());
    expect(s.key()).toBe('k1');
  });

  it('doble clic: a lo sumo un envío en vuelo, con la misma llave', () => {
    const s = new IdempotentSubmission(counterKeys());
    const pending = new Subject<string>();
    const send = vi.fn((key: string) => pending.asObservable().pipe());
    s.submit(send).subscribe();
    s.submit(send).subscribe(); // segundo clic mientras el primero está en vuelo
    expect(send).toHaveBeenCalledTimes(1);
    expect(send).toHaveBeenCalledWith('k1');
    expect(s.submitting()).toBe(true);
    pending.next('ok');
    pending.complete();
    expect(s.submitting()).toBe(false);
  });

  it('reintento tras error de red: reutiliza la misma llave', () => {
    const s = new IdempotentSubmission(counterKeys());
    const keys: string[] = [];
    s.submit((k) => {
      keys.push(k);
      return throwError(() => new NetworkError('red'));
    }).subscribe({ error: () => undefined });
    s.submit((k) => {
      keys.push(k);
      return of('ok');
    }).subscribe();
    expect(keys).toEqual(['k1', 'k1']);
  });

  it('rechazo de negocio: conserva la llave (el backend no la consumió)', () => {
    const s = new IdempotentSubmission(counterKeys());
    s.submit(() => throwError(() => new CreditError('PAYMENT_EXCEEDS_BALANCE', 'x'))).subscribe({
      error: () => undefined,
    });
    expect(s.key()).toBe('k1');
    expect(s.submitting()).toBe(false);
  });

  it('nueva operación tras éxito: usa una llave nueva', () => {
    const s = new IdempotentSubmission(counterKeys());
    const keys: string[] = [];
    const send = (k: string) => {
      keys.push(k);
      return of('ok');
    };
    s.submit(send).subscribe();
    s.submit(send).subscribe();
    expect(keys).toEqual(['k1', 'k2']);
  });

  it('renew() cambia la intención, pero no durante un envío en vuelo', () => {
    const s = new IdempotentSubmission(counterKeys());
    s.renew();
    expect(s.key()).toBe('k2');
    const pending = new Subject<string>();
    s.submit(() => pending).subscribe();
    s.renew();
    expect(s.key()).toBe('k2');
  });

  it('generateUuid produce un UUID v4', () => {
    expect(generateUuid()).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  });
});
