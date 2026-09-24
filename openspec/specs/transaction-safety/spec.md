# transaction-safety Specification
## Purpose

Garantiza que el dinero se represente de forma exacta y que las operaciones sobre un crédito sean correctas ante concurrencia y reintentos: sin punto flotante, sin carreras sobre el saldo y sin movimientos duplicados.

## Requirements

### Requirement: Dinero en centavos enteros
Todo monto de dinero (cupo, montos de movimientos, capital, interés, saldos y asignaciones) MUST representarse como un número entero de centavos de peso colombiano (COP) en la API, en el almacenamiento y en los cálculos. El sistema MUST NOT usar punto flotante para montos. La API MUST rechazar montos que no sean enteros.

#### Scenario: Monto en centavos
- **WHEN** se registra un consumo de 150000050 centavos
- **THEN** el movimiento queda registrado por exactamente 150000050 centavos ($1.500.000,50)

#### Scenario: Monto no entero rechazado
- **WHEN** se envía a la API un monto con parte decimal, como 1500.5
- **THEN** la solicitud se rechaza indicando que el monto debe ser un entero en centavos

### Requirement: Operaciones serializadas por crédito
Las operaciones que modifican un crédito (consumo, pago, liquidación de interés) MUST ejecutarse de forma serializada por crédito: cada operación valida y actualiza el saldo sin que otra operación concurrente sobre el mismo crédito lea un saldo desactualizado. Las operaciones sobre créditos distintos SHALL NOT bloquearse entre sí. El saldo del crédito y el movimiento que lo modifica MUST persistirse de forma atómica.

#### Scenario: Consumos concurrentes que excederían el cupo
- **WHEN** un crédito con disponible 40000000 recibe al mismo tiempo dos consumos de 30000000
- **THEN** exactamente uno se registra y el otro se rechaza por fondos insuficientes
- **AND** el disponible final es 10000000

#### Scenario: Pagos concurrentes que excederían el saldo
- **WHEN** un crédito con saldo pendiente 50000000 recibe al mismo tiempo dos pagos de 30000000
- **THEN** exactamente uno se registra y el otro se rechaza por superar el saldo pendiente

#### Scenario: Fallo a mitad de operación
- **WHEN** ocurre un error después de validar un pago y antes de terminar de persistirlo
- **THEN** no queda registrado ningún movimiento de esa operación y el saldo del crédito no cambia

### Requirement: Idempotencia de comandos
Todo comando que crea o modifica datos (apertura de crédito, consumo, pago, liquidación de interés) MUST incluir una llave de idempotencia. Si llega un comando con una llave ya procesada con éxito y con el mismo contenido, el sistema MUST devolver el resultado original sin volver a ejecutarlo. Si la llave ya fue procesada con contenido distinto, el sistema MUST rechazar el comando. Un comando rechazado por reglas de negocio MUST NOT consumir la llave. Las llaves de los comandos sobre un crédito son únicas por crédito; las de apertura son únicas globalmente.

#### Scenario: Reintento del mismo pago
- **WHEN** se envía dos veces un pago de 50000000 con la misma llave y el mismo contenido
- **THEN** se registra un solo pago y ambas respuestas contienen el mismo resultado

#### Scenario: Llave reutilizada con otro contenido
- **WHEN** se procesó un pago de 50000000 con la llave K1 y luego llega un pago de 60000000 con la llave K1
- **THEN** el segundo comando se rechaza indicando que la llave ya fue usada con otros datos
- **AND** no se registra un nuevo movimiento

#### Scenario: Reintento tras un rechazo de negocio
- **WHEN** un pago con la llave K1 se rechaza por superar el saldo y luego se envía con la llave K1 un pago válido
- **THEN** el pago válido se registra

#### Scenario: Comando sin llave
- **WHEN** se envía un comando sin llave de idempotencia
- **THEN** el comando se rechaza indicando que la llave es obligatoria

#### Scenario: Reintentos concurrentes con la misma llave
- **WHEN** dos solicitudes idénticas con la misma llave llegan al mismo tiempo
- **THEN** se registra un solo movimiento y ambas respuestas devuelven el mismo resultado
