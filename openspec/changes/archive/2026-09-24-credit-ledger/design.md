# Design

## Context

El repositorio está vacío (ver proposal.md, sección Why). Existe un prototipo de interfaz ("Ledger Crédito", claude.ai/design) con toda la lógica en el cliente, usando `float` y `localStorage`. De ese prototipo se conservan la paleta, las tipografías y la disposición; las reglas pasan al backend. Restricciones: backend en Go, frontend en Angular, monorepo, y todo el sistema debe levantarse con `docker compose up`. Los requisitos de comportamiento están en `specs/`.

## Goals / Non-Goals

**Goals:**
- Un dominio puro en Go, sin dependencias de infraestructura, que concentre todas las reglas y sea verificable con tests unitarios.
- Aritmética de dinero e interés 100% entera y determinista.
- Consistencia ante concurrencia y reintentos garantizada por la base de datos, no por la memoria del proceso.
- Frontend delgado: muestra datos y envía comandos. Las vistas previas también se piden al backend, de modo que no hay reglas duplicadas.

**Non-Goals:**
- Autenticación y autorización.
- Alta disponibilidad, varias réplicas y observabilidad avanzada.
- Todo lo listado en proposal.md, sección "Fuera de alcance".

## Decisions

### D1. Modelo de datos: ledger simplificado con saldo materializado (no partida doble)

Se usan movimientos append-only sobre una sola cuenta (la del cliente) y el saldo se materializa en la fila del crédito.

```
credit_lines                         entries (append-only)              payment_allocations
------------------------------       -------------------------------    ----------------------------
id              UUID PK              id               UUID PK           payment_entry_id  FK entries
account_number  TEXT UNIQUE          credit_line_id   FK                use_entry_id      FK entries
holder          TEXT                 seq              INT               to_capital_cents  BIGINT
limit_cents     BIGINT               type             TEXT              PK(payment, use)
rate_ea_bps     INT                  amount_cents     BIGINT > 0
daily_rate_e15  BIGINT               entry_date       DATE              (la porción a interés va en
capital_cents   BIGINT               description      TEXT               entries.to_interest_cents
interest_due_cents BIGINT            to_interest_cents BIGINT NULL       del PAGO)
accrual_units   NUMERIC(38,0)        balance_after_cents BIGINT
last_accrual_date DATE               idempotency_key  UUID NULL
last_entry_date DATE                 request_hash     TEXT NULL
created_at      TIMESTAMPTZ          created_at       TIMESTAMPTZ
                                     UNIQUE(credit_line_id, seq)
                                     UNIQUE(credit_line_id, idempotency_key)
```

- La apertura guarda su llave en `credit_lines.creation_idempotency_key UUID UNIQUE` (junto con su `request_hash`).
- El capital pendiente de cada uso se calcula como `amount - SUM(allocations.to_capital)`, bajo el mismo bloqueo. Así no hay estado mutable por uso y los movimientos nunca se editan.
- `balance_after_cents` alimenta la columna "Saldo" del historial.
- Agregado en la implementación: `credit_lines.anchor_date` (fecha de la última liquidación, para "días desde la última liquidación") y `credit_lines.last_seq` (siguiente `seq` sin `MAX()` bajo el bloqueo). Además hay un adaptador `adapters/out/memory` (repositorio en memoria) que usan las pruebas de los casos de uso y de HTTP.

**Por qué no partida doble:** hay una sola cuenta y una sola contraparte, y el requerimiento es "cuánto debe el cliente". Las cuentas contables adicionales (caja, ingresos por intereses, por pagar a comercios) no tendrían ningún consumidor en el sistema, así que agregarlas sería YAGNI. Cada tipo de movimiento corresponde a un asiento fijo (por ejemplo, un CONSUMO es débito a cartera y crédito a comercios; un INTERES, débito a cartera y crédito a ingresos), así que migrar a partida doble después es mecánico y no pierde información. La UI ya presenta la lectura Cargo/Abono.

