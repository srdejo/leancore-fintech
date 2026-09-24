# Tasks

## 1. Estructura del monorepo

- [x] 1.1 Crear `backend/` con `go.mod` y el esqueleto `cmd/api`, `internal/domain`, `internal/application/{ports,usecases}`, `internal/adapters/{in/http,out/postgres,out/clock}`, `migrations/`; verificar que `go build ./...` compila
- [x] 1.2 Crear `frontend/` con Angular (standalone, signals) y la carpeta `src/app/credits/{domain,application,infrastructure,ui}`; verificar que `ng build` compila
- [x] 1.3 Agregar las dependencias del backend (`pgx/v5`, `golang-migrate`, `google/uuid`, `testcontainers-go`) y verificar que `go mod tidy` y `go build ./...` terminan sin errores

## 2. Dominio: dinero, tasa e interés

- [x] 2.1 Implementar `Money` (int64 centavos) con suma y resta con detección de overflow y comparación; verificar con tests unitarios de bordes (overflow, cero, negativos)
- [x] 2.2 Implementar la derivación `rate_ea_bps -> daily_rate_e15` con `big.Float` de 256 bits y redondeo HALF_UP; verificar con tests que 2400 bps da el valor esperado calculado de forma independiente y que 0 bps da 0
- [x] 2.3 Implementar el devengo por tramos en `big.Int` (`capital × daily_rate_e15 × días`) y la liquidación HALF_UP con reinicio del acumulado; verificar con los escenarios de `specs/interest-accrual` (tramos, interés por pagar sin interés, ,23 y ,50 centavos, sin arrastre de residuo)

## 3. Dominio: agregado del crédito

- [x] 3.1 Implementar la apertura con desembolso inicial (validaciones, cupo, tasa congelada); verificar con los escenarios de apertura de `specs/credit-lines`
- [x] 3.2 Implementar la regla de fecha no anterior al último movimiento y el cálculo de disponible y estado (`AL_DIA`, `SOBREGIRADO`, `SIN_SALDO`); verificar con tests de fecha anterior, misma fecha y estados
- [x] 3.3 Implementar el consumo (monto > 0, <= disponible, devengo del tramo antes de cambiar el capital); verificar con los escenarios de consumo y de consumo en sobregiro de `specs/credit-ledger-movements`
- [x] 3.4 Implementar la liquidación manual y la vista previa sin efectos (incluido el rechazo cuando el monto es 0); verificar con los escenarios de liquidación, incluido el sobregiro por interés
- [x] 3.5 Implementar el pago: liquidación automática, validación contra el saldo después de liquidar, interés primero y reparto FIFO por uso; verificar con los escenarios de pago y FIFO de `specs/credit-ledger-movements`, incluido que un rechazo no genere el `INTERES` automático
- [x] 3.6 Agregar un test del escenario completo del prototipo (desembolso, consumos, liquidaciones y pagos entre 2026-06-01 y 2026-09-01) con valores esperados calculados a mano, verificando saldo, capital, interés y saldo corrido en cada paso

## 4. Aplicación: puertos y casos de uso

- [x] 4.1 Definir los puertos de entrada (OpenCredit, RegisterPurchase, RegisterPayment, LiquidateInterest, GetCredit, ListCredits, ListEntries, PreviewAt) y de salida (`CreditLineRepository.WithLock`, `Clock`, `IDGenerator`); verificar que `internal/domain` no importa paquetes de infraestructura (`go list -deps`)
- [x] 4.2 Implementar los casos de uso con el flujo bloqueo -> idempotencia -> dominio -> persistir, usando el hash canónico del request; verificar con un repositorio fake en memoria los escenarios de idempotencia de `specs/transaction-safety` (mismo contenido, contenido distinto, un rechazo no consume la llave, llave faltante)
- [x] 4.3 Implementar las consultas (estado con proyección a hoy mediante `Clock`, listado, historial con asignaciones, vista previa); verificar con tests que usan un reloj fijo que la proyección no escribe movimientos

## 5. Persistencia PostgreSQL

