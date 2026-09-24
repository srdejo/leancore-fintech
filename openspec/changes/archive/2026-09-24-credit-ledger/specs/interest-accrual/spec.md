# Spec Delta

## Purpose

Define cómo se calcula el interés simple de un crédito a partir de su tasa efectiva anual: la tasa diaria equivalente, el devengo exacto sobre el capital y el redondeo al centavo al liquidar.

## ADDED Requirements

### Requirement: Tasa diaria equivalente a la EA
La tasa de un crédito SHALL expresarse como efectiva anual (EA) en puntos básicos. Al abrir el crédito, el sistema MUST derivar la tasa diaria equivalente `(1 + EA)^(1/365) - 1`, representarla como un entero escalado por 10^15 redondeado HALF_UP, y conservarla sin cambios durante toda la vida del crédito. Todos los cálculos de interés del crédito MUST usar esa tasa diaria almacenada.

#### Scenario: Derivación para 24% EA
- **WHEN** se abre un crédito con tasa 2400 bps
- **THEN** la tasa diaria almacenada es `round_half_up(((1,24)^(1/365) - 1) × 10^15)`, aproximadamente 589 × 10^9

#### Scenario: Tasa cero
- **WHEN** se abre un crédito con tasa 0 bps
- **THEN** la tasa diaria almacenada es 0 y el crédito nunca devenga interés

### Requirement: Devengo simple diario sobre capital
El interés MUST devengarse de forma simple, día a día, solo sobre el capital pendiente y nunca sobre el interés por pagar. Cada vez que el capital cambia, el sistema SHALL acumular el devengo del tramo transcurrido como `capital (centavos) × tasa diaria escalada × días calendario` en unidades enteras exactas, sin redondeo intermedio. Los días se cuentan como diferencia de días calendario entre fechas, con un año de 365 días.

#### Scenario: Devengo por tramos
- **WHEN** un crédito tiene capital 40000000 entre el día 1 y el día 5, capital 75000000 entre el día 5 y el día 10, y se liquida el día 10
- **THEN** el devengo acumulado es `40000000 × r × 4 + 75000000 × r × 5` unidades, donde `r` es la tasa diaria escalada

#### Scenario: El interés por pagar no genera interés
- **WHEN** un crédito tiene capital 0 e interés por pagar 5000000 durante 30 días
- **THEN** no se devenga interés en ese periodo

### Requirement: Liquidación al centavo con HALF_UP
Al liquidar, el sistema MUST convertir el devengo acumulado a centavos dividiendo por 10^15 y redondeando HALF_UP (la fracción de 0,5 centavos o más sube un centavo; la menor se descarta). Después de liquidar, el devengo acumulado MUST reiniciarse a 0: la fracción descartada no se traslada a liquidaciones futuras.

#### Scenario: Fracción menor a medio centavo
- **WHEN** el devengo acumulado equivale a 328764,23 centavos
- **THEN** se liquidan 328764 centavos y el devengo acumulado queda en 0

#### Scenario: Fracción de medio centavo o más
- **WHEN** el devengo acumulado equivale a 328764,50 centavos
- **THEN** se liquidan 328765 centavos y el devengo acumulado queda en 0

#### Scenario: Liquidaciones sucesivas no arrastran residuo
- **WHEN** una liquidación descarta una fracción de 0,4 centavos y el siguiente periodo devenga exactamente 1000 centavos
- **THEN** la segunda liquidación es de 1000 centavos
