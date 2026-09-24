# credit-ledger-ui Specification
## Purpose

Define la interfaz web para gestionar créditos: listado, apertura y libro mayor. Incluye cómo el cliente maneja el dinero sin punto flotante y cómo genera las llaves de idempotencia.

## Requirements

### Requirement: Listado de créditos
La interfaz SHALL mostrar como pantalla inicial el listado de créditos con titular, número de cuenta, cupo, saldo pendiente, disponible, una barra de utilización y el estado. El estado `SOBREGIRADO` MUST resaltarse visualmente como alerta. La interfaz SHALL ofrecer la acción "Nuevo crédito", y seleccionar un crédito SHALL abrir su libro mayor.

#### Scenario: Navegar a un crédito
- **WHEN** el usuario selecciona un crédito del listado
- **THEN** se abre el libro mayor de ese crédito

#### Scenario: Listado vacío
- **WHEN** no existen créditos
- **THEN** se muestra un mensaje que invita a crear el primer crédito

### Requirement: Pantalla de nuevo crédito
La interfaz SHALL permitir abrir un crédito con titular, cupo aprobado, tasa EA (%), y monto, fecha y descripción del desembolso inicial. La interfaz SHALL indicar que la tasa y el cupo no podrán cambiarse después, y SHALL ofrecer un atajo para desembolsar el cupo completo. Los errores de validación MUST mostrarse junto al formulario sin perder los datos ingresados. Al crear el crédito, la interfaz SHALL navegar a su libro mayor.

#### Scenario: Desembolsar cupo completo
- **WHEN** el usuario ingresa un cupo de $10.000.000 y usa "Desembolsar cupo completo"
- **THEN** el monto del desembolso se completa con $10.000.000

#### Scenario: Error de validación
- **WHEN** el backend rechaza la apertura porque el desembolso supera el cupo
- **THEN** se muestra el mensaje de error y el formulario conserva los valores ingresados

### Requirement: Libro mayor del crédito
La interfaz SHALL mostrar para un crédito: el titular, el número de cuenta, el cupo y la tasa EA en modo solo lectura; el saldo pendiente destacado y el saldo proyectado con el interés devengado a hoy; el capital; el interés por pagar y el interés devengado sin liquidar; el disponible con su barra de utilización; y el historial de movimientos con fecha, tipo, detalle, cargo, abono, saldo y totales. Para cada pago, el detalle MUST mostrar la porción aplicada a interés y a capital, y el reparto de capital por uso. La interfaz MUST NOT ofrecer acciones para editar el cupo o la tasa, ni para deshacer movimientos.

#### Scenario: Detalle de un pago
- **WHEN** el historial contiene un pago que aplicó $643.800 a interés y $8.356.200 a capital repartido entre los usos #2 y #3
- **THEN** la fila del pago muestra "Interés $643.800 · Capital $8.356.200" y el reparto por uso

#### Scenario: Crédito sobregirado
- **WHEN** el crédito está sobregirado
- **THEN** la barra de utilización y el estado se muestran como alerta

### Requirement: Formularios de operación
El libro mayor SHALL ofrecer tres operaciones: Consumo, Pago y Liquidar interés. El formulario de pago SHALL mostrar, para la fecha elegida, el interés que se liquidará automáticamente y el saldo total a esa fecha, y SHALL ofrecer un atajo "Pagar saldo total". El formulario de liquidación SHALL mostrar la vista previa (base de capital, días desde la última liquidación y monto) antes de confirmar. Tras cada operación exitosa, el saldo y el historial MUST actualizarse.

#### Scenario: Vista previa del pago
- **WHEN** el usuario elige la fecha 2026-09-10 en el formulario de pago
- **THEN** se muestran el interés a liquidar y el saldo total a esa fecha, obtenidos del backend

#### Scenario: Pagar saldo total
- **WHEN** el usuario usa "Pagar saldo total"
- **THEN** el monto se completa con el saldo total a la fecha elegida

### Requirement: Dinero sin punto flotante en el cliente
La interfaz MUST convertir los montos ingresados a centavos enteros procesando el texto, sin aritmética de punto flotante, y MUST formatear para mostrar a partir de los centavos enteros recibidos. Los montos SHALL mostrarse en COP con separador de miles y dos decimales. La tasa ingresada en porcentaje MUST convertirse a puntos básicos de la misma forma.

#### Scenario: Conversión de un monto ingresado
- **WHEN** el usuario ingresa "1.500.000,50"
- **THEN** la interfaz envía 150000050 centavos

#### Scenario: Conversión de la tasa
- **WHEN** el usuario ingresa una tasa de "24,5"
- **THEN** la interfaz envía 2450 bps

### Requirement: Llave de idempotencia por intención
La interfaz MUST generar la llave de idempotencia (UUID) al preparar cada formulario de operación, no al hacer clic en enviar. La interfaz MUST reutilizar la misma llave en todos los envíos de ese formulario hasta recibir una respuesta exitosa, y solo entonces generar una nueva. Mientras un envío esté en curso, el botón de envío MUST estar deshabilitado.

#### Scenario: Doble clic
- **WHEN** el usuario hace doble clic en "Registrar pago"
- **THEN** a lo sumo se envía una solicitud, y cualquier envío adicional usa la misma llave

#### Scenario: Reintento tras error de red
- **WHEN** un envío falla por error de red y el usuario vuelve a enviar
- **THEN** el reenvío usa la misma llave

#### Scenario: Nueva operación tras éxito
- **WHEN** un pago se registra con éxito y el usuario registra otro pago
- **THEN** el segundo pago usa una llave nueva
