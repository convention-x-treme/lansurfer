package app

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/notification"
	notificationpostgres "github.com/convention-x-treme/lansurfer/internal/notification/postgres"
	"github.com/convention-x-treme/lansurfer/internal/platform/config"
	platformpostgres "github.com/convention-x-treme/lansurfer/internal/platform/postgres"
	gamerequestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/user"
	userpostgres "github.com/convention-x-treme/lansurfer/internal/user/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newNotificationWorker ties the worker to the caller's root context by
// convention (the caller starts it with `go worker.Run(ctx)` using the
// application's own ctx), matching appBoundIGDBSync's rationale: a
// background worker must keep running, and be cancellable only by
// application shutdown, independent of any single request.
func newNotificationWorker(store *notificationpostgres.Store, transports []notification.Transport, logger *slog.Logger) *notification.Worker {
	return notification.NewWorker(store, transports, logger, time.Now)
}

// newSubscriptionService constructs the Web Push subscription/preference
// service the settings handler and logout hook consume. store satisfies
// both notification.SubscriptionRepository and notification.
// PreferenceRepository, matching SubscriptionService's constructor.
func newSubscriptionService(store *notificationpostgres.Store, encryptionKey []byte) (*notification.SubscriptionService, error) {
	return notification.NewSubscriptionService(store, store, encryptionKey)
}

// notificationTransports constructs the runtime's Web Push transport once for
// both worker delivery and service eligibility checks.
func notificationTransports(cfg config.Config, store *notificationpostgres.Store, followers *game.DiscoveryService, requests *gamerequestquery.Store, users *user.Service) ([]notification.Transport, error) {
	if cfg.WebPush == nil {
		return nil, nil
	}
	if len(cfg.DataEncryptionKey) != 32 {
		return nil, fmt.Errorf("construct Web Push transport: invalid data-encryption key")
	}
	sender, err := notification.NewWebPushSender(cfg.WebPush.PublicKey, cfg.WebPush.PrivateKey, cfg.WebPush.Subject)
	if err != nil {
		return nil, fmt.Errorf("construct Web Push transport: %w", err)
	}
	instanceName := cfg.InstanceName
	if instanceName == "" {
		instanceName = "LANsurfer"
	}
	return []notification.Transport{notification.NewWebPushTransport(
		store, store, store, followers, requests, notificationRequestSummaries{requests}, notificationGameNickReader{users},
		cfg.BaseURL, instanceName, cfg.DataEncryptionKey, sender,
	)}, nil
}

type notificationRequestSummaries struct{ requests *gamerequestquery.Store }

func (r notificationRequestSummaries) GetGameRequestSummary(ctx context.Context, id string) (notification.GameRequestSummary, error) {
	summary, err := r.requests.GetGameRequestSummary(ctx, id)
	if err != nil {
		return notification.GameRequestSummary{}, err
	}
	return notification.GameRequestSummary{
		RequestID: summary.RequestID, GameID: summary.GameID, GameName: summary.GameName,
		StartsAt: summary.StartsAt, Status: string(summary.Status), MemberCount: summary.MemberCount,
		MaxPlayers: summary.MaxPlayers, CreatorGameNick: summary.CreatorGameNick, Description: summary.Description,
	}, nil
}

type notificationGameNickReader struct {
	users *user.Service
}

func (r notificationGameNickReader) GetGameNick(ctx context.Context, userID string) (string, error) {
	account, err := r.users.ByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return account.GameNick, nil
}

// cliNotificationTransports shares runtime construction; provider I/O only
// happens when a worker later sends a claimed delivery.
func cliNotificationTransports(cfg config.Config, pool *pgxpool.Pool) ([]notification.Transport, error) {
	if cfg.WebPush == nil {
		return nil, nil
	}
	gameStore := gamepostgres.NewStore(pool)
	return notificationTransports(
		cfg, notificationpostgres.NewStore(pool),
		game.NewDiscoveryService(gameStore, gameStore, gameStore, nil),
		gamerequestquery.NewStore(pool), user.NewService(userpostgres.NewStore(pool)),
	)
}

// ListNotificationProblems opens its own standalone database connection and
// reports every exhausted, permanent, or uncertain delivery for operator
// diagnosis, following the same one-shot pattern as ListUsers.
func ListNotificationProblems(ctx context.Context, cfg config.Config, logger *slog.Logger) ([]notification.ProblemDelivery, error) {
	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseStartupTimeout)
	pool, err := platformpostgres.Connect(databaseCtx, cfg.DatabaseURL, logger)
	cancelDatabase()
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, migrationTimeout)
	err = platformpostgres.Migrate(migrationCtx, pool, logger)
	cancelMigration()
	if err != nil {
		return nil, fmt.Errorf("apply database migrations: %w", err)
	}

	service := notification.NewService(notificationpostgres.NewStore(pool), nil)
	problems, err := service.ListProblems(ctx)
	if err != nil {
		return nil, fmt.Errorf("list notification problems: %w", err)
	}
	return problems, nil
}

// RetryNotificationDelivery reopens one exhausted or permanent delivery for
// a fresh automatic retry budget, after re-checking present-day eligibility.
func RetryNotificationDelivery(ctx context.Context, cfg config.Config, deliveryID string, logger *slog.Logger) error {
	return retryNotificationDelivery(ctx, cfg, deliveryID, logger, false)
}

// RetryUncertainNotificationDelivery reopens one uncertain delivery despite
// the possibility of a duplicate externally visible effect. It is a
// distinct action from RetryNotificationDelivery.
func RetryUncertainNotificationDelivery(ctx context.Context, cfg config.Config, deliveryID string, logger *slog.Logger) error {
	return retryNotificationDelivery(ctx, cfg, deliveryID, logger, true)
}

func retryNotificationDelivery(ctx context.Context, cfg config.Config, deliveryID string, logger *slog.Logger, uncertain bool) error {
	id, err := strconv.ParseInt(deliveryID, 10, 64)
	if err != nil {
		return fmt.Errorf("parse delivery ID: %w", err)
	}

	databaseCtx, cancelDatabase := context.WithTimeout(ctx, databaseStartupTimeout)
	pool, err := platformpostgres.Connect(databaseCtx, cfg.DatabaseURL, logger)
	cancelDatabase()
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL: %w", err)
	}
	defer pool.Close()

	migrationCtx, cancelMigration := context.WithTimeout(ctx, migrationTimeout)
	err = platformpostgres.Migrate(migrationCtx, pool, logger)
	cancelMigration()
	if err != nil {
		return fmt.Errorf("apply database migrations: %w", err)
	}

	transports, err := cliNotificationTransports(cfg, pool)
	if err != nil {
		return err
	}
	service := notification.NewService(notificationpostgres.NewStore(pool), transports)
	if uncertain {
		return service.ManualRetryUncertain(ctx, id)
	}
	return service.ManualRetry(ctx, id)
}
