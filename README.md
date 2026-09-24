# leancore-fintech · Ledger de crédito

Libro contable simple para una **cuenta de crédito de cupo rotativo**. Permite abrir un crédito con su desembolso inicial, registrar consumos, pagos parciales o totales y liquidaciones de interés, y consultar en todo momento el saldo pendiente y el cupo disponible.

- **Backend:** Go 1.27, arquitectura hexagonal (puertos y adaptadores), `net/http`, PostgreSQL con `pgx`.
- **Frontend:** Angular 22 (standalone, signals, zoneless), servido por nginx.
- **Todo en un comando:** `docker compose up`.

## Cómo levantarlo

Requisito: Docker con Compose.

```bash
docker compose up --build
```

- Aplicación: <http://localhost:8080>
- API (mismo origen, a través de nginx): <http://localhost:8080/api/health>

La base arranca vacía y el backend aplica las migraciones al iniciar. Para cambiar credenciales, puerto o zona horaria, copia `.env.example` a `.env`. Para borrar los datos: `docker compose down -v`.

## Cómo probarlo

```bash
# Backend: unitarias, casos de uso, HTTP e integración contra PostgreSQL real
# (testcontainers; requiere Docker). Con -short se omiten las de integración.
cd backend && go test ./...

# Frontend: unitarias y de componentes (Vitest)
cd frontend && npm ci && npx ng test --watch=false
```

### Recorrido con curl

Los montos van en **centavos** y los comandos exigen `Idempotency-Key` (un UUID). `uuidgen` viene en Linux y macOS; en Windows puedes usar `powershell -c "[guid]::NewGuid()"`.

```bash
API=http://localhost:8080/api
ID=$(curl -s -X POST $API/credits -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"holder":"Maria Ruiz","limitCents":1000000000,"rateEaBps":2400,
       "disbursement":{"amountCents":500000000,"date":"2026-06-01","description":""}}' \
  | sed -E 's/.*"id":"([^"]+)","accountNumber".*/\1/')

curl -s -X POST $API/credits/$ID/purchases -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $(uuidgen)" -d '{"amountCents":80000000,"date":"2026-06-15","description":"Compra"}'

curl -s "$API/credits/$ID/preview?date=2026-07-01"         # interés a liquidar, sin efectos

KEY=$(uuidgen)                                             # repetir con la misma llave devuelve lo mismo
curl -s -X POST $API/credits/$ID/payments -H 'Content-Type: application/json' \
  -H "Idempotency-Key: $KEY" -d '{"amountCents":120000000,"date":"2026-07-01"}'

curl -s $API/credits/$ID           # saldo, disponible, estado e interés devengado a hoy
curl -s $API/credits/$ID/entries   # historial (con el reparto FIFO de cada pago)
```

### API

| Método | Ruta | Uso |
|---|---|---|
| `POST` | `/api/credits` | Abrir un crédito con su desembolso inicial |
| `GET` | `/api/credits` | Listado |
| `GET` | `/api/credits/{id}` | Estado y proyección del interés a hoy |
| `GET` | `/api/credits/{id}/entries` | Historial y totales |
| `GET` | `/api/credits/{id}/preview?date=AAAA-MM-DD` | Vista previa de la liquidación |
| `POST` | `/api/credits/{id}/purchases` | Consumo |
| `POST` | `/api/credits/{id}/payments` | Pago |
| `POST` | `/api/credits/{id}/interest-liquidations` | Liquidar interés a una fecha |

Los errores se devuelven como `{"code","message","details"}`: `400` si el formato es inválido (monto no entero, falta la llave o la fecha es inválida), `404` si el crédito no existe y `422` si se incumple una regla de negocio o se reutiliza una llave con otros datos.

## Reglas del dominio

- **Cupo rotativo.** El desembolso inicial es obligatorio, único y no puede superar el cupo. El cupo y la tasa quedan fijos al abrir el crédito.
- `saldo pendiente = capital + interés por pagar`; `disponible = max(0, cupo − saldo)`.
- **Consumo:** el monto debe ser mayor que 0 y no puede superar el disponible.
- **Pago:** primero se liquida el interés devengado a la fecha del pago. El pago cubre el interés y el resto va a capital en orden **FIFO**, del uso más antiguo al más nuevo. Un pago mayor al saldo se rechaza: no existe saldo a favor.
- **Sobregiro:** el interés sí puede llevar el saldo por encima del cupo. En ese caso el crédito queda `SOBREGIRADO` y se bloquean nuevos consumos.
- Los movimientos son **append-only**: nunca se editan ni se borran, y ninguno puede tener una fecha anterior al último movimiento.

## Decisiones y su justificación

### 1. Modelo de datos: ledger simplificado con saldo materializado (no partida doble)

Los movimientos (`DESEMBOLSO`, `CONSUMO`, `INTERES`, `PAGO`) se guardan append-only en `entries`. El saldo (capital, interés por pagar y devengo pendiente) se **materializa** en `credit_lines` y se actualiza en la misma transacción que cada movimiento. Cada pago registra en `payment_allocations` cuánto abonó al capital de cada uso.

