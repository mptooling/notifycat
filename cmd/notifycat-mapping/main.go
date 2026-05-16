// Command notifycat-mapping manages repository → Slack-channel mappings.
// All logic lives in internal/mappingcli; this binary is just the entrypoint.
package main

import (
	"fmt"
	"os"

	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/config"
	"github.com/mptooling/notifycat/internal/mappingcli"
	"github.com/mptooling/notifycat/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-mapping:", err)
		os.Exit(1)
	}
	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "notifycat-mapping:", err)
		os.Exit(1)
	}
	defer closeDB(db)

	os.Exit(mappingcli.Run(os.Args[1:], db, os.Stdout, os.Stderr, cfg))
}

func closeDB(db *gorm.DB) {
	if sqlDB, err := store.SQLDB(db); err == nil {
		_ = sqlDB.Close()
	}
}
