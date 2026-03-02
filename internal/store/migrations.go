package store

import "database/sql"

const migrationsTable = `CREATE TABLE IF NOT EXISTS migrations (
	version    INTEGER PRIMARY KEY,
	applied_at DATETIME DEFAULT (datetime('now'))
);`

var migrations = []struct {
	version int
	sql     string
}{
	{1, `CREATE TABLE IF NOT EXISTS deliveries (
    id TEXT PRIMARY KEY,
    route_name TEXT NOT NULL,
    event_type TEXT NOT NULL,
    source_org TEXT NOT NULL DEFAULT '',
    source_repo TEXT NOT NULL DEFAULT '',
    delivery_id TEXT NOT NULL DEFAULT '',
    payload_hash TEXT NOT NULL DEFAULT '',
    payload BLOB DEFAULT NULL,
    destination_url TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    response_code INTEGER DEFAULT 0,
    response_body TEXT DEFAULT '',
    attempt INTEGER NOT NULL DEFAULT 1,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    next_retry_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_deliveries_status ON deliveries(status);
CREATE INDEX IF NOT EXISTS idx_deliveries_next_retry ON deliveries(status, next_retry_at);
CREATE INDEX IF NOT EXISTS idx_deliveries_route ON deliveries(route_name);
CREATE INDEX IF NOT EXISTS idx_deliveries_event ON deliveries(event_type);`},
}

func runMigrations(db *sql.DB) error {
	if _, err := db.Exec(migrationsTable); err != nil {
		return err
	}

	for _, m := range migrations {
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM migrations WHERE version = ?", m.version).Scan(&count); err != nil {
			return err
		}
		if count > 0 {
			continue
		}
		if _, err := db.Exec(m.sql); err != nil {
			return err
		}
		if _, err := db.Exec("INSERT INTO migrations (version) VALUES (?)", m.version); err != nil {
			return err
		}
	}
	return nil
}