**Por qué materializar el saldo:** permite validar el disponible y bloquear una sola fila sin recorrer el historial en cada operación, y le da al bloqueo pesimista un objeto concreto. El costo es mantener el saldo y los movimientos coherentes, y eso se garantiza escribiendo ambos en la misma transacción. La reconciliación automática (replay contra el saldo materializado) queda como extensión futura.

Alternativas descartadas: un saldo solo derivado (hay que recorrer todo el historial en cada comando y no hay una fila natural que bloquear) y la partida doble completa (YAGNI).

### D2. Concurrencia: bloqueo pesimista por crédito

Todo comando sigue el mismo flujo transaccional:

```
BEGIN
  SELECT ... FROM credit_lines WHERE id = $1 FOR UPDATE      -- serializa por crédito
  buscar idempotency_key en entries del crédito              -- D4
  cargar usos con capital pendiente (solo para pagos)
  dominio: devengar -> validar -> aplicar -> resultado
  INSERT entries (+ INTERES automático) (+ payment_allocations)
  UPDATE credit_lines SET capital, interest_due, accrual_units, last_*
COMMIT
```

La apertura no necesita `FOR UPDATE`: inserta una fila nueva, y la unicidad de la llave de apertura la protege.

En la arquitectura hexagonal el bloqueo queda detrás de un puerto de salida. La aplicación no conoce SQL:

```go
type CreditLineRepository interface {
    WithLock(ctx context.Context, id CreditLineID, fn func(tx LockedCreditLine) error) error
    // ...
}
```

Alternativas descartadas:
- **Bloqueo optimista con columna `version`:** es redundante con `FOR UPDATE`, porque nadie escribe el saldo sin el bloqueo. Además exige lógica de reintentos y devuelve 409 al cliente en operaciones normales. No se incluye.
- **Mutex en Go:** no funciona con más de una instancia del backend.
- **Aislamiento SERIALIZABLE:** también requiere reintentos por fallos de serialización y es menos explícito.

### D3. Dinero, tasa e interés: todo entero

| Concepto | Representación | Almacenamiento |
|---|---|---|
| Montos y saldos | `type Money int64` en centavos COP | `BIGINT` |
| Tasa pactada | `rate_ea_bps int` (24,5% = 2450) | `INTEGER` |
| Tasa diaria | `daily_rate_e15 int64` = `round_half_up(((1+EA)^(1/365) - 1) × 10^15)` | `BIGINT` |
| Devengo | `accrual_units *big.Int` = Σ `capital_cents × daily_rate_e15 × días` | `NUMERIC(38,0)` |
| API | enteros en JSON (`amountCents`, `rateEaBps`) | — |

- **Tasa diaria:** la raíz 365 no tiene resultado entero exacto. Se calcula **una sola vez al abrir el crédito** con `math/big.Float` de 256 bits de precisión, se redondea HALF_UP a un entero escalado por 10^15 y se guarda. Nunca se recalcula, así que el resultado es determinista y auditable. Es la única operación no entera del sistema, y se aplica a una constante de tasa, nunca a dinero.
- **Devengo:** en cada movimiento se acumula el tramo `capital × daily_rate_e15 × días` desde `last_accrual_date`. El producto puede superar `int64` (del orden de 10^26), por eso se usa `big.Int` y `NUMERIC(38,0)`.
- **Liquidación:** `interés_cents = round_half_up(accrual_units / 10^15)`; después, `accrual_units = 0` (la fracción se descarta, con un error de hasta ±0,5 centavos por liquidación).
- **Por qué HALF_UP y no configurable:** es la convención comercial más común y fácil de explicar a un cliente o a un auditor. Es prácticamente neutral: solo favorece al comercio en el caso exacto de 0,5. Descartar la fracción hace que cada liquidación quede cerrada y sin estado oculto. Se evaluó hacerlo configurable (TRUNCATE, HALF_EVEN, CEILING) por `.env` o por crédito, pero se descartó: su impacto económico es de centavos y la configuración agrega complejidad sin un consumidor real. Si hiciera falta en el futuro, el punto de extensión es una única función del dominio.
- **EA con interés simple:** el interés no se capitaliza (se calcula solo sobre capital), así que lo cobrado en un año queda igual o ligeramente por debajo de la EA pactada. Es una desviación conservadora, a favor del cliente, y es intencional.
- **Días:** diferencia de días calendario, con un año de 365 días (también en años bisiestos).

