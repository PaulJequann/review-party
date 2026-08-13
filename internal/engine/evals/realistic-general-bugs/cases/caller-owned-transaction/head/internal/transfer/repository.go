package transfer

import (
	"context"
	"database/sql"
	_ "embed"
)

//go:embed transfer.sql
var transferSQL string

type Repository struct {
	database *sql.DB
}

func NewRepository(database *sql.DB) Repository {
	return Repository{database: database}
}

func (repository Repository) Transfer(ctx context.Context, sender, recipient string, amount int64) error {
	transaction, err := repository.database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, transferSQL,
		sql.Named("sender", sender),
		sql.Named("recipient", recipient),
		sql.Named("amount", amount),
	); err != nil {
		return err
	}
	return transaction.Commit()
}
