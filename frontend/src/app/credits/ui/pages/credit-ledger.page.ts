import {
  ChangeDetectionStrategy,
  Component,
  DestroyRef,
  WritableSignal,
  computed,
  inject,
  input,
  signal,
} from '@angular/core';
import { takeUntilDestroyed, toObservable } from '@angular/core/rxjs-interop';
import { RouterLink } from '@angular/router';
import { Observable, catchError, forkJoin, of, switchMap } from 'rxjs';
import { IdempotentSubmission } from '../../application/idempotent-submission';
import { CreditDetail, Entry, InterestPreview, Ledger } from '../../domain/credit';
import { CREDIT_REPOSITORY } from '../../domain/credit-repository.token';
import { formatBps, formatCentsInput, formatCop, formatUsedPct, parseCents } from '../../domain/money';
import { ENTRY_LABELS, STATUS_LABELS, errorMessage, formatDate, todayIso } from '../presentation';

type Operation = 'CONSUMO' | 'PAGO' | 'LIQUIDAR';

const TABS: { key: Operation; label: string }[] = [
  { key: 'CONSUMO', label: 'Consumo' },
  { key: 'PAGO', label: 'Pago' },
  { key: 'LIQUIDAR', label: 'Liquidar interés' },
];

const HINTS: Record<Operation, string> = {
  CONSUMO: 'Registra una compra contra el cupo. Aumenta el capital adeudado.',
  PAGO: 'Abono parcial o total. Primero se liquida el interés a la fecha; el pago cubre el interés y el resto va a capital, del uso más antiguo al más nuevo.',
  LIQUIDAR: 'Liquida el interés simple devengado sobre el capital desde la última liquidación.',
};

const SUBMIT_LABELS: Record<Operation, string> = {
  CONSUMO: 'Registrar consumo',
  PAGO: 'Registrar pago',
  LIQUIDAR: 'Liquidar interés',
};

interface Row {
  id: string;
  date: string;
  type: string;
  description: string;
  detail: string;
  uses: string[];
  debit: string;
  credit: string;
  balance: string;
}

