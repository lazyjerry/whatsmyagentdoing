package main

// Two sources (Antigravity, OpenCode) keep their state in SQLite databases.
// Go's standard library has no SQLite driver and the binary stays
// dependency-free, so rows are read through the `sqlite3` command line tool
// (bundled with macOS, a package install elsewhere). No sqlite3 → those
// sources are off; nothing else is affected.

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"time"
)

var (
	sqliteBinary = "sqlite3" // overridable in tests
	errNoSqlite  = errors.New("sqlite3 not found")
)

// sqliteJSON runs one query with `sqlite3 -json` and decodes the rows into
// out (a pointer to a slice of structs tagged with the column names). The
// database is opened `mode=ro` (not `immutable`) so rows an active run has
// written to the WAL are seen.
func sqliteJSON(db, query string, out any) error {
	bin, err := exec.LookPath(sqliteBinary)
	if err != nil {
		return errNoSqlite
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, bin, "-json", "file:"+db+"?mode=ro", query).Output()
	if err != nil {
		return err
	}
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return nil // sqlite3 prints nothing at all for zero rows
	}
	return json.Unmarshal(raw, out)
}

// sqliteWrite runs a script on a database it may create and change (our own
// office.db, todo.go). The script goes in on stdin as one transaction; when
// out is set, the rows of its last SELECT are decoded into it. Values are
// spliced in with sqlQuote: the CLI has no bound parameters.
func sqliteWrite(db, script string, out any) error {
	bin, err := exec.LookPath(sqliteBinary)
	if err != nil {
		return errNoSqlite
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-bail", "-json", db)
	cmd.Stdin = strings.NewReader(".timeout 3000\nBEGIN;\n" + script + "\nCOMMIT;\n")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return errors.New(msg)
		}
		return err
	}
	raw = []byte(strings.TrimSpace(string(raw)))
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// sqlQuote is a SQL string literal: single quotes doubled, NUL dropped
// (SQLite has no backslash escapes, so nothing else can break out).
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, "\x00", ""), "'", "''") + "'"
}
