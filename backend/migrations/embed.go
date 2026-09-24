// Package migrations embebe el esquema SQL para golang-migrate.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