@Component({
  selector: 'app-credit-ledger-page',
  imports: [RouterLink],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: `
    .cards { display: flex; flex-wrap: wrap; gap: 1px; background: var(--line); border: 1px solid var(--line); }
    .card { background: var(--surface); padding: 22px 24px; display: flex; flex-direction: column; gap: 8px; flex: 1 1 180px; min-width: 0; }
    .card--main { background: var(--ink); color: var(--paper); flex: 2 1 360px; gap: 10px; }
    .card__label { font-size: 13px; color: var(--muted); }
    .card--main .card__label, .card__sub--on-ink { color: var(--muted-on-ink); }
    .card__big { font-family: var(--serif); font-size: 52px; line-height: 1; font-variant-numeric: tabular-nums; }
    .card__value { font-family: var(--mono); font-size: 20px; }
    .card__sub { font-size: 12px; color: var(--muted); }
    .layout { display: flex; flex-wrap: wrap; gap: 32px; align-items: flex-start; }
    .ops { flex: 1 1 300px; max-width: 400px; border: 1px solid var(--line); background: var(--surface); }
    .tabs { display: flex; border-bottom: 1px solid var(--line); }
    .tab { flex: 1; padding: 14px 6px; border: none; border-bottom: 2px solid transparent; margin-bottom: -1px; background: transparent; font-size: 14px; color: var(--muted); cursor: pointer; }
    .tab--active { border-bottom-color: var(--ink); color: var(--ink); font-weight: 600; }
    .ops__body { padding: 22px; display: flex; flex-direction: column; gap: 16px; }
    .kv { display: flex; justify-content: space-between; gap: 12px; font-size: 13px; color: var(--muted); }
    .kv span:last-child { font-family: var(--mono); color: var(--ink); white-space: nowrap; }
    .box { display: flex; flex-direction: column; gap: 8px; padding: 14px; background: var(--paper); border-radius: var(--radius); }
    .movements { flex: 999 1 560px; min-width: 0; display: flex; flex-direction: column; gap: 14px; }
  `,
  template: `
    <div class="page">
      <a class="back" routerLink="/creditos">← Créditos</a>

      @if (loadError()) {
        <div class="alert" role="alert">{{ loadError() }}</div>
      }

      @if (credit(); as c) {
        <header class="page-header">
          <div class="page-header__titles">
            <div class="eyebrow">Libro mayor · {{ c.holder }}</div>
            <h1 class="title">Crédito N.º {{ c.accountNumber }}</h1>
          </div>
          <div style="display: flex; gap: 24px; flex-wrap: wrap; align-items: flex-end">
            <div class="field">Cupo aprobado<span class="readonly-value" data-testid="limit">{{ money(c.limitCents) }}</span></div>
            <div class="field">Tasa<span class="readonly-value" data-testid="rate">{{ rate() }}% EA</span></div>
            <span class="pill" [class.pill--alert]="overdrawn()" data-testid="status">{{ statusLabel() }}</span>
          </div>
        </header>

        <section class="cards">
          <div class="card card--main">
            <div class="card__label">Saldo pendiente</div>
            <div class="card__big" data-testid="balance">{{ money(c.balanceCents) }}</div>
            <div class="card__sub card__sub--on-ink">
              Con interés devengado a hoy: <span class="mono" style="color: var(--paper)">{{ money(c.projectedBalanceCents) }}</span>
            </div>
          </div>
          <div class="card">
            <div class="card__label">Capital</div>
            <div class="card__value">{{ money(c.capitalCents) }}</div>
          </div>
          <div class="card">
            <div class="card__label">Interés por pagar</div>
            <div class="card__value">{{ money(c.interestDueCents) }}</div>
            <div class="card__sub">+ {{ money(c.accruedInterestCents) }} devengado sin liquidar</div>
          </div>
          <div class="card">
            <div class="card__label">Cupo disponible</div>
            <div class="card__value" data-testid="available">{{ money(c.availableCents) }}</div>
            <div class="bar" [class.bar--alert]="overdrawn()" data-testid="bar">
              <div class="bar__fill" [style.width.%]="c.usedBps / 100"></div>
            </div>
            <div class="card__sub">{{ used() }} utilizado</div>
          </div>
        </section>

        <div class="layout">
          <aside class="ops">
            <div class="tabs" role="tablist">
              @for (t of tabs; track t.key) {
                <button type="button" role="tab" class="tab" [class.tab--active]="tab() === t.key"
                        [attr.aria-selected]="tab() === t.key" (click)="selectTab(t.key)"
                        [attr.data-testid]="'tab-' + t.key">{{ t.label }}</button>
              }
            </div>
            <form class="ops__body" (submit)="submit($event)" novalidate>
              <p class="lead" style="font-size: 14px">{{ hint() }}</p>

              <label class="field">Fecha
                <input class="input input--mono" type="date" [value]="date()" (input)="set(date, $event)" data-testid="op-date" />
              </label>

              @if (tab() !== 'LIQUIDAR') {
                <label class="field">Monto (COP)
                  <input class="input input--big" inputmode="decimal" placeholder="0" [value]="amount()"
                         (input)="set(amount, $event)" data-testid="op-amount" />
                </label>
              }

              @if (tab() === 'PAGO') {
                <div class="box" data-testid="pay-preview">
                  <div class="kv"><span>Interés a liquidar a esa fecha</span><span>{{ previewMoney('interestToLiquidateCents') }}</span></div>
                  <div class="kv"><span>Saldo total a esa fecha</span><span data-testid="pay-total">{{ previewMoney('totalBalanceCents') }}</span></div>
                </div>
                <button type="button" class="link-btn" style="align-self: flex-start" (click)="payAll()"
                        [disabled]="!preview()" data-testid="pay-all">Pagar saldo total</button>
              }

              @if (tab() === 'LIQUIDAR') {
                <div class="box" data-testid="liq-preview">
                  <div class="kv"><span>Base (capital)</span><span>{{ previewMoney('capitalCents') }}</span></div>
                  <div class="kv"><span>Días desde última liquidación</span><span>{{ preview()?.daysSinceLastLiquidation ?? '—' }}</span></div>
                  <div class="kv" style="font-size: 15px; border-top: 1px solid var(--line); padding-top: 8px">
                    <span>Interés a liquidar</span><span data-testid="liq-amount">{{ previewMoney('interestToLiquidateCents') }}</span>
                  </div>
                  <div class="card__sub">Interés simple diario con la tasa diaria equivalente a la EA, redondeado al centavo (HALF_UP).</div>
                </div>
              }

              @if (tab() !== 'LIQUIDAR') {
                <label class="field">Descripción
                  <input class="input" [value]="description()" (input)="set(description, $event)"
                         [placeholder]="tab() === 'CONSUMO' ? 'Ej. Compra supermercado' : 'Ej. Pago cuota octubre'" />
                </label>
              }

              @if (opError()) {
                <div class="alert" role="alert" data-testid="op-error">{{ opError() }}</div>
              }

              <button class="btn" type="submit" [disabled]="submission.submitting()" data-testid="op-submit">
                {{ submitLabel() }}
              </button>
            </form>
          </aside>

          <main class="movements">
            <h2 class="subtitle">Movimientos</h2>
            <div class="table-wrap">
              <table class="table">
                <thead>
                  <tr>
                    <th>Fecha</th><th>Movimiento</th><th>Detalle</th>
                    <th class="num">Cargo</th><th class="num">Abono</th><th class="num">Saldo</th>
                  </tr>
                </thead>
                <tbody>
                  @for (r of rows(); track r.id) {
                    <tr data-testid="entry-row">
                      <td class="mono muted" style="font-size: 13px; white-space: nowrap">{{ r.date }}</td>
                      <td><span class="pill">{{ r.type }}</span></td>
                      <td>
                        <div>{{ r.description }}</div>
                        @if (r.detail) {
                          <div class="muted" style="font-size: 12px; margin-top: 2px" data-testid="entry-detail">{{ r.detail }}</div>
                        }
                        @for (u of r.uses; track u) {
                          <div class="muted mono" style="font-size: 11px" data-testid="entry-use">{{ u }}</div>
                        }
                      </td>
                      <td class="num debit">{{ r.debit }}</td>
                      <td class="num credit">{{ r.credit }}</td>
                      <td class="num" style="font-weight: 500">{{ r.balance }}</td>
                    </tr>
                  }
                </tbody>
                @if (ledger(); as l) {
                  <tfoot>
                    <tr>
                      <td colspan="3">Totales</td>
                      <td class="num" data-testid="total-debit">{{ money(l.totalDebitCents) }}</td>
                      <td class="num" data-testid="total-credit">{{ money(l.totalCreditCents) }}</td>
                      <td class="num">{{ money(c.balanceCents) }}</td>
                    </tr>
                  </tfoot>
                }
              </table>
            </div>
          </main>
        </div>
      }
    </div>
  `,
})
export class CreditLedgerPage {
  private readonly repo = inject(CREDIT_REPOSITORY);
  private readonly destroyRef = inject(DestroyRef);

