package model

import (
	"context"
	"gomario/lib/config"
	"log"
	"time"

	"github.com/XSAM/otelsql"
	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

type Model struct {
	DbHandle *sqlx.DB
}

const (
	defaultTimeout = 3 * time.Second
	maxIdleTime    = 1 * time.Hour
	maxOpenConns   = 25
	maxIdleConns   = 25
	maxLifetime    = 2 * time.Hour
)

func NewModel(cfg *config.Config) (*Model, error) {

	dbCfg := mysql.NewConfig()

	dbCfg.User = cfg.Db.User
	dbCfg.Passwd = cfg.Db.Password
	dbCfg.Net = cfg.Db.Net
	dbCfg.Addr = cfg.Db.Address
	dbCfg.DBName = cfg.Db.DbName
	dbCfg.ParseTime = cfg.Db.ParseTime

	tz, err := time.LoadLocation(cfg.Db.Location)
	if err != nil {
		log.Fatalf("Error fetching local timezone: %s", err)
	}
	dbCfg.Loc = tz
	dbCfg.AllowNativePasswords = cfg.Db.AllowNativePasswords

	// Open the database through the otelsql wrapper so every query produces
	// an OpenTelemetry child span automatically.
	sqlDB, err := otelsql.Open("mysql", dbCfg.FormatDSN(),
		otelsql.WithAttributes(semconv.DBSystemMySQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{Ping: true}),
	)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, err
	}

	dbHandle := sqlx.NewDb(sqlDB, "mysql")

	dbHandle.SetMaxOpenConns(maxOpenConns)
	dbHandle.SetMaxIdleConns(maxIdleConns)
	dbHandle.SetConnMaxIdleTime(maxIdleTime)
	dbHandle.SetConnMaxLifetime(maxLifetime)

	return &Model{
		DbHandle: dbHandle,
	}, nil
}
