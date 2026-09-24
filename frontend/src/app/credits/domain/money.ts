/**
 * Dinero en el cliente: siempre centavos enteros (COP). El texto ingresado se
 * convierte a centavos procesando la cadena, sin parseFloat ni aritmética
 * flotante; el formato se construye a partir de los dígitos del entero.
 */

/** Centavos de COP. Siempre un entero seguro (Number.isSafeInteger). */
export type Cents = number;

const GROUPED = /^\d{1,3}(\.\d{3})*(,\d{1,2})?$/; // 1.500.000,50
const PLAIN = /^\d+(,\d{1,2})?$/; //                 1500000,5

/**
 * Convierte un monto escrito en formato colombiano a centavos.
 * Acepta "1.500.000,50", "1500000,5", "$ 1.500.000". Devuelve null si el
 * texto no es un monto válido (negativo, más de 2 decimales, letras...).
 */
export function parseCents(text: string): Cents | null {
  const s = text.replace(/\s/g, '').replace(/^\$/, '');
  if (!GROUPED.test(s) && !PLAIN.test(s)) return null;
  const [intPart, frac = ''] = s.split(',');
  const digits = intPart.replace(/\./g, '') + frac.padEnd(2, '0');
  const cents = Number(digits); // entero decimal exacto: no hay fracción
  return Number.isSafeInteger(cents) ? cents : null;
}

/** Formatea centavos como "$ 1.500.000,50". */
export function formatCop(cents: Cents): string {
  const negative = cents < 0;
  const digits = String(Math.abs(cents)).padStart(3, '0');
  const intPart = digits.slice(0, -2);
  const frac = digits.slice(-2);
  return `${negative ? '-' : ''}$ ${groupThousands(intPart)},${frac}`;
}

/** Formatea centavos para un input editable: "1.500.000,50" (sin símbolo). */
export function formatCentsInput(cents: Cents): string {
  return formatCop(cents).replace('$ ', '');
}

/** Convierte una tasa en porcentaje ("24,5", "24.5", "24") a puntos básicos. */
export function parseRateBps(text: string): number | null {
  const s = text.replace(/\s/g, '').replace('%', '').replace('.', ',');
  if (!/^\d{1,4}(,\d{1,2})?$/.test(s)) return null;
  const [intPart, frac = ''] = s.split(',');
  return Number(intPart + frac.padEnd(2, '0'));
}

/** Formatea puntos básicos como porcentaje: 2450 -> "24,5". */
export function formatBps(bps: number): string {
  const whole = Math.trunc(bps / 100);
  const frac = String(bps % 100).padStart(2, '0').replace(/0$/, '');
  return frac === '' || frac === '0' ? String(whole) : `${whole},${frac}`;
}

/** Porcentaje utilizado (usedBps) como "62%". */
export function formatUsedPct(usedBps: number): string {
  return `${Math.round(usedBps / 100)}%`;
}

function groupThousands(intPart: string): string {
  return intPart.replace(/\B(?=(\d{3})+(?!\d))/g, '.');
}
