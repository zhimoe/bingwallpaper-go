package archive

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/zhimoe/bingwallpaper/internal/history"
)

func TestSaveCompatibleSchema(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "wallpapers.db")
	rec := history.NewRecord("https://example/w.jpg", `C:\Wallpapers\w.jpg`, "desc", "en-US", time.Now(), time.Now(), false, []byte("image"))
	if err := Save(filename, []history.Record{rec}); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, imageBytes int
	if err := db.QueryRow(`SELECT count(*), length(Image) FROM BingWallpaperRecords WHERE Url=?`, rec.URL).Scan(&count, &imageBytes); err != nil {
		t.Fatal(err)
	}
	if count != 1 || imageBytes != len(rec.Raw) {
		t.Fatalf("count=%d imageBytes=%d", count, imageBytes)
	}
	var version string
	if err := db.QueryRow(`SELECT printf('%d.%d.%d', MajorVer, MinorVer, Build) FROM BingWallpaperCore`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != "5.6.1" {
		t.Fatalf("version=%s", version)
	}
}

func TestUpgradeLegacySchema(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE BingWallpaperRecords (
 Url CHAR(1024) NOT NULL PRIMARY KEY, DownloadTime DATETIME NOT NULL,
 LocalFilePath CHAR(1024), Description TEXT(1024), Image BLOB,
 IsAccompany BOOLEAN DEFAULT FALSE)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	rec := history.NewRecord("https://example/legacy.jpg", "legacy.jpg", "legacy", "zh-CN", time.Now(), time.Now(), false, nil)
	if err := Save(filename, []history.Record{rec}); err != nil {
		t.Fatal(err)
	}
	db, err = sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var market string
	if err := db.QueryRow(`SELECT Market FROM BingWallpaperRecords WHERE Url=?`, rec.URL).Scan(&market); err != nil {
		t.Fatal(err)
	}
	if market != "zh-CN" {
		t.Fatalf("market=%q", market)
	}
}

func TestSchemaUpgradeRollsBackWhenInsertFails(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "rollback.db")
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE BingWallpaperRecords (
 Url CHAR(1024) NOT NULL PRIMARY KEY, DownloadTime DATETIME NOT NULL,
 LocalFilePath CHAR(1024), Description TEXT(1024), Image BLOB,
 IsAccompany BOOLEAN DEFAULT FALSE);
CREATE TRIGGER reject_insert BEFORE INSERT ON BingWallpaperRecords
BEGIN SELECT RAISE(ABORT, 'reject test insert'); END;`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	rec := history.NewRecord("https://example/fail.jpg", "fail.jpg", "fail", "en-US", time.Now(), time.Now(), false, nil)
	if err := Save(filename, []history.Record{rec}); err == nil {
		t.Fatal("expected insert failure")
	}
	db, err = sql.Open("sqlite", filename)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query(`PRAGMA table_info(BingWallpaperRecords)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatal(err)
		}
		if name == "Market" || name == "StartTime" || name == "EndTime" {
			t.Fatalf("schema change %s was not rolled back", name)
		}
	}
}
