# Spec Delta

## Purpose

Permite abrir créditos de cupo rotativo con un titular, un cupo y una tasa efectiva anual fijos, registrar el desembolso inicial obligatorio, y listar y consultar el estado de cada crédito.

## ADDED Requirements

### Requirement: Apertura de crédito con desembolso inicial
El sistema SHALL permitir abrir un crédito indicando titular, cupo aprobado (centavos), tasa efectiva anual (puntos básicos) y un desembolso inicial con monto, fecha y descripción opcional. La apertura y el desembolso inicial MUST registrarse de forma atómica: o se crean ambos, o ninguno. El sistema SHALL asignar al crédito un número de cuenta único.

#### Scenario: Apertura exitosa
- **WHEN** se abre un crédito con titular "María Fernanda Ruiz", cupo 1000000000, tasa 2400 bps y desembolso de 500000000 el 2026-06-01
- **THEN** el crédito queda creado con un número de cuenta único, capital 500000000, interés por pagar 0 y disponible 500000000
- **AND** el historial contiene un único movimiento `DESEMBOLSO` de 500000000 con fecha 2026-06-01

#### Scenario: Desembolso por el cupo completo
- **WHEN** se abre un crédito con cupo 1000000000 y desembolso de 1000000000
- **THEN** el crédito queda creado con disponible 0

#### Scenario: Desembolso mayor al cupo
- **WHEN** se abre un crédito con cupo 1000000000 y desembolso de 1000000001
- **THEN** la apertura se rechaza indicando que el desembolso supera el cupo aprobado
- **AND** no se crea el crédito ni ningún movimiento

#### Scenario: Datos obligatorios inválidos
- **WHEN** se abre un crédito sin titular, con cupo menor o igual a 0, con desembolso menor o igual a 0, sin fecha de desembolso o con tasa negativa
- **THEN** la apertura se rechaza indicando el campo inválido

### Requirement: Condiciones fijas tras la apertura
El cupo aprobado y la tasa efectiva anual de un crédito MUST quedar fijos desde su apertura. El sistema SHALL NOT ofrecer ninguna operación que los modifique.

#### Scenario: Consulta de condiciones
- **WHEN** se consulta un crédito abierto con cupo 1000000000 y tasa 2400 bps
- **THEN** la respuesta muestra cupo 1000000000 y tasa 2400 bps como valores de solo lectura

### Requirement: Un único desembolso por crédito
Cada crédito MUST tener exactamente un movimiento `DESEMBOLSO`, el registrado en la apertura. Los cargos posteriores contra el cupo SHALL registrarse como consumos.

#### Scenario: No existe un segundo desembolso
- **WHEN** se inspeccionan las operaciones disponibles sobre un crédito abierto
- **THEN** solo están disponibles consumo, pago y liquidación de interés

### Requirement: Listado de créditos
El sistema SHALL listar todos los créditos con titular, número de cuenta, cupo, saldo pendiente, disponible y estado. El estado MUST ser uno de: `SOBREGIRADO` si el saldo pendiente supera el cupo, `SIN_SALDO` si el saldo pendiente es 0, o `AL_DIA` en cualquier otro caso.

#### Scenario: Listado con créditos en distintos estados
- **WHEN** existen un crédito con saldo 621438000 y cupo 1000000000, otro con saldo 501240000 y cupo 500000000, y otro con saldo 0
- **THEN** el listado los muestra con estados `AL_DIA`, `SOBREGIRADO` y `SIN_SALDO` respectivamente

#### Scenario: Sin créditos
- **WHEN** no existe ningún crédito
- **THEN** el listado se devuelve vacío

### Requirement: Consulta del estado de un crédito
El sistema SHALL devolver, para un crédito, sus condiciones y su saldo: capital, interés por pagar, saldo pendiente (capital + interés por pagar), disponible (`max(0, cupo - saldo pendiente)`), porcentaje utilizado y estado. La consulta SHALL incluir además el interés devengado sin liquidar hasta la fecha actual y el saldo proyectado con ese interés, sin registrar ningún movimiento ni modificar el saldo.

#### Scenario: Consulta con interés devengado
- **WHEN** se consulta un crédito con capital 615000000, interés por pagar 6438000 y devengo pendiente equivalente a 3890000 a la fecha actual
- **THEN** la respuesta muestra saldo pendiente 621438000, interés devengado sin liquidar 3890000 y saldo proyectado 625328000
- **AND** el historial de movimientos no cambia

#### Scenario: Crédito inexistente
- **WHEN** se consulta un crédito con un identificador que no existe
- **THEN** el sistema responde que el crédito no fue encontrado
