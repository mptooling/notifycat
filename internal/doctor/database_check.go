package doctor

import (
	"github.com/mptooling/notifycat/internal/store"
)

// CheckDatabase opens dsn, pings the underlying connection, and reports the
// result. It does not run migrations — that is the server's job.
func CheckDatabase(dsn string) Section {
	sec := Section{Name: "database"}
	if dsn == "" {
		sec.Checks = append(sec.Checks, failResult("open", "DATABASE_URL is empty; set it to a SQLite path or file: DSN"))
		return sec
	}
	db, err := store.Open(dsn)
	if err != nil {
		sec.Checks = append(sec.Checks, failResult("open", "cannot open %q: %v; ensure the parent directory exists and is writable", dsn, err))
		return sec
	}
	sqlDB, err := store.SQLDB(db)
	if err != nil {
		sec.Checks = append(sec.Checks, failResult("open", "cannot resolve underlying *sql.DB: %v", err))
		return sec
	}
	defer func() { _ = sqlDB.Close() }()
	if err := sqlDB.Ping(); err != nil {
		sec.Checks = append(sec.Checks, failResult("ping", "ping failed: %v", err))
		return sec
	}
	sec.Checks = append(sec.Checks, okResult("open", dsn))
	return sec
}
