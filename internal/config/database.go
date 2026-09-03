package config

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Database wraps pgxpool so legacy callers retain a hard operation deadline
// without leaving one timer alive for DatabaseOperationTimeout after every
// successful query.
type Database struct {
	*pgxpool.Pool
}

func NewDatabase(pool *pgxpool.Pool) *Database {
	if pool == nil {
		return nil
	}
	return &Database{Pool: pool}
}

func databaseOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	return context.WithTimeout(parent, DatabaseOperationTimeout)
}

func (db *Database) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	bounded, cancel := databaseOperationContext(ctx)
	defer cancel()
	return db.Pool.Exec(bounded, sql, arguments...)
}

func (db *Database) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	bounded, cancel := databaseOperationContext(ctx)
	rows, err := db.Pool.Query(bounded, sql, args...)
	if err != nil {
		cancel()
		return nil, err
	}
	return &cancelRows{Rows: rows, cancel: cancel}, nil
}

func (db *Database) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	bounded, cancel := databaseOperationContext(ctx)
	return &cancelRow{Row: db.Pool.QueryRow(bounded, sql, args...), cancel: cancel}
}

func (db *Database) Begin(ctx context.Context) (pgx.Tx, error) {
	bounded, cancel := databaseOperationContext(ctx)
	defer cancel()
	tx, err := db.Pool.Begin(bounded)
	if err != nil {
		return nil, err
	}
	return &databaseTx{Tx: tx}, nil
}

func (db *Database) BeginTx(ctx context.Context, options pgx.TxOptions) (pgx.Tx, error) {
	bounded, cancel := databaseOperationContext(ctx)
	defer cancel()
	tx, err := db.Pool.BeginTx(bounded, options)
	if err != nil {
		return nil, err
	}
	return &databaseTx{Tx: tx}, nil
}

func (db *Database) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	bounded, cancel := databaseOperationContext(ctx)
	return &cancelBatchResults{BatchResults: db.Pool.SendBatch(bounded, batch), cancel: cancel}
}

type cancelRows struct {
	pgx.Rows
	cancel context.CancelFunc
	once   sync.Once
}

func (rows *cancelRows) finish() {
	rows.once.Do(rows.cancel)
}

func (rows *cancelRows) Close() {
	rows.Rows.Close()
	rows.finish()
}

func (rows *cancelRows) Next() bool {
	next := rows.Rows.Next()
	if !next {
		rows.finish()
	}
	return next
}

func (rows *cancelRows) Scan(dest ...any) error {
	err := rows.Rows.Scan(dest...)
	if err != nil {
		rows.finish()
	}
	return err
}

type cancelRow struct {
	pgx.Row
	cancel context.CancelFunc
}

func (row *cancelRow) Scan(dest ...any) error {
	defer row.cancel()
	return row.Row.Scan(dest...)
}

type cancelBatchResults struct {
	pgx.BatchResults
	cancel context.CancelFunc
	once   sync.Once
}

func (results *cancelBatchResults) Close() error {
	err := results.BatchResults.Close()
	results.once.Do(results.cancel)
	return err
}

type databaseTx struct {
	pgx.Tx
}

func (tx *databaseTx) Begin(ctx context.Context) (pgx.Tx, error) {
	bounded, cancel := databaseOperationContext(ctx)
	defer cancel()
	nested, err := tx.Tx.Begin(bounded)
	if err != nil {
		return nil, err
	}
	return &databaseTx{Tx: nested}, nil
}

func (tx *databaseTx) Commit(ctx context.Context) error {
	bounded, cancel := databaseOperationContext(ctx)
	defer cancel()
	return tx.Tx.Commit(bounded)
}

func (tx *databaseTx) Rollback(ctx context.Context) error {
	bounded, cancel := databaseOperationContext(ctx)
	defer cancel()
	return tx.Tx.Rollback(bounded)
}

func (tx *databaseTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	bounded, cancel := databaseOperationContext(ctx)
	defer cancel()
	return tx.Tx.CopyFrom(bounded, tableName, columnNames, rowSrc)
}

func (tx *databaseTx) SendBatch(ctx context.Context, batch *pgx.Batch) pgx.BatchResults {
	bounded, cancel := databaseOperationContext(ctx)
	return &cancelBatchResults{BatchResults: tx.Tx.SendBatch(bounded, batch), cancel: cancel}
}

func (tx *databaseTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	bounded, cancel := databaseOperationContext(ctx)
	defer cancel()
	return tx.Tx.Prepare(bounded, name, sql)
}

func (tx *databaseTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	bounded, cancel := databaseOperationContext(ctx)
	defer cancel()
	return tx.Tx.Exec(bounded, sql, arguments...)
}

func (tx *databaseTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	bounded, cancel := databaseOperationContext(ctx)
	rows, err := tx.Tx.Query(bounded, sql, args...)
	if err != nil {
		cancel()
		return nil, err
	}
	return &cancelRows{Rows: rows, cancel: cancel}, nil
}

func (tx *databaseTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	bounded, cancel := databaseOperationContext(ctx)
	return &cancelRow{Row: tx.Tx.QueryRow(bounded, sql, args...), cancel: cancel}
}
