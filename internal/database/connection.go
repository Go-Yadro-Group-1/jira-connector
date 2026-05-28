package database

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	// Register pgx/v5 driver for golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	// PostgreSQL driver for database/sql.
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

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

	err = database.PingContext(ctx)
	if err != nil {
		database.Close()

		return nil, fmt.Errorf("ping database: %w", err)
	}

	err = runMigrations(dsn)
	if err != nil {
		database.Close()

		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return database, nil
}

func runMigrations(originalDSN string) error {
	migrateDSN := strings.Replace(originalDSN, "postgres://", "pgx5://", 1)

	driver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("create iofs driver: %w", err)
	}

	migrator, err := migrate.NewWithSourceInstance("iofs", driver, migrateDSN)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer migrator.Close()

	err = migrator.Up()
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations: %w", err)
	}

	return nil
}