  /** Id del crédito, desde la ruta (withComponentInputBinding). */
  readonly id = input.required<string>();

  protected readonly tabs = TABS;
  protected readonly money = formatCop;
  protected readonly submission = new IdempotentSubmission();

  protected readonly credit = signal<CreditDetail | null>(null);
  protected readonly ledger = signal<Ledger | null>(null);
  protected readonly preview = signal<InterestPreview | null>(null);
  protected readonly loadError = signal('');
  protected readonly opError = signal('');

  protected readonly tab = signal<Operation>('CONSUMO');
  protected readonly date = signal('');
  protected readonly amount = signal('');
  protected readonly description = signal('');
  /** Se incrementa tras cada operación para recargar saldo, historial y vista previa. */
  private readonly version = signal(0);

  protected readonly hint = computed(() => HINTS[this.tab()]);
  protected readonly submitLabel = computed(() => SUBMIT_LABELS[this.tab()]);
  protected readonly overdrawn = computed(() => this.credit()?.status === 'SOBREGIRADO');
  protected readonly statusLabel = computed(() => {
    const c = this.credit();
    return c ? STATUS_LABELS[c.status] : '';
  });
  protected readonly rate = computed(() => formatBps(this.credit()?.rateEaBps ?? 0));
  protected readonly used = computed(() => formatUsedPct(this.credit()?.usedBps ?? 0));
  protected readonly rows = computed(() => (this.ledger()?.entries ?? []).map(toRow));

