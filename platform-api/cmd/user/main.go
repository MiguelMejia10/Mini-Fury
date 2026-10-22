// Explicit, one-time bootstrap. No default password and no public signup route.
package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"mini-operador-ti/platform-api/internal/identity"
	"mini-operador-ti/platform-api/internal/platform"
	"os"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Bootstrap no completado:", err)
		os.Exit(1)
	}
}
func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	in := identity.NewUser{Email: os.Getenv("ADMIN_EMAIL"), Name: os.Getenv("ADMIN_NAME"), Password: os.Getenv("ADMIN_PASSWORD"), Role: "admin"}
	if err := in.Validate(); err != nil {
		return err
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(2)
	if err = platform.Migrate(ctx, db); err != nil {
		return err
	}
	service, err := identity.NewService(&identity.Postgres{DB: db})
	if err != nil {
		return err
	}
	_, err = service.CreateUser(ctx, nil, in)
	if errors.Is(err, identity.ErrForbidden) {
		fmt.Println("Ya hay usuarios: bootstrap omitido; ninguna contraseña fue cambiada.")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Println("Administrador inicial creado. La contraseña no se imprime en logs.")
	return nil
}
