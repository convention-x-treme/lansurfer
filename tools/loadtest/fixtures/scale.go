package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/convention-x-treme/lansurfer/internal/game"
	gamepostgres "github.com/convention-x-treme/lansurfer/internal/game/postgres"
	"github.com/convention-x-treme/lansurfer/internal/gamerequest"
	gamerequestpostgres "github.com/convention-x-treme/lansurfer/internal/gamerequest/postgres"
	gamerequestquery "github.com/convention-x-treme/lansurfer/internal/query/gamerequest"
	"github.com/convention-x-treme/lansurfer/tools/loadtest/internal/target"
	"github.com/jackc/pgx/v5/pgxpool"
)

type scaleProgress struct {
	TargetGames          int    `json:"target_games"`
	TargetRequests       int    `json:"target_requests"`
	TargetChatMessages   int    `json:"messages_per_request"`
	BaseRequests         int    `json:"base_requests"`
	GamesFollowed        int    `json:"games_followed"`
	RequestsComplete     int    `json:"requests_complete"`
	ChatMessagesComplete int    `json:"chat_messages_complete"`
	CyclesComplete       int    `json:"cycles_complete"`
	ChatRequestID        string `json:"chat_request_id,omitempty"`
}

func scaledGameIdentity(index int) (name, slug string) {
	return fmt.Sprintf("Loadtest Game %04d", index), fmt.Sprintf("loadtest-large-v1-game-%04d", index)
}

func largeRequestDescription(index int) string {
	return fmt.Sprintf("loadtest large-v1 request %06d", index)
}

func countRequests(fixtures fixtureFile) int {
	count := 0
	for _, ids := range fixtures.Requests {
		count += len(ids)
	}
	return count
}
func planLargeRequests(users, games []string, count int) []plannedRequest {
	plans := planRequests(users, games, count, 139)
	for i := range plans {
		plan := &plans[i]
		if plan.Kind != "empty" && len(plan.Joiners)+1 >= int(plan.MaxPlayers) {
			plan.Joiners = plan.Joiners[:len(plan.Joiners)-1]
		}
	}
	return plans
}

