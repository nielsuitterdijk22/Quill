// Command demo-tenant-plan applies the embedded migrations to the database
// named by QUILL_TEST_DATABASE_URL and prints the resulting tenants table
// columns. It exists to demonstrate migration 000019 (the plan column) from
// the outside; see .foundry/demo.sh.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/nielsuitterdijk22/quill/internal/store"
)

func main() {
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		log.Fatal("QUILL_TEST_DATABASE_URL is not set")
	}
	if err := store.Migrate(dsn); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close()
	rows, err := st.Pool().Query(context.Background(),
		`SELECT column_name, data_type, column_default, is_nullable
		 FROM information_schema.columns
		 WHERE table_name = 'tenants' ORDER BY ordinal_position`)
	if err != nil {
		log.Fatalf("query columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name, dataType, nullable string
		var def any
		if err := rows.Scan(&name, &dataType, &def, &nullable); err != nil {
			log.Fatalf("scan: %v", err)
		}
		fmt.Printf("%-16s %-14s default=%v nullable=%s\n", name, dataType, def, nullable)
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("rows: %v", err)
	}
}