Alternativas descartadas: `float64` (prohibido), `shopspring/decimal` (innecesario si todo es entero; agregaría una dependencia), pesos sin centavos (el requerimiento pide dos decimales) y tasa nominal ÷ 365 (en Colombia la convención es EA).

### D4. Idempotencia

- El header `Idempotency-Key` (UUID) es obligatorio en `POST`. Si falta: 400.
- Dentro de la transacción de D2, después del `FOR UPDATE`, se busca la llave. Si existe y el `request_hash` (SHA-256 del cuerpo canónico) coincide, se reconstruye y devuelve la respuesta original con el mismo status. Si el hash difiere: 422 `IDEMPOTENCY_KEY_REUSED`.
- Como la búsqueda ocurre bajo el bloqueo del crédito, dos reintentos concurrentes con la misma llave se serializan: el segundo encuentra la llave del primero. `UNIQUE(credit_line_id, idempotency_key)` es la red de seguridad.
- La llave se guarda en el movimiento principal del comando (el `PAGO`, no el `INTERES` automático). Si el comando se rechaza, la transacción hace rollback y la llave no queda consumida.
- Para la apertura, un conflicto de `UNIQUE` en `creation_idempotency_key` se resuelve releyendo el crédito existente y comparando el hash.
- No se usa una tabla aparte de respuestas: la respuesta se reconstruye a partir del movimiento y del crédito (KISS).

Del lado del frontend, la llave representa una intención y no un clic:

```
preparar formulario -> key = crypto.randomUUID()
enviar (N veces)    -> misma key; botón deshabilitado en vuelo (exhaustMap)
respuesta 2xx       -> key nueva
error red / 4xx     -> se conserva la key
```

### D5. Arquitectura hexagonal del backend

```
backend/
  cmd/api/main.go                 composición: config, pool, adapters, casos de uso
  internal/
    domain/                       sin imports de infraestructura
      money.go                    Money (int64 centavos), operaciones seguras
      rate.go                     EA bps -> daily_rate_e15 (big.Float, una vez)
      accrual.go                  devengo por tramos (big.Int) + HALF_UP
      creditline.go               agregado: Open, Purchase, Pay, LiquidateInterest,
                                  Preview; reglas de fecha, disponible y sobregiro
      allocation.go               reparto FIFO de capital por uso
      errors.go                   errores de dominio tipados (códigos estables)
    application/
      ports/in.go                 OpenCredit, RegisterPurchase, RegisterPayment,
                                  LiquidateInterest, GetCredit, ListCredits,
                                  ListEntries, PreviewAt
      ports/out.go                CreditLineRepository (WithLock), Clock, IDGenerator
      usecases/*.go               orquestan: lock -> idempotencia -> dominio -> persistir
    adapters/
      in/http/                    handlers net/http (Go 1.22 routing), DTOs, errores
      out/postgres/               pgx; SELECT FOR UPDATE; mapeo NUMERIC <-> big.Int
      out/clock/                  reloj del sistema (fecha "hoy" para proyecciones)
  migrations/                     golang-migrate (SQL)
```

La regla de dependencias es `adapters -> application -> domain`. El dominio no conoce HTTP, SQL ni `time.Now()`: `Clock` se inyecta para que las proyecciones "a hoy" se puedan probar.

**API REST**

