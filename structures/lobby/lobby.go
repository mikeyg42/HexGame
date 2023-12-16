package lobby

import (
	"context"
	"encoding/json"
	"time"

	hex "github.com/mikeyg42/HexGame/structures"
)

type Lobby struct { // will implement the LobbyController interface
	Players            []hex.Player
	MatchmakingService chan []byte            // the lobby receives on this channel from the matchmaking service
	PlayerChans        map[string]chan []byte // the lobby broadcasts on these channels to all players
	GameChans          map[string]chan []byte // the lobby received on these channels fro all games?
}

func NewLobby() *Lobby {
	lobby := Lobby{
		Players:            make([]hex.Player, 0),
		MatchmakingService: make(chan []byte),
		PlayerChans:        make(map[string]chan []byte),
	}

	return &lobby

}

func (l *Lobby) MatchmakingLoop(ctx context.Context, playerIDs [2]string) {
    for {
        select {
        case <-ctx.Done():
            return
        case match := <-l.MatchmakingService:
            playerIDs, err := l.PublishPairing(match)
            if err != nil {
                // log.Printf("Error publishing pairing: %v", err)
                continue
            }

            if l.awaitAcknowledgments(ctx, playerIDs) {
                l.LockPairIntoMatch(playerIDs)
            } else {
                //log.Println("Pairing failed, players did not acknowledge in time.")
            }
        }
    }
}


func (l *Lobby) awaitAcknowledgments(ctx context.Context, playerUsernames [2]string) bool {
    acks := make(map[string]bool)
    timeout := time.After(5 * time.Second)

    for len(acks) < 2 {
        select {
        case <-ctx.Done():
            return false
        case ack := <-l.PlayerChans[playerUsernames[0]]:
            if isValidAck(ack,playerUsernames[0]) {
                acks[playerUsernames[0]]= true
            }
        case ack := <-l.PlayerChans[playerUsernames[1]]:
            if isValidAck(ack, playerUsernames[1]) {
                acks[playerUsernames[1]] = true
            }
        case <-timeout:
            return false
        }
    }

    return true
}


func isValidAck(ack []byte, expectedPlayerUsername string) bool {
    var ackData struct {
        Username string `json:"username"`
    }

    if err := json.Unmarshal(ack, &ackData); err != nil {
        // The message is not in the correct JSON format
        return false
    }

    // Check if the message is from the expected player
    if ackData.Username != expectedPlayerUsername {
        return false
    }

    return true
}

func (l *Lobby) PublishPairing(match []byte) ([2]string, error) {
    var playerIDs [2]string
    err := json.Unmarshal(match, &playerIDs)
    if err != nil {
        return [2]string{}, err
    }

	announcePair := hex.LobbyEvent{
		Data:       playerIDs,
		Originator: "MatchmakingService",
		TimeStamp:  time.Now().UnixNano(),
	}

	jsonPair, err := json.Marshal(announcePair)
	if err != nil {
        return [2]string{}, err
    }

    for _, playerID := range playerIDs {
        if ch, ok := l.PlayerChans[playerID]; ok {
            ch <- jsonPair
        }
    }

    return playerIDs, nil
}
