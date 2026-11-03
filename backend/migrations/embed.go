// Package migrations embarque les migrations goose (SQL pur) dans le binaire.
package migrations

import "embed"

// FS contient les fichiers *.sql, à la racine.
//
//go:embed *.sql
var FS embed.FS
