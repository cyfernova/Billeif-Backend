package app

import (
	"testing"
	"time"

	"invoice-backend/internal/config"
)

type recordingDatabasePool struct {
	maxOpen  int
	maxIdle  int
	idleTime time.Duration
	lifetime time.Duration
}

func (p *recordingDatabasePool) SetMaxOpenConns(value int)              { p.maxOpen = value }
func (p *recordingDatabasePool) SetMaxIdleConns(value int)              { p.maxIdle = value }
func (p *recordingDatabasePool) SetConnMaxIdleTime(value time.Duration) { p.idleTime = value }
func (p *recordingDatabasePool) SetConnMaxLifetime(value time.Duration) { p.lifetime = value }

func TestConfigureDatabasePoolUsesSmallAPIPool(t *testing.T) {
	pool := &recordingDatabasePool{}

	configureDatabasePool(pool, config.ProfileHTTP)

	if pool.maxOpen != 2 || pool.maxIdle != 1 ||
		pool.idleTime != 2*time.Minute || pool.lifetime != 10*time.Minute {
		t.Fatalf("API database pool = %#v, want two open and one idle connection with bounded lifetime", pool)
	}
}

func TestConfigureDatabasePoolUsesSingleWorkerConnection(t *testing.T) {
	pool := &recordingDatabasePool{}

	configureDatabasePool(pool, config.ProfileInvoice)

	if pool.maxOpen != 1 || pool.maxIdle != 0 ||
		pool.idleTime != 2*time.Minute || pool.lifetime != 10*time.Minute {
		t.Fatalf("worker database pool = %#v, want one open and no idle connections with bounded lifetime", pool)
	}
}