func writeFixtureFile(path string, fixtures fixtureFile) error {
	directory := filepath.Dir(path)
	file, err := os.CreateTemp(directory, ".loadtest-fixtures-*")
	if err != nil {
		return fmt.Errorf("create fixture checkpoint: %w", err)
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return fmt.Errorf("protect fixture checkpoint: %w", err)
	}
	if err := json.NewEncoder(file).Encode(fixtures); err != nil {
		_ = file.Close()
		return fmt.Errorf("write fixture checkpoint: %w", err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return fmt.Errorf("sync fixture checkpoint: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close fixture checkpoint: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace fixture checkpoint: %w", err)
	}
	if dir, err := os.Open(directory); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}

func runScale(gameCount, requestCount, chatMessageCount int, sessionsPath, extendFrom, output, dataset string) error {
	if gameCount < 1 || gameCount > 1000 || requestCount < 1 || requestCount > 5000 || chatMessageCount < 0 || chatMessageCount > 4 {
		return errors.New("large-v1 targets must be Games 1..1000, Game Requests 1..5000, and messages per request 0..4")
	}
	if sessionsPath == "" || output == "" || dataset != "large-v1" {
		return errors.New("-sessions, -output, and -dataset large-v1 are required")
	}
	if extendFrom == "" {
		extendFrom = output
	}
	cfg, err := target.DatabaseConfig()
	if err != nil {
		return err
	}
	users, err := readUserIDs(sessionsPath)
	if err != nil {
		return err
	}
	if len(users) < 9 {
		return errors.New("need at least 9 seeded users")
	}
	info, err := os.Stat(filepath.Clean(extendFrom))
	if err != nil {
		return fmt.Errorf("open existing fixture manifest: %w", err)
	}
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("fixture manifest must not be accessible by group or other users")
	}
	data, err := os.ReadFile(filepath.Clean(extendFrom))
	if err != nil {
		return fmt.Errorf("read existing fixture manifest: %w", err)
	}
	var fixtures fixtureFile
	if err := json.Unmarshal(data, &fixtures); err != nil {
		return fmt.Errorf("decode existing fixture manifest: %w", err)
	}
	if len(fixtures.Games) == 0 || countRequests(fixtures) == 0 {
		return errors.New("run the initial fixture task before large-v1 mode")
	}
	if fixtures.Dataset != "" && fixtures.Dataset != dataset {
		return errors.New("fixture manifest belongs to another dataset")
	}
	if gameCount < len(fixtures.Games) || requestCount < countRequests(fixtures) {
		return errors.New("large-v1 targets cannot reduce existing fixtures")
	}
	if fixtures.Requests == nil {
		fixtures.Requests = make(map[string][]string)
	}
	if fixtures.Scale == nil {
		if chatMessageCount > 0 && len(fixtures.Requests["open"]) == 0 {
			return errors.New("chat fixtures need an existing open Game Request")
		}
		fixtures.Dataset = dataset
		fixtures.Scale = &scaleProgress{
			TargetGames: gameCount, TargetRequests: requestCount, TargetChatMessages: chatMessageCount,
			BaseRequests: countRequests(fixtures), GamesFollowed: len(fixtures.Games), RequestsComplete: countRequests(fixtures),
		}
		if chatMessageCount > 0 {
			fixtures.Scale.ChatRequestID = fixtures.Requests["open"][0]
		}
	} else if fixtures.Dataset != dataset || fixtures.Scale.TargetGames != gameCount || fixtures.Scale.TargetRequests != requestCount || fixtures.Scale.TargetChatMessages != chatMessageCount {
		return errors.New("resume with the same dataset, game, request, and messages-per-request targets")
	}
	extraRequests := requestCount - fixtures.Scale.BaseRequests
	if fixtures.Scale.BaseRequests < 1 || extraRequests < 0 || fixtures.Scale.GamesFollowed < 0 || fixtures.Scale.GamesFollowed > len(fixtures.Games) || fixtures.Scale.RequestsComplete < fixtures.Scale.BaseRequests || fixtures.Scale.RequestsComplete > requestCount || fixtures.Scale.ChatMessagesComplete < 0 || fixtures.Scale.ChatMessagesComplete > extraRequests*chatMessageCount || fixtures.Scale.CyclesComplete < 0 || fixtures.Scale.CyclesComplete > extraRequests {
		return errors.New("fixture checkpoint progress is invalid")
	}
	if filepath.Clean(output) != filepath.Clean(extendFrom) {
		if _, err := os.Stat(filepath.Clean(output)); err == nil {
			return errors.New("output fixture manifest already exists")
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("inspect output fixture manifest: %w", err)
		}
	}
	if err := writeFixtureFile(output, fixtures); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect to load-test database: %w", err)
	}
	defer pool.Close()

	gameStore := gamepostgres.NewStore(pool)
	catalog := game.NewService(gameStore)
	follows := game.NewSubscriptionService(gameStore)
	requestStore := gamerequestpostgres.NewStore(pool).WithChatRate(gamerequest.ChatRate{Limit: chatMessageCount + 100, Window: time.Hour})
	requests := gamerequest.NewService(requestStore, catalog, nil)
	queries := gamerequestquery.NewStore(pool)
	persist := func() error { return writeFixtureFile(output, fixtures) }

	for len(fixtures.Games) < gameCount {
		index := len(fixtures.Games) + 1
		name, slug := scaledGameIdentity(index)
		matches, err := catalog.List(ctx, name)
		if err != nil {
			return fmt.Errorf("find scaled Game %d: %w", index, err)
		}
		var id string
		for _, match := range matches {
			if match.Name != name && match.Slug != slug {
				continue
			}
			if match.Name != name || match.Slug != slug || id != "" {
				return fmt.Errorf("conflicting large-v1 Game marker %d", index)
			}
			id = match.ID
		}
		if id == "" {
			created, err := catalog.Create(ctx, name, slug)
			if err != nil {
				return fmt.Errorf("create scaled Game %d: %w", index, err)
			}
			id = created.ID
		}
		fixtures.Games = append(fixtures.Games, id)
		if err := persist(); err != nil {
			return err
		}
	}
	for fixtures.Scale.GamesFollowed < len(fixtures.Games) {
		index := fixtures.Scale.GamesFollowed
		follower := users[index%len(users)]
		followers, err := follows.ListFollowers(ctx, fixtures.Games[index])
		if err != nil {
			return fmt.Errorf("list followers for scaled Game %d: %w", index+1, err)
		}
		if !slices.Contains(followers, follower) {
			if err := follows.Follow(ctx, follower, fixtures.Games[index]); err != nil {
				return fmt.Errorf("follow scaled Game %d: %w", index+1, err)
			}
		}
		fixtures.Scale.GamesFollowed++
		if err := persist(); err != nil {
			return err
		}
	}

	plans := planLargeRequests(users, fixtures.Games, requestCount)
	for index := fixtures.Scale.RequestsComplete; index < requestCount; index++ {
		plan := plans[index]
		description := largeRequestDescription(index + 1)
		matches, err := queries.ListActive(ctx, users[0], gamerequestquery.Filter{Search: description})
		if err != nil {
			return fmt.Errorf("find resumable large-v1 Game Request %d: %w", index+1, err)
		}
		var request gamerequestquery.Request
		for _, match := range matches {
			if match.Description != description {
				continue
			}
			if request.ID != "" {
				return fmt.Errorf("multiple Game Requests match large-v1 marker %d", index+1)
			}
			request = match
		}
		if request.ID != "" {
			if request.CreatorID != plan.Creator || request.GameID != fixtures.Games[plan.GameIndex] || request.MaxPlayers == nil || *request.MaxPlayers != plan.MaxPlayers {
				return fmt.Errorf("Game Request marker %d has conflicting parameters", index+1)
			}
		} else {
			maxPlayers := plan.MaxPlayers
			created, err := requests.Create(ctx, plan.Creator, fixtures.Games[plan.GameIndex], &maxPlayers, time.Now().UTC().Add(time.Duration(30+index%400*7)*time.Minute), description)
			if err != nil {
				return fmt.Errorf("create large-v1 Game Request %d: %w", index+1, err)
			}
			request = gamerequestquery.Request{ID: created.ID, GameID: fixtures.Games[plan.GameIndex], CreatorID: plan.Creator, MaxPlayers: &maxPlayers, Description: description}
		}
		if !hasFixtureRequest(fixtures, request.ID) {
			fixtures.Requests[plan.Kind] = append(fixtures.Requests[plan.Kind], request.ID)
			if err := persist(); err != nil {
				return err
			}
		}
		participants, err := queries.ListParticipants(ctx, request.ID)
		if err != nil {
			return fmt.Errorf("list members for large-v1 Game Request %d: %w", index+1, err)
		}
		memberSet := make(map[string]bool, len(participants))
		for _, userID := range participants {
			memberSet[userID] = true
		}
		for _, userID := range plan.Joiners {
			if memberSet[userID] {
				continue
			}
			if err := requests.Join(ctx, request.ID, userID); err != nil {
				return fmt.Errorf("join large-v1 Game Request %d: %w", index+1, err)
			}
			memberSet[userID] = true
		}
		if err := ensureMembershipCycle(ctx, requests, queries, request.ID, plan, users, memberSet, persist); err != nil {
			return fmt.Errorf("membership cycle for large-v1 Game Request %d: %w", index+1, err)
		}
		if err := ensureRequestMessages(ctx, pool, requests, request.ID, plan.Creator, index+1, chatMessageCount, func(message int) error {
			fixtures.Scale.ChatMessagesComplete = (index-fixtures.Scale.BaseRequests)*chatMessageCount + message
			return persist()
		}); err != nil {
			return fmt.Errorf("messages for large-v1 Game Request %d: %w", index+1, err)
		}
		if plan.Kind == "playing" {
			current, err := queries.ByID(ctx, request.ID, plan.Creator)
			if err != nil {
				return fmt.Errorf("read large-v1 Game Request %d: %w", index+1, err)
			}
			if current.Status != gamerequest.StatusPlaying {
				if err := requests.Start(ctx, request.ID, plan.Creator); err != nil {
					return fmt.Errorf("start large-v1 Game Request %d: %w", index+1, err)
				}
				if err := persist(); err != nil {
					return err
				}
			}
		}
		fixtures.Scale.ChatMessagesComplete = (index + 1 - fixtures.Scale.BaseRequests) * chatMessageCount
		fixtures.Scale.CyclesComplete = index + 1 - fixtures.Scale.BaseRequests
		fixtures.Scale.RequestsComplete = index + 1
		if err := persist(); err != nil {
			return err
		}
	}
	fmt.Printf("large-v1 fixtures: games=%d requests=%d chat_messages=%d; checkpoint=%s\n", len(fixtures.Games), countRequests(fixtures), fixtures.Scale.ChatMessagesComplete, output)
	return nil
}