| Método | Ruta | Uso |
|---|---|---|
| POST | `/api/credits` | abrir crédito con desembolso inicial |
| GET | `/api/credits` | listado |
| GET | `/api/credits/{id}` | estado + proyección a hoy |
| GET | `/api/credits/{id}/entries` | historial (con asignaciones de pagos) |
| GET | `/api/credits/{id}/preview?date=YYYY-MM-DD` | interés a liquidar, base, días, saldo total a la fecha |
| POST | `/api/credits/{id}/purchases` | consumo |
| POST | `/api/credits/{id}/payments` | pago |
| POST | `/api/credits/{id}/interest-liquidations` | liquidar interés a una fecha |

- Formato de error: `{"code": "INSUFFICIENT_AVAILABLE", "message": "...", "details": {...}}`.
- Códigos HTTP: 400 (formato, llave faltante, monto no entero), 404 (crédito inexistente), 422 (regla de negocio o llave reutilizada), 201 (creado) y 200 (reintento idempotente, mismo cuerpo).
- La decodificación JSON usa `json.Decoder.UseNumber()` y valida que los montos sean enteros, para no pasar nunca por `float64`.

Librerías: la estándar `net/http`, `jackc/pgx/v5`, `golang-migrate/migrate` y `google/uuid`. Son mantenidas, gratuitas y suficientes; no hace falta un framework web.

### D6. Frontend Angular

- Angular reciente con componentes standalone, signals y un diseño hexagonal pragmático por feature:

```
frontend/src/app/credits/
  domain/          modelos (Credit, Entry; montos como number entero en centavos),
                   parseo y formato de dinero
  application/     casos de uso / fachadas con signals (estado de pantalla)
  infrastructure/  adaptador HTTP (CreditsApi); la llave de idempotencia la pasa
                   explícitamente cada formulario (sin interceptor global)
  ui/              pages: credit-list, credit-new, credit-ledger; componentes de tabs
```

- **Money en el cliente:** los montos llegan como enteros en centavos (el máximo seguro de JS, 2^53, equivale a unos 90 billones de pesos, suficiente). El parseo del input es por texto: se quitan los separadores de miles `.`, la `,` se toma como separador decimal y se forman los centavos con strings. Nunca se usa `parseFloat`. Para mostrar se usa `Intl.NumberFormat('es-CO', {style:'currency', currency:'COP', minimumFractionDigits: 2})` sobre `cents / 100` expresado como string (parte entera y centavos por separado), no sobre un float calculado.
- La llave de idempotencia está en el estado del formulario (un signal), como se describe en D4.
- **Sistema visual del diseño original:** fondo `#f3efe6`, tinta `#1c1b18`, superficie `#fbf9f4`, líneas `#d9d2c3`/`#cfc7b6`, texto apagado `#6b665c`, verde `#2f6b52` (abonos, barra y hover), rojo `#9a3b1f` sobre `#f6e4db` (cargos, errores y sobregiro). Tipografías: Instrument Serif (títulos y saldo destacado), Instrument Sans (texto) y JetBrains Mono (cifras, fechas y número de cuenta). Contenedor de 1240px, encabezados con borde inferior de 2px en tinta, labels en mayúsculas espaciadas y radios de 4px.
- **Ajustes al prototipo:** se agrega el listado y la navegación "← Créditos". Cupo y tasa pasan a solo lectura. Se quitan "Deshacer último", "Cargar ejemplo", el checkbox "liquidar antes del pago", el orden "Capital primero" y el selector de moneda. La pestaña "Interés" pasa a llamarse "Liquidar interés". El disponible descuenta el interés. Se muestra el estado Sobregirado, el detalle FIFO en cada pago y la tasa etiquetada como "EA".

