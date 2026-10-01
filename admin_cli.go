package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/term"
)

func runAdminCommand(args []string) error {
	if len(args) != 1 || (args[0] != "username" && args[0] != "password") {
		return errors.New("usage: ctd username | ctd password")
	}

	databasePath := filepath.Join(stateDirectory(), "accounts.sqlite")
	if _, err := os.Stat(databasePath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("account database does not exist yet")
		}
		return err
	}
	store, err := openAuthStore(databasePath)
	if err != nil {
		return err
	}
	defer store.close()

	account, err := store.getProfile(context.Background())
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("no account has been created yet")
	}
	if err != nil {
		return err
	}

	switch args[0] {
	case "username":
		fmt.Fprintln(os.Stdout, account.Username)
		return nil
	case "password":
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("password reset needs an interactive terminal; use docker compose exec -it")
		}
		fmt.Fprintf(os.Stdout, "Resetting password for %s\n", account.Username)
		password, err := readHiddenPassword("New password: ")
		if err != nil {
			return err
		}
		confirmation, err := readHiddenPassword("Confirm new password: ")
		if err != nil {
			return err
		}
		if password != confirmation {
			return errors.New("passwords do not match")
		}
		if err := store.resetPassword(context.Background(), password); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Password updated. All active sessions have been signed out.")
		return nil
	default:
		return errors.New("usage: ctd username | ctd password")
	}
}

func readHiddenPassword(prompt string) (string, error) {
	fmt.Fprint(os.Stdout, prompt)
	password, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return "", err
	}
	return string(password), nil
}
