package sqlserver

import (
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type mergeSchemaTableModel struct {
	ID   uint   `gorm:"primaryKey"`
	Name string `gorm:"column:name"`
}

func (mergeSchemaTableModel) TableName() string {
	return "testschema.users"
}

type identityInsertSchemaTableModel struct {
	ID   uint   `gorm:"primaryKey;autoIncrement"`
	Name string `gorm:"column:name"`
}

func (identityInsertSchemaTableModel) TableName() string {
	return "testschema.identity_models"
}

func openDryRunDB(t *testing.T) *gorm.DB {
	t.Helper()

	sqlDB, _, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	db, err := gorm.Open(New(Config{Conn: sqlDB}), &gorm.Config{
		DryRun:                 true,
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("failed to open dry run db: %v", err)
	}
	return db
}

func TestCreateSQL_MergeUsesSchemaQualifiedTable(t *testing.T) {
	db := openDryRunDB(t)

	tx := db.Clauses(clause.OnConflict{
		DoUpdates: clause.AssignmentColumns([]string{"name"}),
	}).Create(&mergeSchemaTableModel{ID: 1, Name: "alice"})
	if tx.Error != nil {
		t.Fatalf("create failed: %v", tx.Error)
	}

	sql := tx.Statement.SQL.String()
	if !strings.Contains(sql, `MERGE INTO "testschema"."users"`) {
		t.Fatalf("expected schema-qualified MERGE target, got SQL: %s", sql)
	}
	if !strings.Contains(sql, `"testschema"."users"."id"`) {
		t.Fatalf("expected schema-qualified target column in ON clause, got SQL: %s", sql)
	}
}

func TestCreateSQL_IdentityInsertUsesSchemaQualifiedTable(t *testing.T) {
	db := openDryRunDB(t)

	tx := db.Create(&identityInsertSchemaTableModel{ID: 42, Name: "bob"})
	if tx.Error != nil {
		t.Fatalf("create failed: %v", tx.Error)
	}

	sql := tx.Statement.SQL.String()
	if !strings.Contains(sql, `SET IDENTITY_INSERT "testschema"."identity_models" ON;`) {
		t.Fatalf("expected schema-qualified IDENTITY_INSERT ON, got SQL: %s", sql)
	}
	if !strings.Contains(sql, `SET IDENTITY_INSERT "testschema"."identity_models" OFF;`) {
		t.Fatalf("expected schema-qualified IDENTITY_INSERT OFF, got SQL: %s", sql)
	}
}