func ensureMembershipCycle(ctx context.Context, requests *gamerequest.Service, queries *gamerequestquery.Store, requestID string, plan plannedRequest, users []string, members map[string]bool, persist func() error) error {
	expected := map[string]bool{plan.Creator: true}
	for _, userID := range plan.Joiners {
		expected[userID] = true
	}
	var joiner string
	for _, userID := range users {
		if !expected[userID] {
			joiner = userID
			break
		}
	}
	if joiner == "" {
		return errors.New("no distinct temporary member available")
	}
	lobby, err := queries.Lobby(ctx, requestID, plan.Creator)
	if err != nil {
		return err
	}
	joined, left := 0, 0
	for _, entry := range lobby.Entries {
		if entry.Source != gamerequestquery.LobbySourceActivity || entry.Activity.ActorID != joiner {
			continue
		}
		switch entry.Activity.Kind {
		case gamerequest.ActivityJoined:
			joined++
		case gamerequest.ActivityLeft:
			left++
		}
	}
	if joined == 0 && left == 0 {
		if members[joiner] {
			return errors.New("temporary member unexpectedly belongs to request")
		}
		if err := requests.Join(ctx, requestID, joiner); err != nil {
			return err
		}
		if err := persist(); err != nil {
			return err
		}
		joined = 1
	}
	if joined == 1 && left == 0 {
		if err := requests.Leave(ctx, requestID, joiner); err != nil {
			return err
		}
		if err := persist(); err != nil {
			return err
		}
		left = 1
	}
	if joined != 1 || left != 1 {
		return fmt.Errorf("unexpected temporary membership activity counts: joined=%d left=%d", joined, left)
	}
	return nil
}

func ensureRequestMessages(ctx context.Context, pool *pgxpool.Pool, requests *gamerequest.Service, requestID, authorID string, requestIndex, count int, checkpoint func(int) error) error {
	for message := 1; message <= count; message++ {
		body := fmt.Sprintf("loadtest large-v1 request %06d message %d", requestIndex, message)
		var matches int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM game_request_messages WHERE game_request_id = $1::uuid AND author_id = $2::uuid AND body = $3`, requestID, authorID, body).Scan(&matches); err != nil {
			return err
		}
		if matches > 1 {
			return fmt.Errorf("duplicate message marker %d/%d", requestIndex, message)
		}
		if matches == 0 {
			if _, err := requests.PostMessage(ctx, requestID, authorID, body); err != nil {
				return err
			}
		}
		if err := checkpoint(message); err != nil {
			return err
		}
	}
	return nil
}

func hasFixtureRequest(fixtures fixtureFile, id string) bool {
	for _, ids := range fixtures.Requests {
		if slices.Contains(ids, id) {
			return true
		}
	}
	return false
}
