import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { firstValueFrom } from 'rxjs';
import { CreditError, NetworkError } from '../domain/credit';
import { CreditsHttpRepository } from './credits-http.repository';

describe('CreditsHttpRepository', () => {
  let repo: CreditsHttpRepository;
  let http: HttpTestingController;
  const id = '11111111-1111-4111-8111-111111111111';
  const key = '22222222-2222-4222-8222-222222222222';

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting(), CreditsHttpRepository],
    });
    repo = TestBed.inject(CreditsHttpRepository);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('lista créditos', async () => {
    const p = firstValueFrom(repo.list());
    http.expectOne({ method: 'GET', url: '/api/credits' }).flush([]);
    expect(await p).toEqual([]);
  });

  it('consulta crédito, historial y vista previa', async () => {
    const d = firstValueFrom(repo.get(id));
    http.expectOne(`/api/credits/${id}`).flush({ id });
    expect((await d).id).toBe(id);

    const l = firstValueFrom(repo.ledger(id));
    http.expectOne(`/api/credits/${id}/entries`).flush({ entries: [], totalDebitCents: 0, totalCreditCents: 0 });
    expect((await l).entries).toEqual([]);

    const pv = firstValueFrom(repo.preview(id, '2026-09-01'));
    const req = http.expectOne((r) => r.url === `/api/credits/${id}/preview`);
    expect(req.request.params.get('date')).toBe('2026-09-01');
    req.flush({ interestToLiquidateCents: 5 });
    expect((await pv).interestToLiquidateCents).toBe(5);
  });

  it('abre un crédito con la llave de idempotencia y el cuerpo esperado', async () => {
    const cmd = {
      holder: 'María',
      limitCents: 1000000000,
      rateEaBps: 2400,
      disbursement: { amountCents: 500000000, date: '2026-06-01', description: '' },
    };
    const p = firstValueFrom(repo.open(cmd, key));
    const req = http.expectOne({ method: 'POST', url: '/api/credits' });
    expect(req.request.headers.get('Idempotency-Key')).toBe(key);
    expect(req.request.body).toEqual(cmd);
    req.flush({ id, accountNumber: '0042-7781' });
    expect((await p).accountNumber).toBe('0042-7781');
  });

  it.each([
    ['registerPurchase', 'purchases'],
    ['registerPayment', 'payments'],
  ] as const)('%s envía el comando a /%s', async (method, path) => {
    const cmd = { amountCents: 150000050, date: '2026-06-05', description: 'Compra' };
    const p = firstValueFrom(repo[method](id, cmd, key));
    const req = http.expectOne({ method: 'POST', url: `/api/credits/${id}/${path}` });
    expect(req.request.headers.get('Idempotency-Key')).toBe(key);
    expect(req.request.body).toEqual(cmd);
    req.flush({ entry: { id: 'e1', type: 'CONSUMO' } });
    expect((await p).id).toBe('e1');
  });

  it('liquida interés a una fecha', async () => {
    const p = firstValueFrom(repo.liquidateInterest(id, '2026-09-01', key));
    const req = http.expectOne({ method: 'POST', url: `/api/credits/${id}/interest-liquidations` });
    expect(req.request.body).toEqual({ date: '2026-09-01' });
    expect(req.request.headers.get('Idempotency-Key')).toBe(key);
    req.flush({ entry: { id: 'e2', type: 'INTERES' } });
    expect((await p).type).toBe('INTERES');
  });

  it('traduce un 422 a CreditError con código y detalles', async () => {
    const p = firstValueFrom(repo.registerPurchase(id, { amountCents: 1, date: '2026-06-01', description: '' }, key));
    http
      .expectOne(`/api/credits/${id}/purchases`)
      .flush(
        { code: 'INSUFFICIENT_AVAILABLE', message: 'Fondos insuficientes.', details: { availableCents: 2500 } },
        { status: 422, statusText: 'Unprocessable Entity' },
      );
    const err = await p.catch((e: unknown) => e);
    expect(err).toBeInstanceOf(CreditError);
    expect((err as CreditError).code).toBe('INSUFFICIENT_AVAILABLE');
    expect((err as CreditError).details['availableCents']).toBe(2500);
  });

  it('traduce errores de red y 5xx a NetworkError', async () => {
    const p1 = firstValueFrom(repo.list());
    http.expectOne('/api/credits').error(new ProgressEvent('error'));
    expect(await p1.catch((e: unknown) => e)).toBeInstanceOf(NetworkError);

    const p2 = firstValueFrom(repo.list());
    http.expectOne('/api/credits').flush({ code: 'INTERNAL' }, { status: 500, statusText: 'Error' });
    expect(await p2.catch((e: unknown) => e)).toBeInstanceOf(NetworkError);
  });
});
