package database

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	// PostgreSQL driver for database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed schema.sql
var schemaSQL string

const (
	maxOpenConns    = 25
	maxIdleConns    = 5
	connMaxLifetime = 5 * time.Minute
)

func NewConnection(ctx context.Context, cfg config.DBConfig) (*sql.DB, error) {
	hostPort := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	dsn := fmt.Sprintf(
		"postgres://%s:%s@%s/%s?sslmode=disable",
		cfg.User,
		cfg.Password,
		hostPort,
		cfg.DBName,
	)

	database, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	database.SetMaxOpenConns(maxOpenConns)
	database.SetMaxIdleConns(maxIdleConns)
	database.SetConnMaxLifetime(connMaxLifetime)

	if err := database.PingContext(ctx); err != nil {
		database.Close()

		return nil, fmt.Errorf("ping database: %w", err)
	}

	if err := initSchema(ctx, database); err != nil {
		database.Close()

		return nil, fmt.Errorf("init schema: %w", err)
	}

	return database, nil
}

func initSchema(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, schemaSQL)
	if err != nil {
		return fmt.Errorf("execute schema: %w", err)
	}

	return nil
}
