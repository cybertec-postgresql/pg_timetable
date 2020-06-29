package migrator

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

const defaultTableName = "migrations"

// Migrator is the migrator implementation
type Migrator struct {
	TableName  string
	migrations []interface{}
	onNotice   func(string)
}

// Option sets options such migrations or table name.
type Option func(*Migrator)

// TableName creates an option to allow overriding the default table name
func TableName(tableName string) Option {
	return func(m *Migrator) {
		m.TableName = tableName
	}
}

// SetNotice overrides the default standard output function
func SetNotice(noticeFunc func(string)) Option {
	return func(m *Migrator) {
		m.onNotice = noticeFunc
	}
}

// Migrations creates an option with provided migrations
func Migrations(migrations ...interface{}) Option {
	return func(m *Migrator) {
		m.migrations = migrations
	}
}

// New creates a new migrator instance
func New(opts ...Option) (*Migrator, error) {
	m := &Migrator{
		TableName: defaultTableName,
		onNotice: func(msg string) {
			fmt.Println(msg)
		},
	}
	for _, opt := range opts {
		opt(m)
	}

	if len(m.migrations) == 0 {
		return nil, errors.New("Migrations must be provided")
	}

	for _, m := range m.migrations {
		switch m.(type) {
		case *Migration:
		case *MigrationNoTx:
		default:
			return nil, errors.New("Invalid migration type")
		}
	}

	return m, nil
}

// Migrate applies all available migrations
func (m *Migrator) Migrate(ctx context.Context, db *sql.DB) error {
	// create migrations table if doesn't exist
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id INT8 NOT NULL,
			version TEXT	 NOT NULL,
			PRIMARY KEY (id)
		);
	`, m.TableName))
	if err != nil {
		return err
	}

	pm, count, err := m.Pending(ctx, db)
	if err != nil {
		return err
	}

	// plan migrations
	for idx, migration := range pm {
		insertVersion := fmt.Sprintf("INSERT INTO %s (id, version) VALUES (%d, '%s')", m.TableName, idx+count, migration.(fmt.Stringer).String())
		switch mm := migration.(type) {
		case *Migration:
			if err := migrate(ctx, db, insertVersion, mm, m.onNotice); err != nil {
				return fmt.Errorf("Error while running migrations: %w", err)
			}
		case *MigrationNoTx:
			if err := migrateNoTx(ctx, db, insertVersion, mm, m.onNotice); err != nil {
				return fmt.Errorf("Error while running migrations: %w", err)
			}
		}
	}

	return nil
}

// Pending returns all pending (not yet applied) migrations and count of migration applied
func (m *Migrator) Pending(ctx context.Context, db *sql.DB) ([]interface{}, int, error) {
	count, err := countApplied(ctx, db, m.TableName)
	if err != nil {
		return nil, 0, err
	}
	if count > len(m.migrations) {
		count = len(m.migrations)
	}
	return m.migrations[count:len(m.migrations)], count, nil
}

// NeedUpgrade returns True if database need to be updated with migrations
func (m *Migrator) NeedUpgrade(ctx context.Context, db *sql.DB) (bool, error) {
	exists, err := tableExists(ctx, db, m.TableName)
	if !exists {
		return true, err
	}
	mm, _, err := m.Pending(ctx, db)
	return len(mm) > 0, err
}

func countApplied(ctx context.Context, db *sql.DB, tableName string) (int, error) {
	// count applied migrations
	var count int
	err := db.QueryRowContext(ctx, fmt.Sprintf("SELECT count(*) FROM %s", tableName)).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

func tableExists(ctx context.Context, db *sql.DB, tableName string) (bool, error) {
	var exists bool
	err := db.QueryRowContext(ctx, "SELECT to_regclass($1) IS NOT NULL", tableName).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// Migration represents a single migration
type Migration struct {
	Name string
	Func func(*sql.Tx) error
}

// String returns a string representation of the migration
func (m *Migration) String() string {
	return m.Name
}

// MigrationNoTx represents a single not transactional migration
type MigrationNoTx struct {
	Name string
	Func func(context.Context, *sql.DB) error
}

func (m *MigrationNoTx) String() string {
	return m.Name
}

func migrate(ctx context.Context, db *sql.DB, insertVersion string, migration *Migration, notice func(string)) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if errRb := tx.Rollback(); errRb != nil {
				err = fmt.Errorf("Error rolling back: %s\n%s", errRb, err)
			}
			return
		}
		err = tx.Commit()
	}()
	notice(fmt.Sprintf("Applying migration named '%s'...", migration.Name))
	if err = migration.Func(tx); err != nil {
		return fmt.Errorf("Error executing golang migration: %w", err)
	}
	if _, err = tx.Exec(insertVersion); err != nil {
		return fmt.Errorf("Error updating migration versions: %w", err)
	}
	notice(fmt.Sprintf("Applied migration named '%s'", migration.Name))

	return err
}

func migrateNoTx(ctx context.Context, db *sql.DB, insertVersion string, migration *MigrationNoTx, notice func(string)) error {
	notice(fmt.Sprintf("Applying no tx migration named '%s'...", migration.Name))
	if err := migration.Func(ctx, db); err != nil {
		return fmt.Errorf("Error executing golang migration: %w", err)
	}
	if _, err := db.ExecContext(ctx, insertVersion); err != nil {
		return fmt.Errorf("Error updating migration versions: %w", err)
	}
	notice(fmt.Sprintf("Applied no tx migration named '%s'...", migration.Name))

	return nil
}
