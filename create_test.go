package sqlserver_test

import (
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/sqlserver"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type testSchemaUser struct {
	ID   int64 `gorm:"primaryKey"`
	Name string
}

func (testSchemaUser) TableName() string {
	return "users"
}

func setupSchemaTable(t *testing.T, db *gorm.DB) {
	t.Helper()

	if err := db.Exec("CREATE SCHEMA testschema").Error; err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := db.Exec("DROP TABLE IF EXISTS testschema.users").Error; err != nil {
			t.Error(err)
		}
		if err := db.Exec("DROP SCHEMA IF EXISTS testschema").Error; err != nil {
			t.Error(err)
		}
	})

	if err := db.Exec(`
		CREATE TABLE testschema.users (
			id BIGINT IDENTITY(1,1) PRIMARY KEY,
			name NVARCHAR(100)
		)
	`).Error; err != nil {
		t.Fatal(err)
	}
}

func assertUser(t *testing.T, db *gorm.DB, id int64, name string) {
	t.Helper()

	var got testSchemaUser
	if err := db.Table("testschema.users").First(&got, id).Error; err != nil {
		t.Fatal(err)
	}

	if got.Name != name {
		t.Fatalf("expected %q, got %q", name, got.Name)
	}
}

func TestCreateWithSchemaTable(t *testing.T) {
	db, err := gorm.Open(sqlserver.Open(sqlserverDSN))
	if err != nil {
		t.Fatal(err)
	}

	setupSchemaTable(t, db)

	if err := db.Table("testschema.users").Create(&testSchemaUser{
		ID:   1,
		Name: "gorm",
	}).Error; err != nil {
		t.Fatal(err)
	}

	assertUser(t, db, 1, "gorm")
}

func TestSaveWithSchemaTable(t *testing.T) {
	db, err := gorm.Open(sqlserver.Open(sqlserverDSN))
	if err != nil {
		t.Fatal(err)
	}

	setupSchemaTable(t, db)

	if err := db.Table("testschema.users").Save(&testSchemaUser{
		ID:   1,
		Name: "gorm",
	}).Error; err != nil {
		t.Fatal(err)
	}

	assertUser(t, db, 1, "gorm")
}

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

	db, err := gorm.Open(sqlserver.New(sqlserver.Config{Conn: sqlDB}), &gorm.Config{
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
	if !strings.Contains(sql, `MERGE INTO "testschema"."users" AS "target"`) {
		t.Fatalf("expected schema-qualified MERGE target with alias, got SQL: %s", sql)
	}
	if !strings.Contains(sql, `"target"."id" = "excluded"."id"`) {
		t.Fatalf("expected target alias column in ON clause, got SQL: %s", sql)
	}
}

func TestCreateSQL_MergeWithTableOverride(t *testing.T) {
	db := openDryRunDB(t)

	tx := db.Table("archive.custom_users").Clauses(clause.OnConflict{
		DoUpdates: clause.AssignmentColumns([]string{"name"}),
	}).Create(&testSchemaUser{ID: 1, Name: "alice"})
	if tx.Error != nil {
		t.Fatalf("create failed: %v", tx.Error)
	}

	sql := tx.Statement.SQL.String()
	if !strings.Contains(sql, `MERGE INTO "archive"."custom_users" AS "target"`) {
		t.Fatalf("expected MERGE INTO \"archive\".\"custom_users\" AS \"target\", got: %s", sql)
	}
	if !strings.Contains(sql, `"target"."id" = "excluded"."id"`) {
		t.Fatalf("expected ON \"target\".\"id\" = \"excluded\".\"id\", got: %s", sql)
	}
}

func TestCreateSQL_MergeWithMap(t *testing.T) {
	db := openDryRunDB(t)

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panicked on map with OnConflict: %v", r)
		}
	}()

	tx := db.Table("users").Clauses(clause.OnConflict{
		DoUpdates: clause.AssignmentColumns([]string{"name"}),
	}).Create(map[string]interface{}{"name": "alice"})
	if tx.Error != nil {
		t.Fatalf("create failed: %v", tx.Error)
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
