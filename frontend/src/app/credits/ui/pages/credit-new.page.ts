import { ChangeDetectionStrategy, Component, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { FormControl, FormGroup, ReactiveFormsModule } from '@angular/forms';
import { Router, RouterLink } from '@angular/router';
import { IdempotentSubmission } from '../../application/idempotent-submission';
import { OpenCreditCommand } from '../../domain/credit';
import { CREDIT_REPOSITORY } from '../../domain/credit-repository.token';
import { parseCents, parseRateBps } from '../../domain/money';
import { errorMessage, todayIso } from '../presentation';

@Component({
  selector: 'app-credit-new-page',
  imports: [ReactiveFormsModule, RouterLink],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <form class="page page--narrow" [formGroup]="form" (ngSubmit)="create()" novalidate>
      <a class="back" routerLink="/creditos">← Créditos</a>
      <header class="page-header" style="flex-direction: column; align-items: stretch; gap: 8px">
        <div class="eyebrow">Apertura</div>
        <h1 class="title">Nuevo crédito</h1>
        <p class="lead">
          Define las condiciones de la cuenta y registra el desembolso inicial. La tasa y el cupo no podrán
          cambiarse después.
        </p>
      </header>

      <section style="display: flex; flex-direction: column; gap: 18px">
        <div class="section-label">Condiciones</div>
        <div class="grid">
          <label class="field" style="grid-column: 1 / -1">Titular
            <input class="input" formControlName="holder" placeholder="Nombre del titular" data-testid="holder" />
          </label>
          <label class="field">Cupo aprobado (COP)
            <input class="input input--mono" formControlName="limit" inputmode="decimal" placeholder="10.000.000"
                   data-testid="limit" />
          </label>
          <label class="field">Tasa efectiva anual (% EA)
            <input class="input input--mono" formControlName="rate" inputmode="decimal" placeholder="24"
                   data-testid="rate" />
          </label>
        </div>
      </section>

      <section style="display: flex; flex-direction: column; gap: 18px; border-top: 1px solid var(--line); padding-top: 24px">
        <div style="display: flex; justify-content: space-between; align-items: baseline; gap: 12px; flex-wrap: wrap">
          <div class="section-label">Desembolso inicial · obligatorio</div>
          <button type="button" class="link-btn" (click)="disburseFullLimit()" data-testid="full-limit">
            Desembolsar cupo completo
          </button>
        </div>
        <div class="grid">
          <label class="field">Monto (COP)
            <input class="input input--big" formControlName="amount" inputmode="decimal" placeholder="0"
                   data-testid="amount" />
          </label>
          <label class="field">Fecha
            <input class="input input--mono" type="date" formControlName="date" data-testid="date" />
          </label>
        </div>
        <label class="field">Descripción
          <input class="input" formControlName="description" placeholder="Desembolso inicial del crédito" />
        </label>
      </section>

      @if (error()) {
        <div class="alert" role="alert" data-testid="error">{{ error() }}</div>
      }

      <div style="display: flex; justify-content: flex-end">
        <button class="btn" type="submit" [disabled]="submission.submitting()" data-testid="submit">
          Crear crédito y desembolsar
        </button>
      </div>
    </form>
  `,
})
export class CreditNewPage {
  private readonly repo = inject(CREDIT_REPOSITORY);
  private readonly router = inject(Router);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly submission = new IdempotentSubmission();
  protected readonly error = signal('');

  protected readonly form = new FormGroup({
    holder: new FormControl('', { nonNullable: true }),
    limit: new FormControl('10.000.000', { nonNullable: true }),
    rate: new FormControl('24', { nonNullable: true }),
    amount: new FormControl('', { nonNullable: true }),
    date: new FormControl(todayIso(), { nonNullable: true }),
    description: new FormControl('', { nonNullable: true }),
  });

  constructor() {
    this.form.valueChanges.pipe(takeUntilDestroyed()).subscribe(() => this.error.set(''));
  }

  protected disburseFullLimit(): void {
    this.form.controls.amount.setValue(this.form.controls.limit.value);
  }

  protected create(): void {
    const cmd = this.toCommand();
    if (typeof cmd === 'string') {
      this.error.set(cmd);
      return;
    }
    this.submission
      .submit((key) => this.repo.open(cmd, key))
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (c) => void this.router.navigate(['/creditos', c.id]),
        error: (e: unknown) => this.error.set(errorMessage(e)),
      });
  }

  /** Convierte el formulario a comando; devuelve un mensaje si algo no es válido. */
  private toCommand(): OpenCreditCommand | string {
    const v = this.form.getRawValue();
    const limitCents = parseCents(v.limit);
    const rateEaBps = parseRateBps(v.rate);
    const amountCents = parseCents(v.amount);
    if (!v.holder.trim()) return 'Indica el nombre del titular.';
    if (limitCents === null || limitCents <= 0) return 'El cupo aprobado debe ser un monto mayor que cero.';
    if (rateEaBps === null) return 'La tasa EA debe ser un porcentaje válido (por ejemplo 24 o 24,5).';
    if (amountCents === null || amountCents <= 0) return 'El desembolso inicial es obligatorio y debe ser mayor que cero.';
    if (!v.date) return 'Indica la fecha del desembolso.';
    return {
      holder: v.holder.trim(),
      limitCents,
      rateEaBps,
      disbursement: { amountCents, date: v.date, description: v.description.trim() },
    };
  }
}
