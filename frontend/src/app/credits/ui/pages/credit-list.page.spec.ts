import { TestBed } from '@angular/core/testing';
import { Router, provideRouter } from '@angular/router';
import { of } from 'rxjs';
import { CREDIT_REPOSITORY } from '../../domain/credit-repository.token';
import { fakeRepository, summary } from '../credits.testing';
import { CreditListPage } from './credit-list.page';

async function render(repo = fakeRepository()) {
  await TestBed.configureTestingModule({
    imports: [CreditListPage],
    providers: [provideRouter([]), { provide: CREDIT_REPOSITORY, useValue: repo }],
  }).compileComponents();
  const fixture = TestBed.createComponent(CreditListPage);
  await fixture.whenStable();
  return { fixture, el: fixture.nativeElement as HTMLElement };
}

describe('CreditListPage', () => {
  it('muestra el estado vacío', async () => {
    const { el } = await render();
    expect(el.querySelector('[data-testid=empty]')?.textContent).toContain('Aún no hay créditos');
    expect(el.querySelector('[data-testid=new-credit]')).not.toBeNull();
  });

  it('lista créditos con cifras formateadas y alerta de sobregiro', async () => {
    const repo = fakeRepository({
      list: vi.fn(() =>
        of([
          summary(),
          summary({ id: 'c2', holder: 'Carlos Pérez', status: 'SOBREGIRADO', usedBps: 10000, availableCents: 0 }),
        ]),
      ),
    });
    const { el } = await render(repo);
    const rows = el.querySelectorAll('[data-testid=credit-row]');
    expect(rows.length).toBe(2);
    expect(rows[0].textContent).toContain('$ 6.214.380,00');
    expect(rows[0].textContent).toContain('62% utilizado');
    const statuses = el.querySelectorAll('[data-testid=status]');
    expect(statuses[0].textContent?.trim()).toBe('Al día');
    expect(statuses[1].textContent?.trim()).toBe('Sobregirado');
    expect(statuses[1].classList).toContain('pill--alert');
    expect(rows[1].querySelector('.bar')?.classList).toContain('bar--alert');
  });

  it('navega al libro mayor al seleccionar un crédito', async () => {
    const repo = fakeRepository({ list: vi.fn(() => of([summary({ id: 'abc' })])) });
    const { el } = await render(repo);
    const router = TestBed.inject(Router);
    const nav = vi.spyOn(router, 'navigate').mockResolvedValue(true);
    (el.querySelector('[data-testid=credit-row]') as HTMLElement).click();
    expect(nav).toHaveBeenCalledWith(['/creditos', 'abc']);
  });
});
