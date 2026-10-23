package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strings"
)

// migrations 内嵌全部幂等 schema（所有 DDL 均为 IF NOT EXISTS / OR REPLACE，
// 可在启动时安全重复执行）。内嵌进二进制，避免新环境"忘了手动 psql"导致缺表。
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// Migrate 按文件名顺序执行内嵌迁移，幂等。传入的 db 须具备建表/建扩展权限。
func Migrate(ctx context.Context, db *sql.DB) error {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("store: read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("store: read migration %s: %w", name, err)
		}
		if _, err := db.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("store: apply migration %s: %w", name, err)
		}
	}
	return nil
}
