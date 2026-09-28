package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/convention-x-treme/lansurfer/internal/app"
	"github.com/convention-x-treme/lansurfer/internal/platform/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], logger, os.Stdout); err != nil {
		logger.Error("command failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, logger *slog.Logger, output io.Writer) error {
	if len(arguments) == 0 {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load configuration: %w", err)
		}
		if err := app.Run(ctx, cfg, logger); err != nil {
			return fmt.Errorf("application stopped: %w", err)
		}
		return nil
	}

	if len(arguments) == 2 && arguments[0] == "admin" && arguments[1] == "users" {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load configuration: %w", err)
		}
		accounts, err := app.ListUsers(ctx, cfg, logger)
		if err != nil {
			return err
		}
		if len(accounts) == 0 {
			_, err = fmt.Fprintln(output, "No local users found. Log in once before promoting an account.")
			return err
		}
		if _, err := fmt.Fprintln(output, "USER ID\tROLE\tGAME NICK"); err != nil {
			return err
		}
		for _, account := range accounts {
			if _, err := fmt.Fprintf(output, "%s\t%s\t%s\n", account.ID, account.Role, account.GameNick); err != nil {
				return err
			}
		}
		return nil
	}

	if len(arguments) >= 2 && arguments[0] == "admin" && arguments[1] == "promote" {
		userID, err := promoteUserID(arguments[2:])
		if err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load configuration: %w", err)
		}
		account, err := app.PromoteAdmin(ctx, cfg, userID, logger)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "Promoted %s (%s) to admin.\n", account.GameNick, account.ID)
		return err
	}

	if len(arguments) == 3 && arguments[0] == "admin" && arguments[1] == "notifications" && arguments[2] == "generate-secrets" {
		return writeNotificationSecrets(output)
	}

	if len(arguments) == 3 && arguments[0] == "admin" && arguments[1] == "notifications" && arguments[2] == "problems" {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load configuration: %w", err)
		}
		problems, err := app.ListNotificationProblems(ctx, cfg, logger)
		if err != nil {
			return err
		}
		if len(problems) == 0 {
			_, err = fmt.Fprintln(output, "No exhausted, permanent, or uncertain notification deliveries.")
			return err
		}
		if _, err := fmt.Fprintln(output, "DELIVERY ID\tSTATUS\tTRANSPORT\tGAME REQUEST\tLAST ERROR"); err != nil {
			return err
		}
		for _, problem := range problems {
			if _, err := fmt.Fprintf(output, "%d\t%s\t%s\t%s\t%s\n",
				problem.DeliveryID, problem.Status, problem.Target.Transport, problem.Activity.GameRequestID, problem.LastError,
			); err != nil {
				return err
			}
		}
		return nil
	}

	if len(arguments) >= 3 && arguments[0] == "admin" && arguments[1] == "notifications" &&
		(arguments[2] == "retry" || arguments[2] == "retry-uncertain") {
		deliveryID, err := notificationDeliveryID(arguments[3:])
		if err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load configuration: %w", err)
		}
		if arguments[2] == "retry-uncertain" {
			err = app.RetryUncertainNotificationDelivery(ctx, cfg, deliveryID, logger)
		} else {
			err = app.RetryNotificationDelivery(ctx, cfg, deliveryID, logger)
		}
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "Reopened notification delivery %s for retry.\n", deliveryID)
		return err
	}

	return usageError()
}

func promoteUserID(arguments []string) (string, error) {
	flags := flag.NewFlagSet("admin promote", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	userID := flags.String("user-id", "", "local LANsurfer user UUID")
	if err := flags.Parse(arguments); err != nil {
		return "", usageError()
	}
	if *userID == "" || flags.NArg() != 0 {
		return "", usageError()
	}
	return *userID, nil
}

func notificationDeliveryID(arguments []string) (string, error) {
	flags := flag.NewFlagSet("admin notifications retry", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	deliveryID := flags.String("delivery-id", "", "notification delivery ID")
	if err := flags.Parse(arguments); err != nil {
		return "", usageError()
	}
	if *deliveryID == "" || flags.NArg() != 0 {
		return "", usageError()
	}
	return *deliveryID, nil
}

func usageError() error {
	return errors.New("usage: lansurfer [admin users | admin promote --user-id <UUID> | " +
		"admin notifications problems | admin notifications generate-secrets | " +
		"admin notifications retry --delivery-id <ID> | " +
		"admin notifications retry-uncertain --delivery-id <ID>]")
}
