package sqlserver_test

import (
	"regexp"
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

type compositeKeySchemaModel struct {
	TenantID uint   `gorm:"primaryKey"`
	ID       uint   `gorm:"primaryKey"`
	Name     string `gorm:"column:name"`
}

func (compositeKeySchemaModel) TableName() string {
	return "testschema.composite_models"
}

type noPrimaryKeyModel struct {
	Name string `gorm:"column:name"`
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

func TestCreateSQL_MergeWithCompositePrimaryKey(t *testing.T) {
	db := openDryRunDB(t)

	tx := db.Clauses(clause.OnConflict{
		DoUpdates: clause.AssignmentColumns([]string{"name"}),
	}).Create(&compositeKeySchemaModel{TenantID: 1, ID: 2, Name: "charlie"})
	if tx.Error != nil {
		t.Fatalf("create failed: %v", tx.Error)
	}

	sql := tx.Statement.SQL.String()
	if !strings.Contains(sql, `MERGE INTO "testschema"."composite_models" AS "target"`) {
		t.Fatalf("expected schema-qualified MERGE target with alias, got SQL: %s", sql)
	}
	expectedOn := `"target"."tenant_id" = "excluded"."tenant_id" AND "target"."id" = "excluded"."id"`
	if !strings.Contains(sql, expectedOn) {
		t.Fatalf("expected composite key ON clause %q, got SQL: %s", expectedOn, sql)
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

	sql := tx.Statement.SQL.String()
	if strings.Contains(sql, "MERGE INTO") {
		t.Fatalf("expected map create without schema to fall back to INSERT, got: %s", sql)
	}
	if !strings.Contains(sql, `INSERT INTO "users"`) {
		t.Fatalf("expected INSERT fallback, got: %s", sql)
	}
}

func TestCreateSQL_NoPrimaryKeyFallsBackToInsert(t *testing.T) {
	db := openDryRunDB(t)

	tx := db.Model(&noPrimaryKeyModel{}).Clauses(clause.OnConflict{
		DoUpdates: clause.AssignmentColumns([]string{"name"}),
	}).Create(&noPrimaryKeyModel{Name: "alice"})
	if tx.Error != nil {
		t.Fatalf("create failed: %v", tx.Error)
	}

	sql := tx.Statement.SQL.String()
	if strings.Contains(sql, "MERGE INTO") {
		t.Fatalf("expected model without primary key to fall back to INSERT, got: %s", sql)
	}
}

func TestMergeCreate_DirectCallGuard(t *testing.T) {
	db := openDryRunDB(t)

	// Direct call with nil schema
	stmt := &gorm.Statement{DB: db}
	dbNilSchema := db.Session(&gorm.Session{})
	dbNilSchema.Statement = stmt
	if sqlserver.MergeCreate(dbNilSchema, clause.OnConflict{}, clause.Values{}) {
		t.Fatalf("expected MergeCreate to return false for nil schema")
	}

	// Direct call with empty primary fields
	if err := db.Statement.Parse(&noPrimaryKeyModel{}); err != nil {
		t.Fatalf("failed to parse model: %v", err)
	}
	if sqlserver.MergeCreate(db, clause.OnConflict{}, clause.Values{}) {
		t.Fatalf("expected MergeCreate to return false for schema without primary keys")
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

type holdLockKeyValueModel struct {
	Bucket string `gorm:"primaryKey"`
	Key    string `gorm:"primaryKey"`
	Value  string
}

func (holdLockKeyValueModel) TableName() string {
	return "testschema.key_values"
}

func TestCreateSQL_MergeWithoutHoldLockByDefault(t *testing.T) {
	db := openDryRunDB(t)

	tx := db.Clauses(clause.OnConflict{UpdateAll: true}).Create(&holdLockKeyValueModel{Bucket: "b", Key: "k", Value: "v"})
	if tx.Error != nil {
		t.Fatalf("create failed: %v", tx.Error)
	}

	sql := tx.Statement.SQL.String()
	if strings.Contains(sql, "HOLDLOCK") {
		t.Fatalf("expected no HOLDLOCK hint without opt-in, got SQL: %s", sql)
	}
}

func TestCreateSQL_MergeWithHoldLock(t *testing.T) {
	db := openDryRunDB(t)

	tx := db.Set(sqlserver.MergeHoldLockKey, true).
		Clauses(clause.OnConflict{UpdateAll: true}).
		Create(&holdLockKeyValueModel{Bucket: "b", Key: "k", Value: "v"})
	if tx.Error != nil {
		t.Fatalf("create failed: %v", tx.Error)
	}

	sql := tx.Statement.SQL.String()
	if !strings.Contains(sql, `MERGE INTO "testschema"."key_values" WITH (HOLDLOCK) AS "target"`) {
		t.Fatalf("expected HOLDLOCK hint between MERGE target and alias, got SQL: %s", sql)
	}
}

// Save runs an UPDATE first and falls back to MERGE when no row was affected. The opt-in has
// to survive that fallback, since Save is the usual way to upsert.
func TestSave_MergeWithHoldLock(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("failed to create sqlmock: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	db, err := gorm.Open(sqlserver.New(sqlserver.Config{Conn: sqlDB}), &gorm.Config{
		SkipDefaultTransaction: true,
	})
	if err != nil {
		t.Fatalf("failed to open db: %v", err)
	}

	mock.ExpectExec(regexp.QuoteMeta(`UPDATE "testschema"."key_values"`)).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(regexp.QuoteMeta(`MERGE INTO "testschema"."key_values" WITH (HOLDLOCK) AS "target"`)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := db.Set(sqlserver.MergeHoldLockKey, true).Save(&holdLockKeyValueModel{Bucket: "b", Key: "k", Value: "v"}).Error; err != nil {
		t.Fatalf("save failed: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
