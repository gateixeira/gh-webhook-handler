package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLite opens the database at dbPath and runs migrations.
func NewSQLite(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if err := runMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Create(d *Delivery) error {
	_, err := s.db.Exec(`INSERT INTO deliveries
		(id, route_name, event_type, source_org, source_repo, delivery_id,
		 payload_hash, payload, destination_url, status, response_code, response_body,
		 attempt, max_attempts, next_retry_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'), datetime('now'))`,
		d.ID, d.RouteName, d.EventType, d.SourceOrg, d.SourceRepo,
		d.DeliveryID, d.PayloadHash, d.Payload, d.DestinationURL, d.Status,
		d.ResponseCode, d.ResponseBody, d.Attempt, d.MaxAttempts,
		nullableTime(d.NextRetryAt))
	return err
}

func (s *SQLiteStore) Update(d *Delivery) error {
	_, err := s.db.Exec(`UPDATE deliveries SET
		route_name = ?, event_type = ?, source_org = ?, source_repo = ?,
		delivery_id = ?, payload_hash = ?, payload = ?, destination_url = ?, status = ?,
		response_code = ?, response_body = ?, attempt = ?, max_attempts = ?,
		next_retry_at = ?, updated_at = datetime('now')
		WHERE id = ?`,
		d.RouteName, d.EventType, d.SourceOrg, d.SourceRepo,
		d.DeliveryID, d.PayloadHash, d.Payload, d.DestinationURL, d.Status,
		d.ResponseCode, d.ResponseBody, d.Attempt, d.MaxAttempts,
		nullableTime(d.NextRetryAt), d.ID)
	return err
}

func (s *SQLiteStore) Get(id string) (*Delivery, error) {
	row := s.db.QueryRow(`SELECT id, route_name, event_type, source_org, source_repo,
		delivery_id, payload_hash, payload, destination_url, status, response_code,
		response_body, attempt, max_attempts, next_retry_at, created_at, updated_at
		FROM deliveries WHERE id = ?`, id)
	return scanDelivery(row)
}

func (s *SQLiteStore) List(filter ListFilter) ([]Delivery, error) {
	query := `SELECT id, route_name, event_type, source_org, source_repo,
		delivery_id, payload_hash, payload, destination_url, status, response_code,
		response_body, attempt, max_attempts, next_retry_at, created_at, updated_at
		FROM deliveries WHERE 1=1`
	var args []any

	if filter.RouteName != "" {
		query += " AND route_name = ?"
		args = append(args, filter.RouteName)
	}
	if filter.EventType != "" {
		query += " AND event_type = ?"
		args = append(args, filter.EventType)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.SourceRepo != "" {
		query += " AND source_repo = ?"
		args = append(args, filter.SourceRepo)
	}

	query += " ORDER BY created_at DESC"

	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query += " LIMIT ?"
	args = append(args, limit)

	if filter.Offset > 0 {
		query += " OFFSET ?"
		args = append(args, filter.Offset)
	}

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []Delivery
	for rows.Next() {
		d, err := scanDeliveryRows(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, *d)
	}
	return deliveries, rows.Err()
}

func (s *SQLiteStore) GetRetryable(now time.Time) ([]Delivery, error) {
	rows, err := s.db.Query(`SELECT id, route_name, event_type, source_org, source_repo,
		delivery_id, payload_hash, payload, destination_url, status, response_code,
		response_body, attempt, max_attempts, next_retry_at, created_at, updated_at
		FROM deliveries
		WHERE status = 'failed' AND next_retry_at <= ? AND attempt < max_attempts
		ORDER BY next_retry_at ASC
		LIMIT 100`, now.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deliveries []Delivery
	for rows.Next() {
		d, err := scanDeliveryRows(rows)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, *d)
	}
	return deliveries, rows.Err()
}

func (s *SQLiteStore) DeleteOlderThan(status string, before time.Time) (int64, error) {
	result, err := s.db.Exec(
		"DELETE FROM deliveries WHERE status = ? AND updated_at < ?",
		status, before.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SQLiteStore) ClearPayloadsOlderThan(status string, before time.Time) (int64, error) {
	result, err := s.db.Exec(
		"UPDATE deliveries SET payload = NULL, updated_at = datetime('now') WHERE status = ? AND payload IS NOT NULL AND updated_at < ?",
		status, before.UTC().Format("2006-01-02 15:04:05"))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanDelivery(row *sql.Row) (*Delivery, error) {
	var d Delivery
	var nextRetry sql.NullString
	var createdAt, updatedAt string

	err := row.Scan(&d.ID, &d.RouteName, &d.EventType, &d.SourceOrg, &d.SourceRepo,
		&d.DeliveryID, &d.PayloadHash, &d.Payload, &d.DestinationURL, &d.Status,
		&d.ResponseCode, &d.ResponseBody, &d.Attempt, &d.MaxAttempts,
		&nextRetry, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	d.CreatedAt = parseTime(createdAt)
	d.UpdatedAt = parseTime(updatedAt)
	if nextRetry.Valid {
		t := parseTime(nextRetry.String)
		if !t.IsZero() {
			d.NextRetryAt = &t
		}
	}
	return &d, nil
}

func scanDeliveryRows(rows *sql.Rows) (*Delivery, error) {
	var d Delivery
	var nextRetry sql.NullString
	var createdAt, updatedAt string

	err := rows.Scan(&d.ID, &d.RouteName, &d.EventType, &d.SourceOrg, &d.SourceRepo,
		&d.DeliveryID, &d.PayloadHash, &d.Payload, &d.DestinationURL, &d.Status,
		&d.ResponseCode, &d.ResponseBody, &d.Attempt, &d.MaxAttempts,
		&nextRetry, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}

	d.CreatedAt = parseTime(createdAt)
	d.UpdatedAt = parseTime(updatedAt)
	if nextRetry.Valid {
		t := parseTime(nextRetry.String)
		if !t.IsZero() {
			d.NextRetryAt = &t
		}
	}
	return &d, nil
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

// parseTime attempts to parse a datetime string in common SQLite formats.
func parseTime(s string) time.Time {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z",
		"2006-01-02T15:04:05",
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