  private readonly previewQuery = computed(() => {
    const tab = this.tab();
    const date = this.date();
    if (tab === 'CONSUMO' || !date || !this.credit()) return null;
    return { id: this.id(), date, version: this.version() };
  });

  constructor() {
    toObservable(computed(() => ({ id: this.id(), version: this.version() })))
      .pipe(
        switchMap(({ id }) => forkJoin({ credit: this.repo.get(id), ledger: this.repo.ledger(id) })),
        takeUntilDestroyed(),
      )
      .subscribe({
        next: ({ credit, ledger }) => {
          this.credit.set(credit);
          this.ledger.set(ledger);
          if (!this.date()) this.date.set(maxDate(todayIso(), credit.lastEntryDate));
        },
        error: (e: unknown) => this.loadError.set(errorMessage(e)),
      });

    toObservable(this.previewQuery)
      .pipe(
        switchMap((q): Observable<InterestPreview | null> =>
          q ? this.repo.preview(q.id, q.date).pipe(catchError(() => of(null))) : of(null),
        ),
        takeUntilDestroyed(),
      )
      .subscribe((p) => this.preview.set(p));
  }

  protected set(target: WritableSignal<string>, event: Event): void {
    target.set((event.target as HTMLInputElement).value);
    this.opError.set('');
  }

  protected selectTab(tab: Operation): void {
    if (tab === this.tab()) return;
    this.tab.set(tab);
    this.amount.set('');
    this.description.set('');
    this.opError.set('');
    this.submission.renew(); // otra operación = otra intención
  }

  protected previewMoney(field: 'interestToLiquidateCents' | 'totalBalanceCents' | 'capitalCents'): string {
    const p = this.preview();
    return p ? formatCop(p[field]) : '—';
  }

  protected payAll(): void {
    const p = this.preview();
    if (p) this.amount.set(formatCentsInput(p.totalBalanceCents));
  }

  protected submit(event: Event): void {
    event.preventDefault();
    const op = this.buildOperation();
    if (typeof op === 'string') {
      this.opError.set(op);
      return;
    }
    this.submission
      .submit(op)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: () => {
          this.amount.set('');
          this.description.set('');
          this.version.update((v) => v + 1);
        },
        error: (e: unknown) => this.opError.set(errorMessage(e)),
      });
  }

  private buildOperation(): ((key: string) => Observable<Entry>) | string {
    const id = this.id();
    const date = this.date();
    if (!date) return 'Indica la fecha del movimiento.';
    const tab = this.tab();
    if (tab === 'LIQUIDAR') return (key) => this.repo.liquidateInterest(id, date, key);

    const amountCents = parseCents(this.amount());
    if (amountCents === null || amountCents <= 0) return 'Ingresa un monto válido mayor que cero (por ejemplo 150.000,50).';
    const cmd = { amountCents, date, description: this.description().trim() };
    return tab === 'CONSUMO'
      ? (key) => this.repo.registerPurchase(id, cmd, key)
      : (key) => this.repo.registerPayment(id, cmd, key);
  }
}

function maxDate(a: string, b: string): string {
  return a > b ? a : b; // AAAA-MM-DD se ordena lexicográficamente
}

function toRow(e: Entry): Row {
  const isPayment = e.type === 'PAGO';
  return {
    id: e.id,
    date: formatDate(e.date),
    type: ENTRY_LABELS[e.type],
    description: e.description,
    detail: isPayment
      ? `Interés ${formatCop(e.toInterestCents ?? 0)} · Capital ${formatCop(e.toCapitalCents ?? 0)}`
      : e.detail,
    uses: (e.allocations ?? []).map((a) => `Uso #${a.useSeq}: ${formatCop(a.toCapitalCents)}`),
    debit: isPayment ? '' : formatCop(e.amountCents),
    credit: isPayment ? formatCop(e.amountCents) : '',
    balance: formatCop(e.balanceAfterCents),
  };
}
