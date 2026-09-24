import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { of, throwError } from 'rxjs';
import { CreditError } from '../../domain/credit';
import { CREDIT_REPOSITORY } from '../../domain/credit-repository.token';
import { fakeRepository } from '../credits.testing';
import { CreditNewPage } from './credit-new.page';

async function render(repo = fakeRepository()) {
  await TestBed.configureTestingModule({
    imports: [CreditNewPage],
    providers: [provideRouter([]), { provide: CREDIT_REPOSITORY, useValue: repo }],
  }).compileComponents();
  const fixture = TestBed.createComponent(CreditNewPage);
  await fixture.whenStable();
  const el = fixture.nativeElement as HTMLElement;
  const input = (id: string) => el.querySelector(`[data-testid=${id}]`) as HTMLInputElement;
  const type = (id: string, value: string) => {
    input(id).value = value;
    input(id).dispatchEvent(new Event('input'));
  };
  const submit = async () => {
    (el.querySelector('[data-testid=submit]') as HTMLButtonElement).click();
    await fixture.whenStable();
  };
  return { fixture, el, input, type, submit, repo };
}

describe('CreditNewPage', () => {
  it('"Desembolsar cupo completo" copia el cupo al monto', async () => {
    const { input, type, el, fixture } = await render();
    type('limit', '10.000.000');
    (el.querySelector('[data-testid=full-limit]') as HTMLButtonElement).click();
    await fixture.whenStable();
    expect(input('amount').value).toBe('10.000.000');
  });

  it('envía el comando en centavos y bps, con llave de idempotencia, y navega al libro', async () => {
    const repo = fakeRepository();
    const { type, submit } = await render(repo);
    const nav = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    type('holder', 'María Fernanda Ruiz');
    type('limit', '10.000.000');
    type('rate', '24,5');
    type('amount', '5.000.000,50');
    type('date', '2026-06-01');
    await submit();
    expect(repo.open).toHaveBeenCalledTimes(1);
    const [cmd, key] = repo.open.mock.calls[0] as unknown as [unknown, string];
    expect(cmd).toEqual({
      holder: 'María Fernanda Ruiz',
      limitCents: 1_000_000_000,
      rateEaBps: 2450,
      disbursement: { amountCents: 500_000_050, date: '2026-06-01', description: '' },
    });
    expect(key).toMatch(/^[0-9a-f-]{36}$/);
    expect(nav).toHaveBeenCalledWith(['/creditos', 'c1']);
  });

  it('muestra el error del backend y conserva los datos ingresados', async () => {
    const repo = fakeRepository({
      open: vi.fn(() =>
        throwError(
          () =>
            new CreditError('DISBURSEMENT_EXCEEDS_LIMIT', 'El desembolso supera el cupo aprobado.', {
              limitCents: 1_000_000_000,
            }),
        ),
      ),
    });
    const { type, submit, el, input } = await render(repo);
    type('holder', 'María');
    type('amount', '20.000.000');
    await submit();
    expect(el.querySelector('[data-testid=error]')?.textContent).toContain(
      'El desembolso supera el cupo aprobado ($ 10.000.000,00)',
    );
    expect(input('holder').value).toBe('María');
    expect(input('amount').value).toBe('20.000.000');
  });

  it('valida en el cliente sin llamar al backend', async () => {
    const repo = fakeRepository();
    const { type, submit, el } = await render(repo);
    type('holder', 'María');
    type('amount', '1,234');
    await submit();
    expect(repo.open).not.toHaveBeenCalled();
    expect(el.querySelector('[data-testid=error]')?.textContent).toContain('desembolso inicial');
  });

  it('reintento tras error de red usa la misma llave', async () => {
    let calls = 0;
    const repo = fakeRepository({
      open: vi.fn(() => (++calls === 1 ? throwError(() => new Error('red')) : of({ id: 'c9', accountNumber: 'x' }))),
    });
    const { type, submit } = await render(repo);
    vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    type('holder', 'María');
    type('amount', '1.000');
    await submit();
    await submit();
    const keys = repo.open.mock.calls.map((c) => (c as unknown as [unknown, string])[1]);
    expect(keys.length).toBe(2);
    expect(keys[0]).toBe(keys[1]);
  });
});
