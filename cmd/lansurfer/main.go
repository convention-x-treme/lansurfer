package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/app"
	"github.com/convention-x-treme/lansurfer/internal/notification"
	"github.com/convention-x-treme/lansurfer/internal/platform/config"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Args[1:], logger, os.Stdout); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("command failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, arguments []string, logger *slog.Logger, output io.Writer) error {
	if len(arguments) == 1 && arguments[0] == "version" {
		_, err := fmt.Fprintf(output, "LANsurfer %s (%s)\n", version, commit)
		return err
	}

	if len(arguments) == 1 && (arguments[0] == "help" || arguments[0] == "--help" || arguments[0] == "-h") {
		return writeHelp(output)
	}

	if len(arguments) > 0 && arguments[0] == "healthcheck" {
		return healthcheck(ctx, arguments[1:], output)
	}
	if len(arguments) == 0 {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load configuration: %w", err)
		}
		if err := app.Run(ctx, cfg, logger, version); err != nil {
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
				problem.DeliveryID, problem.Status, problem.Target.Transport, notificationProblemGameRequestID(problem), problem.LastError,
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

func notificationProblemGameRequestID(problem notification.ProblemDelivery) string {
	if problem.StartReminder != nil {
		return problem.StartReminder.GameRequestID
	}
	return problem.Activity.GameRequestID
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
func writeHelp(output io.Writer) error {
	_, err := fmt.Fprint(output, `LANsurfer command line

Usage:
  lansurfer
  lansurfer version
  lansurfer healthcheck [--url URL]
  lansurfer help | -h | --help
  lansurfer admin users
  lansurfer admin promote --user-id <UUID>
  lansurfer admin notifications problems
  lansurfer admin notifications generate-secrets
  lansurfer admin notifications retry --delivery-id <ID>
  lansurfer admin notifications retry-uncertain --delivery-id <ID>

With no arguments, start the LANsurfer server.
`)
	return err
}

func healthcheck(ctx context.Context, arguments []string, output io.Writer) error {
	flags := flag.NewFlagSet("healthcheck", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	endpoint := flags.String("url", "http://127.0.0.1:8080/readyz", "readiness endpoint URL")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return errors.New("usage: lansurfer healthcheck [--url URL]")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, *endpoint, nil)
	if err != nil {
		return errors.New("invalid healthcheck URL")
	}
	client := &http.Client{
		Timeout: 2 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return errors.New("healthcheck request failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck returned HTTP status %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 7))
	if err != nil || string(body) != "ready\n" {
		return errors.New("healthcheck returned an invalid readiness response")
	}
	_, err = fmt.Fprintln(output, "LANsurfer is ready.")
	return err
}

func usageError() error {
	return errors.New("usage: lansurfer [version | healthcheck [--url URL] | help | " +
		"admin users | admin promote --user-id <UUID> | " +
		"admin notifications problems | admin notifications generate-secrets | " +
		"admin notifications retry --delivery-id <ID> | " +
		"admin notifications retry-uncertain --delivery-id <ID>]")
}
