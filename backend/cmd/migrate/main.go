package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
)

func main() {
	var (
		dir     = flag.String("dir", filepath.Join("pkg", "storage", "cockroach", "migrations"), "migration directory")
		timeout = flag.Duration("timeout", 30*time.Second, "database timeout")
	)
	flag.Parse()

	command := "up"
	if flag.NArg() > 0 {
		command = strings.TrimSpace(flag.Arg(0))
	}
	if command != "up" {
		fatalf("unsupported command %q (only \"up\" is supported)", command)
	}

	_ = godotenv.Load(".env")
	dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
	if dsn == "" {
		dsn = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if dsn == "" {
		fatalf("DB_DSN or DATABASE_URL is required")
	}

	files, err := collectSQLFiles(*dir)
	if err != nil {
		fatalf("collect migrations: %v", err)
	}
	if len(files) == 0 {
		fatalf("no migration files found in %s", *dir)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		fatalf("open db: %v", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		fatalf("ping db: %v", err)
	}

	for _, path := range files {
		sqlBytes, readErr := os.ReadFile(path)
		if readErr != nil {
			fatalf("read migration %s: %v", path, readErr)
		}
		if _, execErr := db.ExecContext(ctx, string(sqlBytes)); execErr != nil {
			fatalf("apply migration %s: %v", filepath.Base(path), execErr)
		}
		fmt.Printf("applied %s\n", filepath.Base(path))
	}
}

func collectSQLFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(strings.ToLower(name), ".sql") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, errors.New("no .sql files found")
	}
	return files, nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