**Por qué no partida doble:** hay una sola cuenta y una sola contraparte, y la pregunta del negocio es "cuánto debe el cliente". Las cuentas adicionales (caja, ingresos por intereses, por pagar a comercios) no tendrían ningún consumidor en el sistema. Como cada tipo de movimiento corresponde a un asiento fijo (por ejemplo, un consumo es débito a cartera y crédito a comercios), migrar después es mecánico. La interfaz ya muestra la lectura Cargo/Abono.

**Por qué materializar el saldo:** así se valida el disponible sin recorrer todo el historial en cada operación, y el bloqueo de concurrencia tiene una fila concreta sobre la cual actuar.

### 2. Concurrencia: bloqueo pesimista sobre la fila del saldo

Cada comando se ejecuta en una transacción que primero hace `SELECT … FROM credit_lines WHERE id = $1 FOR UPDATE`. Así se serializan las operaciones de un mismo crédito, mientras que los créditos distintos no se bloquean entre sí. Hay una prueba con 10 consumos simultáneos que excederían el cupo: siempre entra exactamente uno. Se corre 20 veces seguidas.

Alternativas descartadas:
- **Versionamiento optimista:** es redundante, porque nadie escribe el saldo sin el bloqueo. Además exige reintentos y devuelve 409 en operaciones normales.
- **Mutex en memoria:** no funciona con más de una instancia.
- **`SERIALIZABLE`:** también requiere reintentos.

### 3. Dinero: enteros en todas partes y redondeo HALF_UP

| Concepto | Representación |
|---|---|
| Montos y saldos | `int64` en **centavos** de COP (`BIGINT`, enteros en JSON y en Angular) |
| Tasa pactada | Efectiva anual en puntos básicos (`2450` = 24,5% EA) |
| Tasa diaria | `round_half_up(((1+EA)^(1/365) − 1) × 10^15)`, calculada **una sola vez** al abrir el crédito (`big.Float` de 256 bits) y guardada |
| Devengo | `Σ capital × tasa_diaria × días` en unidades enteras (`big.Int` / `NUMERIC(38,0)`), sin redondeos intermedios |

- **Ningún monto pasa por punto flotante.** La API rechaza montos no enteros, y el frontend convierte el texto ingresado a centavos procesando la cadena, nunca con `parseFloat`. La única operación no entera del sistema es la raíz de la tasa: se aplica a una constante, no a dinero, y el resultado queda congelado.
- **Redondeo:** al liquidar, el devengo se lleva a centavos con **HALF_UP**, y la fracción restante se **descarta** en lugar de arrastrarse al periodo siguiente. HALF_UP es la convención comercial más común y fácil de explicar a un cliente. Solo favorece al comercio en el caso exacto de 0,5 centavos. Descartar la fracción deja cada liquidación cerrada y autocontenida, con un error máximo de ±0,5 centavos por liquidación.
- **Interés simple sobre una EA:** como el interés se calcula solo sobre capital y no se capitaliza, lo cobrado en un año queda igual o ligeramente por debajo de la EA pactada. Es una desviación conservadora y a favor del cliente.

### 4. Idempotencia: una llave por intención

- Todo `POST` exige el header `Idempotency-Key` (UUID). El backend la busca **dentro de la misma transacción del bloqueo**:
  - misma llave y mismo contenido (hash SHA-256 del comando): devuelve el resultado original (`200`, mismo cuerpo);
  - misma llave con otro contenido: `422 IDEMPOTENCY_KEY_REUSED`;
  - un rechazo de negocio hace rollback, así que **no consume** la llave.
- `UNIQUE(credit_line_id, idempotency_key)` en la base de datos es la red de seguridad. Dos reintentos concurrentes con la misma llave dejan un solo movimiento, y hay prueba de ello.
- En el frontend, la llave se genera **al preparar el formulario**, no al hacer clic. Se reutiliza en cada reenvío hasta que llega una respuesta exitosa, y solo entonces se renueva. Mientras hay un envío en curso, el botón se deshabilita. De esta forma, un doble clic o un reintento tras un error de red nunca duplican un pago.

## Arquitectura

```
backend/
  cmd/api/                  composición: config, migraciones, adaptadores
  internal/domain/          reglas puras (sin HTTP, SQL ni reloj)
  internal/application/     puertos (in/out) y casos de uso: bloqueo → idempotencia → dominio → persistir
  internal/adapters/in/http       API REST
  internal/adapters/out/postgres  repositorio (FOR UPDATE, NUMERIC ↔ big.Int)
  internal/adapters/out/memory    repositorio en memoria (tests)
  internal/adapters/out/{clock,ids}
  migrations/               esquema SQL embebido
frontend/src/app/credits/
  domain/                   modelos, dinero en centavos, puerto del repositorio
  application/              ciclo de vida de la llave de idempotencia
  infrastructure/           adaptador HTTP
  ui/                       listado, nuevo crédito, libro mayor
docker-compose.yml          db (postgres) + backend + frontend (nginx con proxy /api)
```

## Extensiones futuras (fuera de alcance)

- **Facturación:** fecha de corte, extracto congelado, pago mínimo, ventana de pago y aplicación del excedente.
- **Reconciliación de saldos:** replay de los movimientos contra el saldo materializado, endpoint de verificación y cierre diario.
- **Partida doble** completa.
- **Tasa de usura** y topes regulatorios.
- **Mora** y cargos por pago tardío.
- **Tasas o cuotas por uso.**
- **Múltiples monedas.**
