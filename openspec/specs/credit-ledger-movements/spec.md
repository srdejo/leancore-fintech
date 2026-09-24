# credit-ledger-movements Specification
## Purpose

Define los movimientos del libro de un crédito (consumos, pagos y liquidación de interés), las reglas que los validan, cómo afectan el saldo, y el historial append-only con su saldo corrido.

## Requirements

### Requirement: Movimientos append-only
Los movimientos (`DESEMBOLSO`, `CONSUMO`, `INTERES`, `PAGO`) MUST ser inmutables: una vez registrados no se editan ni se eliminan. Cada movimiento SHALL registrar su tipo, monto positivo en centavos, fecha, descripción y el saldo pendiente resultante tras aplicarlo. El sistema SHALL NOT ofrecer ninguna operación para deshacer o borrar movimientos.

#### Scenario: Historial con saldo corrido
- **WHEN** un crédito registra un desembolso de 500000000, un consumo de 80000000 y un pago de 120000000 sin interés pendiente
- **THEN** el historial muestra los tres movimientos con saldos resultantes 500000000, 580000000 y 460000000

#### Scenario: Orden del historial
- **WHEN** se consulta el historial de un crédito
- **THEN** los movimientos se devuelven del más reciente al más antiguo, junto con el total de cargos y el total de abonos

### Requirement: Orden cronológico de fechas
Un movimiento MUST NOT tener una fecha anterior a la del último movimiento registrado en el crédito. Se permite registrar varios movimientos con la misma fecha.

#### Scenario: Fecha anterior rechazada
- **WHEN** el último movimiento del crédito es del 2026-08-05 y se registra un consumo con fecha 2026-08-01
- **THEN** el consumo se rechaza indicando que la fecha no puede ser anterior al último movimiento (2026-08-05)

#### Scenario: Misma fecha permitida
- **WHEN** el último movimiento es del 2026-08-05 y se registra un consumo con fecha 2026-08-05
- **THEN** el consumo se registra

### Requirement: Registro de consumos
El sistema SHALL registrar consumos contra el cupo con monto, fecha y descripción opcional. Un consumo MUST tener un monto mayor que 0 y MUST NOT superar el disponible (`max(0, cupo - capital - interés por pagar)`). Un consumo aceptado SHALL aumentar el capital en su monto.

#### Scenario: Consumo dentro del disponible
- **WHEN** un crédito con cupo 1000000000, capital 400000000 e interés por pagar 0 registra un consumo de 35000000
- **THEN** el consumo se registra, el capital queda en 435000000 y el disponible en 565000000

#### Scenario: Consumo mayor al disponible
- **WHEN** un crédito con disponible 25000000 registra un consumo de 30000000
- **THEN** el consumo se rechaza indicando fondos insuficientes y el disponible actual (25000000)

#### Scenario: Consumo con monto no positivo
- **WHEN** se registra un consumo con monto 0 o negativo
- **THEN** el consumo se rechaza indicando que el monto debe ser mayor que cero

### Requirement: Sobregiro por interés
La liquidación de interés MUST aplicarse aunque el saldo pendiente resultante supere el cupo. Mientras el saldo pendiente supere el cupo, el crédito SHALL considerarse sobregirado, con disponible 0, y MUST rechazar nuevos consumos.

#### Scenario: Interés que sobregira
- **WHEN** un crédito con cupo 1000000000 y saldo pendiente 999500000 liquida interés por 800000
- **THEN** la liquidación se registra, el saldo pendiente queda en 1000300000, el disponible en 0 y el estado en `SOBREGIRADO`

#### Scenario: Consumo en sobregiro
- **WHEN** un crédito sobregirado registra un consumo de 1000
- **THEN** el consumo se rechaza indicando fondos insuficientes

#### Scenario: Pago que regulariza
- **WHEN** un crédito sobregirado con cupo 1000000000 y saldo 1000300000 recibe un pago de 5000000
- **THEN** el saldo queda en 995300000, el disponible en 4700000 y el estado deja de ser `SOBREGIRADO`