- [x] 5.1 Escribir las migraciones de `credit_lines`, `entries` y `payment_allocations` con sus restricciones (`amount > 0`, `UNIQUE(credit_line_id, seq)`, `UNIQUE(credit_line_id, idempotency_key)`, `creation_idempotency_key UNIQUE`, `NUMERIC(38,0)`); verificar que las migraciones suben y bajan limpias contra un Postgres de testcontainers
- [x] 5.2 Implementar el adaptador del repositorio con `pgx` (`WithLock` con `SELECT ... FOR UPDATE` dentro de la transacción, mapeo `NUMERIC <-> big.Int`, carga de usos con capital pendiente); verificar con tests de integración de ida y vuelta de datos y de rollback ante error
- [x] 5.3 Tests de integración de concurrencia: N goroutines consumiendo sobre el mismo crédito sin superar el cupo, pagos concurrentes sin superar el saldo y reintentos concurrentes con la misma llave que dejan un solo movimiento; verificar que pasan de forma repetida (`-count=20`)

## 6. API HTTP

- [x] 6.1 Implementar los handlers `net/http` para las rutas de `design.md` (D5), con decodificación `UseNumber()`, rechazo de montos no enteros, header `Idempotency-Key` obligatorio en POST y mapeo de errores de dominio a 400/404/422 con `{code, message, details}`; verificar con tests `httptest` por endpoint, incluido el reintento idempotente que devuelve el mismo cuerpo
- [x] 6.2 Componer la aplicación en `cmd/api/main.go` (config por variables de entorno, pool, migraciones embebidas al arrancar, reloj real); verificar que el binario arranca contra Postgres y responde `GET /api/credits` con `[]`

## 7. Frontend: dominio e infraestructura

- [x] 7.1 Implementar el parseo de texto a centavos (`"1.500.000,50" -> 150000050`), de tasa en % a bps (`"24,5" -> 2450`) y el formato COP desde centavos, sin `parseFloat` ni aritmética flotante; verificar con tests unitarios de casos válidos, inválidos y bordes
- [x] 7.2 Implementar el adaptador HTTP `CreditsApi` para todos los endpoints, con el `Idempotency-Key` pasado explícitamente; verificar con tests de `HttpTestingController` que se envían los headers y los cuerpos esperados
- [x] 7.3 Implementar el estado de formulario con la llave de idempotencia (se crea al preparar, se conserva ante error de red o 4xx, se renueva solo tras 2xx) y el bloqueo de envíos en vuelo; verificar con tests unitarios de los escenarios de `specs/credit-ledger-ui` (doble clic, reintento, nueva operación)

## 8. Frontend: pantallas

- [x] 8.1 Aplicar el sistema visual (tokens de color, tipografías Instrument Serif/Sans y JetBrains Mono, contenedor y bordes) como estilos globales; verificar visualmente contra el prototipo "Ledger Crédito"
- [x] 8.2 Implementar la pantalla de listado (tabla, barra de utilización, estados con alerta de sobregiro, estado vacío, "Nuevo crédito"); verificar con tests de componente y navegación al libro mayor
- [x] 8.3 Implementar la pantalla de nuevo crédito (condiciones, aviso de condiciones fijas, desembolso con "Desembolsar cupo completo", errores sin perder datos); verificar con tests de componente de los escenarios de `specs/credit-ledger-ui`
- [x] 8.4 Implementar el libro mayor (encabezado de solo lectura, tarjetas de saldo con proyección, tabs Consumo/Pago/Liquidar interés con vistas previas del backend, "Pagar saldo total", historial con detalle interés/capital y FIFO, y totales); verificar con tests de componente y con la actualización tras cada operación

## 9. Docker y documentación

- [x] 9.1 Escribir los Dockerfile multi-stage del backend y del frontend (nginx con proxy `/api` hacia el backend) y el `docker-compose.yml` con Postgres (healthcheck y volumen), junto con `.env.example`; verificar que `docker compose up --build` levanta todo y que la app responde en `http://localhost:8080`
- [x] 9.2 Escribir el `README.md` con cómo levantar y probar el proyecto, y la justificación de las decisiones (modelo de datos, concurrencia, dinero y redondeo, idempotencia) y las extensiones futuras; verificar que los comandos documentados funcionan tal como están escritos

## 10. Verificación integral

- [x] 10.1 Recorrer de punta a punta con `docker compose up`: crear un crédito, hacer consumos hasta rechazar por cupo, liquidar interés, pagar parcial y total, y reintentar un pago con la misma llave (con curl); verificar que los saldos, el historial y el reparto FIFO coinciden con los specs
