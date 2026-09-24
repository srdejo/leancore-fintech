# Proposal

## Why

Se necesita un ledger (libro contable) simple para una cuenta de crédito que soporte desembolso inicial, consumos, pagos parciales, cálculo del saldo pendiente e interés simple, manejando el dinero con precisión exacta (sin punto flotante) y de forma segura ante concurrencia y reintentos. El repositorio está vacío; este cambio establece el producto completo: dominio, API, persistencia, interfaz y entorno ejecutable con un solo comando.

## What Changes

- Nuevo **cupo rotativo**: cada crédito tiene un titular, un cupo aprobado y una tasa efectiva anual (EA) fijados al abrirlo. El desembolso inicial es obligatorio, único y no puede superar el cupo.
- Nuevos **movimientos** append-only: `DESEMBOLSO`, `CONSUMO`, `INTERES` y `PAGO`. Nunca se editan ni se borran, y ninguno puede tener una fecha anterior al último movimiento.
- **Saldo materializado** en la cuenta de crédito (capital, interés por pagar y devengo acumulado), actualizado en la misma transacción que cada movimiento.
- **Consumos** validados contra el disponible (`cupo - capital - interés`).
- **Pagos parciales** que primero causan el interés pendiente, luego cubren el interés y el resto se aplica a capital en orden FIFO (del uso más antiguo al más nuevo). Se rechaza un pago mayor al saldo total; no existe saldo a favor.
- **Liquidación manual de interés a una fecha**, con vista previa.
- **Interés simple diario** con la tasa diaria equivalente a la EA, derivada y congelada al abrir. El devengo se acumula en unidades enteras y se liquida al centavo con redondeo HALF_UP, descartando la fracción.
- **Sobregiro**: el interés puede llevar el saldo por encima del cupo. En ese caso la cuenta queda sobregirada y se bloquean nuevos consumos.
- **Dinero en centavos enteros** (`int64`) en la base de datos, el backend, la API y el frontend. Solo COP.
- **Concurrencia**: bloqueo pesimista (`SELECT ... FOR UPDATE`) sobre la fila del saldo de cada crédito.
- **Idempotencia**: cada comando lleva un `Idempotency-Key` (UUID) que el frontend genera al preparar el formulario. El backend lo verifica dentro de la transacción del bloqueo.
- **API REST** en Go con arquitectura hexagonal (puertos y adaptadores).
- **Frontend Angular**: listado de créditos, creación de crédito y libro mayor, a partir del diseño "Ledger Crédito" ajustado.
- **Monorepo dockerizado**: `docker compose up` levanta PostgreSQL, el backend y el frontend.

### Fuera de alcance (extensiones futuras)

- Facturación: fecha de corte, extracto congelado, pago mínimo, ventana de pago y aplicación del excedente.
- Reconciliación de saldos: test de replay, endpoint de verificación y cierre diario.
- Partida doble real.
- Tasa de usura y topes regulatorios.
- Mora y cargos por pago tardío.
- Tasas o cuotas por uso.
- Múltiples monedas.

## Capabilities

### New Capabilities

- `credit-lines`: apertura de créditos (titular, cupo, tasa EA, desembolso inicial obligatorio), listado y consulta del estado de un crédito (saldo, capital, interés, disponible, estado).
- `credit-ledger-movements`: registro de consumos, pagos (interés primero y capital FIFO) y liquidación de interés; reglas de fecha, disponible, sobregiro y rechazo de pagos excedentes; historial de movimientos con su saldo corrido.
- `interest-accrual`: conversión de EA a tasa diaria equivalente, devengo simple diario sobre el capital en unidades enteras y liquidación al centavo con HALF_UP.
- `transaction-safety`: representación exacta del dinero en centavos, serialización de operaciones concurrentes sobre un mismo crédito e idempotencia de comandos.
- `credit-ledger-ui`: pantallas de listado, nuevo crédito y libro mayor, con generación de la llave de idempotencia y el manejo de dinero sin punto flotante en el cliente.

### Modified Capabilities

(ninguna: no existen specs previas)

## Impact

- **Código nuevo**: `backend/` (Go: `domain`, `application` con puertos, `adapters` HTTP y PostgreSQL, migraciones), `frontend/` (Angular) y `docker-compose.yml` en la raíz.
- **API**: nuevos endpoints REST bajo `/api/credits`.
- **Dependencias**: PostgreSQL, `pgx`, `golang-migrate`, la biblioteca estándar `net/http` y nginx para servir el frontend y hacer de proxy hacia `/api`.
- **Datos**: tablas `credit_lines`, `entries` y `payment_allocations`.
