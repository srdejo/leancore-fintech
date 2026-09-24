import { formatBps, formatCentsInput, formatCop, parseCents, parseRateBps } from './money';

describe('parseCents', () => {
  it.each([
    ['1.500.000,50', 150000050],
    ['1500000,5', 150000050],
    ['1500000', 150000000],
    ['$ 1.500.000', 150000000],
    ['0,01', 1],
    ['0', 0],
    [' 35.000 ', 3500000],
    ['999.999.999.999,99', 99999999999999],
  ])('%s -> %d centavos', (text, cents) => {
    expect(parseCents(text)).toBe(cents);
  });

  it.each(['', 'abc', '-100', '1,234', '1.50', '1.5000', '12,', ',5', '1e3', '1.500.000.50', '10,5,3'])(
    'rechaza %j',
    (text) => {
      expect(parseCents(text)).toBeNull();
    },
  );

  it('rechaza montos fuera del rango entero seguro', () => {
    expect(parseCents('999999999999999999')).toBeNull();
  });
});

describe('formatCop', () => {
  it.each([
    [150000050, '$ 1.500.000,50'],
    [0, '$ 0,00'],
    [1, '$ 0,01'],
    [99, '$ 0,99'],
    [100, '$ 1,00'],
    [100000000, '$ 1.000.000,00'],
    [-2500, '-$ 25,00'],
  ])('%d -> %s', (cents, text) => {
    expect(formatCop(cents)).toBe(text);
  });

  it('ida y vuelta con parseCents', () => {
    for (const cents of [1, 150000050, 99999999999999]) {
      expect(parseCents(formatCop(cents))).toBe(cents);
      expect(parseCents(formatCentsInput(cents))).toBe(cents);
    }
  });
});

describe('tasa', () => {
  it.each([
    ['24,5', 2450],
    ['24.5', 2450],
    ['24', 2400],
    ['0', 0],
    ['24,05', 2405],
    ['24 %', 2400],
  ])('%s -> %d bps', (text, bps) => {
    expect(parseRateBps(text)).toBe(bps);
  });

  it.each(['', '-1', 'abc', '24,555', '12345'])('rechaza %j', (text) => {
    expect(parseRateBps(text)).toBeNull();
  });

  it.each([
    [2450, '24,5'],
    [2400, '24'],
    [2405, '24,05'],
    [0, '0'],
  ])('%d bps -> %s', (bps, text) => {
    expect(formatBps(bps)).toBe(text);
  });
});