### Requirement: Registro de pagos
El sistema SHALL registrar pagos parciales o totales con monto, fecha y descripción opcional. Al registrar un pago, el sistema MUST primero liquidar el interés devengado hasta la fecha del pago (registrando un movimiento `INTERES` si el monto liquidado es mayor que 0), y después aplicar el pago: primero al interés por pagar y el remanente a capital. El monto del pago MUST ser mayor que 0 y MUST NOT superar el saldo pendiente calculado después de esa liquidación. No existe saldo a favor.

#### Scenario: Pago que cubre interés y capital
- **WHEN** un crédito con capital 580000000 e interés por pagar 0 recibe un pago de 120000000 en una fecha cuyo devengo liquida 11000000 de interés
- **THEN** se registra un movimiento `INTERES` de 11000000 y luego el `PAGO` de 120000000
- **AND** el pago se aplica 11000000 a interés y 109000000 a capital, que queda en 471000000

#### Scenario: Pago que solo alcanza para interés
- **WHEN** un crédito con interés por pagar 5000000 (después de liquidar) recibe un pago de 3000000
- **THEN** el pago se aplica completo a interés, el interés por pagar queda en 2000000 y el capital no cambia

#### Scenario: Pago del saldo total
- **WHEN** un crédito recibe un pago igual a su capital más el interés por pagar después de liquidar a la fecha del pago
- **THEN** el pago se registra y el saldo pendiente queda en 0

#### Scenario: Pago mayor al saldo
- **WHEN** un crédito con saldo pendiente 471000000 (después de liquidar) recibe un pago de 471000001
- **THEN** el pago se rechaza indicando que supera el saldo pendiente (471000000)
- **AND** no se registra ningún movimiento, incluido el de interés

#### Scenario: Pago con monto no positivo
- **WHEN** se registra un pago con monto 0 o negativo
- **THEN** el pago se rechaza indicando que el monto debe ser mayor que cero

### Requirement: Aplicación FIFO del capital
La porción de un pago que se aplica a capital MUST repartirse entre los usos (desembolso y consumos) con capital pendiente, del uso más antiguo al más nuevo, hasta agotarse. El sistema SHALL registrar y exponer, para cada pago, cuánto se aplicó a interés y cuánto a capital de cada uso.

#### Scenario: Pago que cierra un uso y abona el siguiente
- **WHEN** un crédito tiene el uso #1 (desembolso) con 40000000 pendientes, el uso #2 (consumo) con 35000000 y el uso #3 (consumo) con 10000000, sin interés por pagar, y recibe un pago de 50000000
- **THEN** el uso #1 queda en 0, el uso #2 en 25000000 y el uso #3 en 10000000
- **AND** el detalle del pago muestra 40000000 al uso #1 y 10000000 al uso #2

### Requirement: Liquidación manual de interés a una fecha
El sistema SHALL permitir liquidar el interés devengado hasta una fecha dada, registrando un movimiento `INTERES` que aumenta el interés por pagar. El sistema SHALL ofrecer además una vista previa, sin efectos, con la base de capital, los días transcurridos desde la última liquidación y el monto que se liquidaría. Si el monto a liquidar es 0, la liquidación MUST rechazarse.

#### Scenario: Vista previa sin efectos
- **WHEN** se pide la vista previa de liquidación de un crédito a la fecha 2026-09-01
- **THEN** la respuesta muestra base de capital, días desde la última liquidación e interés a liquidar
- **AND** no se registra ningún movimiento

#### Scenario: Liquidación exitosa
- **WHEN** se liquida el interés de un crédito a una fecha con devengo mayor que 0
- **THEN** se registra un movimiento `INTERES` por el monto liquidado y el interés por pagar aumenta en ese monto

#### Scenario: Nada que liquidar
- **WHEN** se liquida el interés de un crédito a la misma fecha de su última liquidación, o de un crédito sin capital
- **THEN** la liquidación se rechaza indicando que no hay interés devengado para liquidar

#### Scenario: Pago tras una liquidación manual del mismo día
- **WHEN** se liquida interés manualmente el 2026-09-01 y después se registra un pago con fecha 2026-09-01
- **THEN** el pago no genera un nuevo movimiento `INTERES` y se aplica contra el interés ya liquidado
