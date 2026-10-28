package persistence_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/mptooling/notifycat/internal/platform/persistence"
)

func TestOpen_EnablesForeignKeysOnEveryPooledConnection(t *testing.T) {
	db := persistence.NewTestDB(t)
	connections := holdConnections(t, db, 3)

	enabled := foreignKeysPerConnection(t, connections)

	assert.Equal(t, []int{1, 1, 1}, enabled)
}

func TestOpen_KeepsOperatorQueryParameters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "query.db")
	db, err := persistence.Open("file:" + path + "?_pragma=busy_timeout(7000)")
	require.NoError(t, err)
	connections := holdConnections(t, db, 2)

	enabled := foreignKeysPerConnection(t, connections)
	busyTimeout := pragmaValue(t, connections[1], "busy_timeout")

	assert.Equal(t, []int{1, 1}, enabled)
	assert.Equal(t, 7000, busyTimeout)
}

func TestOpen_OverridesOperatorForeignKeysOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "off.db")
	db, err := persistence.Open("file:" + path + "?_pragma=foreign_keys(0)")
	require.NoError(t, err)
	connections := holdConnections(t, db, 2)

	enabled := foreignKeysPerConnection(t, connections)

	assert.Equal(t, []int{1, 1}, enabled, "foreign keys must stay on even when the operator turns them off")
}

func TestOpen_AcceptsBarePath(t *testing.T) {
	db, err := persistence.Open(filepath.Join(t.TempDir(), "bare.db"))
	require.NoError(t, err)
	connections := holdConnections(t, db, 2)

	enabled := foreignKeysPerConnection(t, connections)

	assert.Equal(t, []int{1, 1}, enabled)
}

func TestOpen_CascadesOnASecondConnection(t *testing.T) {
	db := persistence.NewTestDB(t)
	connections := holdConnections(t, db, 2)
	ctx := context.Background()
	_, err := connections[1].ExecContext(ctx,
		"INSERT INTO pull_requests (id, gh_repository, pr_number, created_at, updated_at) VALUES (1, 'o/r', 1, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)")
	require.NoError(t, err)
	_, err = connections[1].ExecContext(ctx,
		"INSERT INTO messages (pull_request_id, channel, message_id) VALUES (1, 'C1', '1.1')")
	require.NoError(t, err)

	_, err = connections[1].ExecContext(ctx, "DELETE FROM pull_requests")

	require.NoError(t, err)
	assert.Equal(t, 0, countRows(t, connections[1], "messages"),
		"deleting a PR must cascade on any pooled connection")
}

func holdConnections(t *testing.T, db *gorm.DB, count int) []*sql.Conn {
	t.Helper()
	sqlDB, err := persistence.SQLDB(db)
	require.NoError(t, err)
	connections := make([]*sql.Conn, 0, count)
	for range count {
		connection, err := sqlDB.Conn(context.Background())
		require.NoError(t, err)
		connections = append(connections, connection)
	}
	t.Cleanup(func() {
		for _, connection := range connections {
			_ = connection.Close()
		}
	})
	return connections
}

func foreignKeysPerConnection(t *testing.T, connections []*sql.Conn) []int {
	t.Helper()
	enabled := make([]int, 0, len(connections))
	for _, connection := range connections {
		enabled = append(enabled, pragmaValue(t, connection, "foreign_keys"))
	}
	return enabled
}

func pragmaValue(t *testing.T, connection *sql.Conn, name string) int {
	t.Helper()
	var value int
	err := connection.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&value)
	require.NoError(t, err)
	return value
}

func countRows(t *testing.T, connection *sql.Conn, table string) int {
	t.Helper()
	var count int
	err := connection.QueryRowContext(context.Background(), fmt.Sprintf("SELECT COUNT(*) FROM %s", table)).Scan(&count)
	require.NoError(t, err)
	return count
}
