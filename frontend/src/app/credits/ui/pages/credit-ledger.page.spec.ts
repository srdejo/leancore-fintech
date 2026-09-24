import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { of, throwError } from 'rxjs';
import { CreditError } from '../../domain/credit';
import { CREDIT_REPOSITORY } from '../../domain/credit-repository.token';
import { FakeRepository, detail, entry, fakeRepository, preview } from '../credits.testing';
import { CreditLedgerPage } from './credit-ledger.page';

async function render(repo: FakeRepository = fakeRepository()) {
  await TestBed.configureTestingModule({
    imports: [CreditLedgerPage],
    providers: [provideRouter([]), { provide: CREDIT_REPOSITORY, useValue: repo }],
  }).compileComponents();
  const fixture = TestBed.createComponent(CreditLedgerPage);
  fixture.componentRef.setInput('id', 'c1');
  await fixture.whenStable();
  const el = fixture.nativeElement as HTMLElement;
  const q = (id: string) => el.querySelector(`[data-testid=${id}]`) as HTMLElement;
  const type = async (id: string, value: string) => {
    const input = q(id) as HTMLInputElement;
    input.value = value;
    input.dispatchEvent(new Event('input'));
    await fixture.whenStable();
  };
  const click = async (id: string) => {
    q(id).click();
    await fixture.whenStable();
  };
  return { fixture, el, q, type, click, repo };
}

const paymentLedger = {
  entries: [
    entry({
      id: 'p1',
      seq: 6,
      type: 'PAGO',
      amountCents: 900_000_000,
      date: '2026-08-05',
      description: 'Pago parcial',
      toInterestCents: 64_380_000,
      toCapitalCents: 835_620_000,
      allocations: [
        { useEntryId: 'u2', useSeq: 2, toCapitalCents: 450_000_000 },
        { useEntryId: 'u3', useSeq: 3, toCapitalCents: 385_620_000 },
      ],
      balanceAfterCents: 621_438_000,
    }),
    entry({ id: 'i1', seq: 5, type: 'INTERES', amountCents: 98_400, detail: '31 días al 24% EA' }),
  ],
  totalDebitCents: 1_521_438_000,
  totalCreditCents: 900_000_000,
};

describe('CreditLedgerPage', () => {
  it('muestra condiciones de solo lectura, saldos y proyección', async () => {
    const { q, el } = await render();
    expect(q('limit').textContent).toContain('$ 10.000.000,00');
    expect(q('rate').textContent).toContain('24% EA');
    expect(q('balance').textContent).toContain('$ 6.214.380,00');
    expect(el.textContent).toContain('Con interés devengado a hoy: $ 6.253.280,00');
    expect(el.querySelectorAll('input[data-testid=limit], input[data-testid=rate]').length).toBe(0);
    expect(el.textContent).not.toContain('Deshacer');
  });

  it('detalla cada pago: interés, capital y reparto FIFO por uso', async () => {
    const repo = fakeRepository({ ledger: vi.fn(() => of(paymentLedger)) });
    const { el, q } = await render(repo);
    expect(q('entry-detail').textContent).toBe('Interés $ 643.800,00 · Capital $ 8.356.200,00');
    const uses = Array.from(el.querySelectorAll('[data-testid=entry-use]')).map((u) => u.textContent);
    expect(uses).toEqual(['Uso #2: $ 4.500.000,00', 'Uso #3: $ 3.856.200,00']);
    expect(q('total-credit').textContent).toContain('$ 9.000.000,00');
  });

  it('resalta el sobregiro', async () => {
    const repo = fakeRepository({ get: vi.fn(() => of(detail({ status: 'SOBREGIRADO', availableCents: 0 }))) });
    const { q } = await render(repo);
    expect(q('status').classList).toContain('pill--alert');
    expect(q('bar').classList).toContain('bar--alert');
  });

  it('pago: muestra la vista previa a la fecha y "Pagar saldo total" completa el monto', async () => {
    const repo = fakeRepository();
    const { q, click, type } = await render(repo);
    await click('tab-PAGO');
    await type('op-date', '2026-09-10');
    expect(repo.preview).toHaveBeenLastCalledWith('c1', '2026-09-10');
    expect(q('pay-total').textContent).toContain('$ 6.253.280,00');
    await click('pay-all');
    expect((q('op-amount') as HTMLInputElement).value).toBe('6.253.280,00');
  });

  it('registra un consumo en centavos con llave y recarga saldo e historial', async () => {
    const repo = fakeRepository();
    const { type, click } = await render(repo);
    await type('op-date', '2026-09-01');
    await type('op-amount', '150.000,50');
    await click('op-submit');
    expect(repo.registerPurchase).toHaveBeenCalledTimes(1);
    const [id, cmd, key] = repo.registerPurchase.mock.calls[0] as [string, unknown, string];
    expect(id).toBe('c1');
    expect(cmd).toEqual({ amountCents: 15_000_050, date: '2026-09-01', description: '' });
    expect(key).toMatch(/^[0-9a-f-]{36}$/);
    expect(repo.get).toHaveBeenCalledTimes(2);
    expect(repo.ledger).toHaveBeenCalledTimes(2);
  });

  it('muestra el rechazo del backend y conserva el monto', async () => {
    const repo = fakeRepository({
      registerPurchase: vi.fn(() =>
        throwError(() => new CreditError('INSUFFICIENT_AVAILABLE', 'Fondos insuficientes.', { availableCents: 2_500_000_000 })),
      ),
    });
    const { type, click, q } = await render(repo);
    await type('op-amount', '30.000.000');
    await click('op-submit');
    expect(q('op-error').textContent).toContain('Fondos insuficientes. Cupo disponible: $ 25.000.000,00.');
    expect((q('op-amount') as HTMLInputElement).value).toBe('30.000.000');
  });

  it('liquidar interés: muestra la vista previa y confirma con la fecha', async () => {
    const repo = fakeRepository({ preview: vi.fn(() => of(preview({ interestToLiquidateCents: 98_400 }))) });
    const { click, type, q } = await render(repo);
    await click('tab-LIQUIDAR');
    await type('op-date', '2026-09-01');
    expect(q('liq-amount').textContent).toContain('$ 984,00');
    await click('op-submit');
    expect(repo.liquidateInterest).toHaveBeenCalledWith('c1', '2026-09-01', expect.any(String));
  });

  it('cambiar de operación renueva la llave; reintentar la misma conserva la llave', async () => {
    let calls = 0;
    const repo = fakeRepository({
      registerPayment: vi.fn(() => (++calls === 1 ? throwError(() => new Error('red')) : of(entry({ type: 'PAGO' })))),
    });
    const { click, type } = await render(repo);
    await click('tab-PAGO');
    await type('op-amount', '1.000');
    await click('op-submit');
    await click('op-submit');
    const keys = repo.registerPayment.mock.calls.map((c) => (c as [string, unknown, string])[2]);
    expect(keys[0]).toBe(keys[1]);

    await click('tab-CONSUMO');
    await type('op-amount', '1.000');
    await click('op-submit');
    const purchaseKey = (repo.registerPurchase.mock.calls[0] as [string, unknown, string])[2];
    expect(purchaseKey).not.toBe(keys[0]);
  });
});
