import { ChangeDetectionStrategy, Component, DestroyRef, OnInit, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Router, RouterLink } from '@angular/router';
import { CreditSummary } from '../../domain/credit';
import { CREDIT_REPOSITORY } from '../../domain/credit-repository.token';
import { formatBps, formatCop, formatUsedPct } from '../../domain/money';
import { STATUS_LABELS, errorMessage } from '../presentation';

@Component({
  selector: 'app-credit-list-page',
  imports: [RouterLink],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="page">
      <header class="page-header">
        <div class="page-header__titles">
          <div class="eyebrow">Libro mayor · Créditos</div>
          <h1 class="title">Créditos</h1>
        </div>
        <a class="btn" routerLink="nuevo" data-testid="new-credit">+ Nuevo crédito</a>
      </header>

      @if (error()) {
        <div class="alert" role="alert">{{ error() }}</div>
      }

      @if (credits(); as list) {
        @if (list.length === 0) {
          <div class="empty" data-testid="empty">Aún no hay créditos. Crea el primero para abrir el libro.</div>
        } @else {
          <div class="table-wrap">
            <table class="table">
              <thead>
                <tr>
                  <th>Titular</th>
                  <th>N.º cuenta</th>
                  <th class="num">Cupo</th>
                  <th class="num">Saldo</th>
                  <th class="num">Disponible</th>
                  <th>Utilización</th>
                  <th>Estado</th>
                </tr>
              </thead>
              <tbody>
                @for (c of list; track c.id) {
                  <tr class="clickable" (click)="open(c)" data-testid="credit-row">
                    <td>
                      <div>{{ c.holder }}</div>
                      <div class="muted" style="font-size: 12px">{{ rate(c) }}% EA</div>
                    </td>
                    <td class="mono">{{ c.accountNumber }}</td>
                    <td class="num">{{ money(c.limitCents) }}</td>
                    <td class="num">{{ money(c.balanceCents) }}</td>
                    <td class="num">{{ money(c.availableCents) }}</td>
                    <td style="min-width: 120px">
                      <div class="bar" [class.bar--alert]="c.status === 'SOBREGIRADO'">
                        <div class="bar__fill" [style.width.%]="c.usedBps / 100"></div>
                      </div>
                      <div class="muted" style="font-size: 12px; margin-top: 4px">{{ used(c) }}</div>
                    </td>
                    <td>
                      <span class="pill" [class.pill--alert]="c.status === 'SOBREGIRADO'" data-testid="status">
                        {{ statusLabel(c) }}
                      </span>
                    </td>
                  </tr>
                }
              </tbody>
            </table>
          </div>
        }
      }
    </div>
  `,
})
export class CreditListPage implements OnInit {
  private readonly repo = inject(CREDIT_REPOSITORY);
  private readonly router = inject(Router);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly credits = signal<CreditSummary[] | null>(null);
  protected readonly error = signal('');

  protected readonly money = formatCop;

  ngOnInit(): void {
    this.repo
      .list()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({ next: (list) => this.credits.set(list), error: (e: unknown) => this.error.set(errorMessage(e)) });
  }

  protected open(c: CreditSummary): void {
    void this.router.navigate(['/creditos', c.id]);
  }

  protected rate(c: CreditSummary): string {
    return formatBps(c.rateEaBps);
  }

  protected used(c: CreditSummary): string {
    return `${formatUsedPct(c.usedBps)} utilizado`;
  }

  protected statusLabel(c: CreditSummary): string {
    return STATUS_LABELS[c.status];
  }
}
