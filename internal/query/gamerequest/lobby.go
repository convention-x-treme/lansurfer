package gamerequest

import (
	"context"
	"fmt"

	domain "github.com/convention-x-treme/lansurfer/internal/gamerequest"
	"github.com/convention-x-treme/lansurfer/internal/query/gamerequest/sqlcgen"
)

// LobbySource identifies the source of one Lobby entry.
type LobbySource int32

const (
	// LobbySourceActivity marks durable Game Request history.
	LobbySourceActivity LobbySource = 0
	// LobbySourceChat marks a participant chat message.
	LobbySourceChat LobbySource = 1
)

// LobbyEntry contains one activity row or chat message.
type LobbyEntry struct {
	Source   LobbySource
	Activity Activity
	Message  domain.Message
}

// GameRequestLobby is the participant-authorized combined history.
type GameRequestLobby struct {
	RequestID string
	Entries   []LobbyEntry
}

// Lobby returns up to 200 combined entries for a current member.
func (s *Store) Lobby(ctx context.Context, requestID, viewerID string) (GameRequestLobby, error) {
	databaseRequestID, err := parseUUID(requestID)
	if err != nil {
		return GameRequestLobby{}, domain.ErrNotFound
	}
	databaseViewerID, err := parseUUID(viewerID)
	if err != nil {
		return GameRequestLobby{}, domain.ErrNotFound
	}
	rows, err := s.queries.ReadGameRequestLobby(ctx, sqlcgen.ReadGameRequestLobbyParams{
		GameRequestID: databaseRequestID, ViewerID: databaseViewerID,
	})
	if err != nil {
		return GameRequestLobby{}, fmt.Errorf("read game request lobby: %w", err)
	}
	if len(rows) == 0 {
		return GameRequestLobby{}, domain.ErrNotFound
	}
	if !rows[0].IsMember {
		return GameRequestLobby{}, domain.ErrNotMember
	}

	lobby := GameRequestLobby{RequestID: rows[0].RequestID, Entries: make([]LobbyEntry, 0, len(rows))}
	for _, row := range rows {
		if row.Source < 0 {
			continue
		}
		entry := LobbyEntry{Source: LobbySource(row.Source)}
		switch entry.Source {
		case LobbySourceActivity:
			entry.Activity = Activity{
				Kind: domain.ActivityKind(textValue(row.Kind)), ActorID: textValue(row.ActorID),
				ActorGameNick: textValue(row.ActorGameNick), OccurredAt: row.OccurredAt.Time,
				PreviousMaxPlayers: int4Pointer(row.PreviousMaxPlayers), NewMaxPlayers: int4Pointer(row.NewMaxPlayers),
				PreviousStartsAt: timestamptzPointer(row.PreviousStartsAt), NewStartsAt: timestamptzPointer(row.NewStartsAt),
				DescriptionChanged: row.DescriptionChanged.Bool,
			}
		case LobbySourceChat:
			if _, err := parseUUID(row.MessageID); err != nil {
				return GameRequestLobby{}, domain.ErrNotFound
			}
			entry.Message = domain.Message{
				ID: row.MessageID, RequestID: row.RequestID, AuthorID: textValue(row.ActorID),
				AuthorGameNick: textValue(row.ActorGameNick), Body: textValue(row.Body), CreatedAt: row.OccurredAt.Time,
			}
		default:
			return GameRequestLobby{}, fmt.Errorf("read game request lobby: unexpected source %d", row.Source)
		}
		lobby.Entries = append(lobby.Entries, entry)
	}
	return lobby, nil
}