```
  LISTADO                         NUEVO CRÉDITO                  LIBRO MAYOR
  +---------------------------+   +------------------------+    +----------------------------------+
  | Créditos   [+ Nuevo]      |   | <- Créditos            |    | <- Créditos                      |
  |===========================|   | Nuevo crédito          |    | Crédito N.º 0042-7781            |
  | titular | n.º | cupo |    |   | titular | cupo | EA %  |    | Cupo 10.000.000 · 24% EA (r/o)   |
  | saldo | disp [###--] |    |   | DESEMBOLSO INICIAL     |    |==================================|
  | (Al día) (Sobregirado)    |   | monto | fecha | desc   |    | saldo | capital | interés | disp |
  +---------------------------+   | [Crear y desembolsar]  |    | [Consumo|Pago|Liquidar] | movs  |
                                  +------------------------+    +----------------------------------+
```

### D7. Monorepo y Docker

```
leancore-fintech/
  backend/            Dockerfile multi-stage (golang -> distroless/alpine)
  frontend/           Dockerfile multi-stage (node build -> nginx), nginx.conf con proxy /api
  docker-compose.yml  db (postgres:16-alpine, healthcheck, volumen)
                      backend (depends_on db healthy; ejecuta migraciones al arrancar)
                      frontend (nginx :80 -> host :8080; /api -> backend:8081)
  .env.example        credenciales de la BD y puertos
  README.md           cómo levantar y justificación de decisiones (resumen de D1-D4)
```

- El navegador solo habla con nginx (mismo origen), así que no hace falta configurar CORS.
- Las migraciones corren al iniciar el backend con golang-migrate (embebidas con `embed`).
- Sin datos de ejemplo: la base arranca vacía y el primer crédito se crea desde la interfaz. El botón "Cargar ejemplo" del prototipo se elimina sin reemplazo.

### D8. Estrategia de pruebas

- **Dominio (unitarias, table-driven):** tasa diaria, devengo por tramos, HALF_UP en los bordes (,49 / ,50), FIFO, sobregiro, fechas, rechazos, y el escenario completo del prototipo con valores esperados calculados a mano.
- **Aplicación:** casos de uso con un repositorio en memoria (fake del puerto), incluyendo la idempotencia (mismo hash, hash distinto, rechazo que no consume la llave).
- **Integración (PostgreSQL real con testcontainers-go):** concurrencia (N goroutines consumiendo sobre el mismo crédito sin superar el cupo), reintentos concurrentes con la misma llave, rollback ante fallo y mapeo `NUMERIC(38,0) <-> big.Int`.
- **Frontend:** unitarias del parseo y formato de dinero y del ciclo de vida de la llave. Pruebas de componentes para los formularios.

## Risks / Trade-offs

- [El saldo materializado puede divergir de los movimientos por un bug] → saldo y movimientos se escriben en la misma transacción y hay tests que verifican el saldo después de cada operación. La reconciliación automática es una extensión futura documentada.
- [`FOR UPDATE` serializa todo el tráfico de un mismo crédito] → la contención por crédito es naturalmente baja (un titular). Los créditos distintos no se bloquean.
- [La tasa diaria con 10^15 de escala introduce un error de redondeo de la constante] → el error relativo es menor a 10^-12 sobre la tasa diaria, despreciable frente a la liquidación al centavo, y queda congelado, así que el cálculo es reproducible.
- [Descartar la fracción al liquidar pierde hasta 0,5 centavos por liquidación] → es la decisión explícita D3. Se documenta en el README como parte de la justificación del redondeo.
- [El interés simple sobre una EA cobra un poco menos que la EA] → es intencional y conservador (D3).
- [Las liquidaciones frecuentes (cada pago) multiplican los eventos de redondeo] → el efecto está acotado a ±0,5 centavos por evento.
- [Montos grandes en JavaScript] → el límite de 2^53 centavos está muy por encima de cualquier cupo realista. El backend valida rangos en la entrada.

## Migration Plan

Es un proyecto nuevo, así que no hay datos que migrar. El despliegue es `docker compose up --build`: las migraciones iniciales crean el esquema al arrancar el backend. Para volver atrás basta `docker compose down -v`.
