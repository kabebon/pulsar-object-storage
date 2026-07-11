// Package migrations embeds the SQL migration files so the binary is
// self-contained: no need to ship the migrations/ directory alongside it.
// Use SourceDir() to obtain an fs.FS for golang-migrate's iofs source driver.
package migrations

import "embed"

//go:embed *.sql
var sql embed.FS

// SQL returns the embedded *.sql migration files.
func SQL() embed.FS { return sql }
