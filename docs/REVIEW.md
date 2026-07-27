# Code Review — ch-compass

Revisión completa del proyecto (~1700 líneas, 20 archivos Go). `go vet ./...`,
`gofmt -l .` y `go build ./...` no reportan nada. No se encontraron problemas
de concurrencia ni de SQL injection: las goroutines en `analyze.go` no
comparten estado, y todas las queries usan bind params (`{name:Type}`), sin
concatenación de strings con input de usuario.

## Alta prioridad

### 1. Fuga de conexión en fallo de ping — ✅ corregido
[internal/ch/client.go:59-66](internal/ch/client.go#L59-L66)

`clickhouse.Open` crea el pool de conexiones y si el `Ping` posterior falla,
`conn` nunca se cierra antes de retornar el error. Cada intento de conexión
fallido deja el pool/goroutines vivos.

```go
conn, err := clickhouse.Open(chOpts)
if err != nil {
    return nil, err
}

if err := conn.Ping(ctx); err != nil {
    conn.Close() // falta esto
    return nil, err
}
```

### 2. El umbral de "tabla fría" no está atado a `--days` — ✅ corregido
[internal/analyze/cold_tables.go:10](internal/analyze/cold_tables.go#L10)

`coldThreshold` está hardcodeado a 60 días, pero la ventana de accesos que
alimenta `accesses` usa el flag `--days` (default **30**,
[internal/cli/analyze.go:84](internal/cli/analyze.go#L84)). Con el default,
una tabla consultada hace 31-59 días no aparece en `accesses` y sí se marca
como fría una vez que `LastModified` pasa los 60 días → falso positivo
estructural con la configuración por defecto.

Fix sugerido: derivar `coldThreshold` de `days`, o exigir `days >= 60` para
el análisis de tablas frías.

### 3. Detección de uso de vistas solo matchea nombre completo `database.view`
[internal/analyze/query_patterns.go:130-158](internal/analyze/query_patterns.go#L130-L158)

`collectRegularViewAccess` hace substring-match de `database.view` contra el
texto crudo de la query. Si las queries usan `USE database` y referencian la
vista sin calificar (patrón muy común), no matchea, y una vista que sí se usa
termina recomendada para archivar/borrar. No está documentado como
limitación, a diferencia del caveat de `query_views_log`.

### 4. Degradación inconsistente ante tablas de sistema restringidas
[internal/analyze/query_patterns.go](internal/analyze/query_patterns.go)

Solo `collectMaterializedViewActivity` (líneas 196-208) maneja con gracia una
tabla de sistema faltante/restringida vía `ExceptionCode`. `SYSTEM FLUSH
LOGS` (línea 45) y `collectTableAccess`/`collectRegularViewAccess` (que
necesitan `system.query_log`) no tienen el mismo fallback — un usuario sin
privilegio para flush logs aborta todo el `analyze` en vez de degradar como
en la ruta de MVs.

## Media prioridad

### 5. Sin validación de `--days`
[internal/cli/analyze.go:84](internal/cli/analyze.go#L84), usado directo como
`{days:UInt32}` en varias queries. Un valor 0 o negativo falla de forma poco
clara dentro del driver/ClickHouse en vez de un error de CLI claro.

### 6. `-v/--verbose` es un no-op
Declarado y bindeado en [internal/cli/root.go:13,24](internal/cli/root.go)
pero nunca leído en el resto del código (confirmado por grep). O se conecta
o se elimina.

### 7. `os.Exit` llamado dentro de `RunE` — ✅ corregido
[internal/cli/analyze.go:70-71](internal/cli/analyze.go#L70-L71). Evita el
manejo de errores propio de cobra y deja un `return nil` inalcanzable justo
después. Preferible retornar un error/código desde `RunE` y salir en
`main.go`.

### 8. Huecos de cobertura de tests
Sin tests en `internal/analyze/analyze.go`, `databases.go`,
`mutation_stats.go`, `table_stats.go`, los helpers puros de
`query_patterns.go` (`mergeAccess`, `shortName`), `internal/report/json.go`,
y todo `internal/cli` (p.ej. `splitTrimmed`, las validaciones de
mutua-exclusión en `runAnalyze`), a pesar de contener lógica pura barata de
testear.

## Nitpicks

- **9.** `--password` ([internal/cli/analyze.go:81](internal/cli/analyze.go#L81))
  solo texto plano, sin fallback por variable de entorno — expuesto en
  historial de shell / `ps`.
- **10.** `defer client.Close()`
  ([internal/cli/analyze.go:129](internal/cli/analyze.go#L129)) descarta su
  error silenciosamente.
- **11.** `mergeAccess`
  ([internal/analyze/query_patterns.go:239-265](internal/analyze/query_patterns.go#L239-L265))
  retorna en orden de iteración de map — no determinístico, inofensivo hoy
  pero frágil si se renderiza directamente en algún momento.
