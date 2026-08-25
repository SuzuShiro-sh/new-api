package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

type namedDialector struct {
	gorm.Dialector
	name string
}

func (dialector namedDialector) Name() string {
	return dialector.name
}

// lockForUpdate must emit FOR UPDATE on databases that support it and skip
// it on SQLite, where the syntax does not exist.
//
// The dummy dialector is used because SQLite drivers strip locking clauses
// from the generated SQL, which would mask what the helper itself does.
func TestLockForUpdateEmitsRowLock(t *testing.T) {
	buildSQL := func(dialect string) string {
		dummyDB, err := gorm.Open(namedDialector{
			Dialector: tests.DummyDialector{},
			name:      dialect,
		}, &gorm.Config{DryRun: true})
		require.NoError(t, err)
		var rows []Redemption
		return lockForUpdate(dummyDB).Where("id = ?", 1).Find(&rows).Statement.SQL.String()
	}

	assert.Contains(t, buildSQL("mysql"), "FOR UPDATE")
	assert.Contains(t, buildSQL("postgres"), "FOR UPDATE")
	assert.NotContains(t, buildSQL("sqlite"), "FOR UPDATE")
}
