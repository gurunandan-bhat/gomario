package model

import (
	"context"
	"gomario/lib/config"
	"log"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
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

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	dbHandle, err := sqlx.ConnectContext(ctx, "mysql", dbCfg.FormatDSN())
	if err != nil {
		return nil, err
	}

	if err := dbHandle.Ping(); err != nil {
		return nil, err
	}

	dbHandle.SetMaxOpenConns(maxOpenConns)
	dbHandle.SetMaxIdleConns(maxIdleConns)
	dbHandle.SetConnMaxIdleTime(maxIdleTime)
	dbHandle.SetConnMaxLifetime(maxLifetime)

	return &Model{
		DbHandle: dbHandle,
	}, nil
}
