package archive

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/zhimoe/bingwallpaper/internal/history"
	_ "modernc.org/sqlite"
)

// Save upgrades the compatible schema and archives records in one transaction.
func Save(filename string, records []history.Record) error {
	db, err := sql.Open("sqlite", filename)
	if err != nil {
		return err
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureSchema(tx); err != nil {
		return err
	}
	stmt := `INSERT OR REPLACE INTO BingWallpaperRecords
 (Url, DownloadTime, StartTime, EndTime, LocalFilePath, Description, Image, IsAccompany, Market)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, r := range records {
		if _, err := tx.Exec(stmt, r.URL, r.Time, r.StartTime, r.EndTime, r.LocalFile, r.Description, r.Raw, r.IsAccompany, r.Market); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type sqlExecutor interface {
	Exec(query string, args ...any) (sql.Result, error)
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

func ensureSchema(db sqlExecutor) error {
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND lower(name)='bingwallpaperrecords'`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		_, err := db.Exec(`
CREATE TABLE BingWallpaperRecords (
 Url CHAR(1024) NOT NULL PRIMARY KEY,
 DownloadTime DATETIME NOT NULL,
 StartTime DATETIME,
 EndTime DATETIME,
 LocalFilePath CHAR(1024),
 Description TEXT(1024),
 Market TEXT(64) DEFAULT '',
 Image BLOB,
 IsAccompany BOOLEAN DEFAULT FALSE
);
CREATE TABLE BingWallpaperCore (MajorVer INTEGER, MinorVer INTEGER, Build INTEGER);
INSERT INTO BingWallpaperCore (MajorVer, MinorVer, Build) VALUES (5, 6, 1);`)
		return err
	}

	columns, err := db.Query(`PRAGMA table_info(BingWallpaperRecords)`)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for columns.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var def any
		if err := columns.Scan(&cid, &name, &typ, &notNull, &def, &pk); err != nil {
			return err
		}
		present[strings.ToLower(name)] = true
	}
	if err := columns.Err(); err != nil {
		columns.Close()
		return err
	}
	if err := columns.Close(); err != nil {
		return err
	}
	columnsToAdd := [][2]string{
		{"Market", "TEXT(64) DEFAULT ''"},
		{"StartTime", "DATETIME DEFAULT NULL"},
		{"EndTime", "DATETIME DEFAULT NULL"},
	}
	for _, column := range columnsToAdd {
		if !present[strings.ToLower(column[0])] {
			if _, err := db.Exec(fmt.Sprintf("ALTER TABLE BingWallpaperRecords ADD COLUMN %s %s", column[0], column[1])); err != nil {
				return err
			}
		}
	}
	var coreCount int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND lower(name)='bingwallpapercore'`).Scan(&coreCount); err != nil {
		return err
	}
	if coreCount == 0 {
		_, err := db.Exec(`CREATE TABLE BingWallpaperCore (MajorVer INTEGER, MinorVer INTEGER, Build INTEGER);
INSERT INTO BingWallpaperCore (MajorVer, MinorVer, Build) VALUES (5, 6, 1)`)
		return err
	}
	var major, minor, build int
	if err := db.QueryRow(`SELECT MajorVer, MinorVer, Build FROM BingWallpaperCore LIMIT 1`).Scan(&major, &minor, &build); err != nil {
		return fmt.Errorf("读取数据库版本: %w", err)
	}
	if major > 5 || (major == 5 && minor > 6) || (major == 5 && minor == 6 && build > 1) {
		return fmt.Errorf("数据库版本 %d.%d.%d 高于支持的 5.6.1", major, minor, build)
	}
	if major != 5 || minor != 6 || build != 1 {
		_, err := db.Exec(`UPDATE BingWallpaperCore SET MajorVer=5, MinorVer=6, Build=1`)
		return err
	}
	return nil
}
