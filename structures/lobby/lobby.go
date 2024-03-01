package lobby

import (
	"context"
	"encoding/json"
	"time"
	"errors"
	hex "github.com/mikeyg42/HexGame/structures"
)

type Lobby struct { // will implement the LobbyController interface
	Players            []hex.Player
	MM_chan_submit 		chan []byte            // the lobby sends on this channel to the matchmaking service
	MM_chan_receive 	chan []byte            // the lobby receives on this channel from the matchmaking service
	PlayerChans        map[string]chan []byte // the lobby broadcasts on these channels to all players
	GameChans          map[string]chan []byte // the lobby received on these channels fro all games?
	EventStream_players chan hex.PlayerLobbyEvent // where the lobby received word that a new player has entered the lobby
}

func NewLobby() *Lobby {
	lobby := Lobby{
		Players:            make([]hex.Player, 0),

		MM_chan_recieve: make(chan []byte), 
		MM_chan_submit: make(chan []byte), 

		PlayerChans:        make(map[string]chan []byte),
		// the lobby sends to the players on these channels. the key is the player's username
		
		EventStream_players: make(chan []byte),
		// events establishing the current state of the lobby queue

		GameChans: 			make(map[string]chan []byte),
		// theres a gameChan to each ref
	}

	return &lobby

}

// ParseMatchmakersMsg parses the JSON message from the matchmaking service and returns the player IDs to be paired for a game.
ıZZZE´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´´B◊√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√√BNVGC CZXVDN ZCXDVZXZ CVXı¸¸¸¸¸¸¸¸¸¸¸¸¸¸¸¸¸¸¸¸¸¸¸ ZXBVD ◊ÇÍ˛˛˛˛˛˛˛˛˛˛˛˛˛˛˛˛˛˛˛˛˛˛˛Z≈ÇÍ√√√√√√◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊◊√√√√Ízz∫ v
func (l *Lobby) ListenForMMResponse(ctx context.Context, playerIDs [2]string) {
	// listen for the matchmaking service to send a message to the lobby indicating what two players will be paired to duel
	for {
		select {
		case <-ctx.Done():
			return

		case matchupMsg, ok:= <-l.MM_Chan_reciev:
			if !ok {
				return 
			}

			playerIDs, err := parseMatchmakingMsg(matchMsg)
			if err != nil {
				// log.Printf("Error parsing matchmaking message: %v", err)
				continue
			}

			// decodes the message from the MMservice into a 2 element array of strings, ie the two players to play

			err := l.PublishPairing(playerIDs)
			if err != nil {
				// log.Printf("Error publishing pairing: %v", err)
				continue
			}

			if l.awaitAcknowledgments(ctx, playerIDs) {
				l.LockPairIntoMatch(playerIDs)
			} else {
				//log.Println("Pairing failed, players did not acknowledge in time....releasing!")
				l.ReleasePairBackLobby(playerIDs)
			}
		}
	}
}

// after a game or if a matchup failed, we try to reintroduce the players to the lobby to be repaired!
func (l *Lobby) ReleasePairBackLobby (playerIDs [2]string) {
	&hex.LobbyEvent{
		

	}



}


// listens for new players entering the lobby, upon which their names get sent off the MMservice
func (l *Lobby) ListenNewPlayers_sendToMM(ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done(): // Listen for context done
				// Perform any cleanup if necessary
				return // Exit the goroutine
			case msg, ok := <-l.NewPlayer:
				if !ok {
					// Channel is closed, exit the goroutine
					return
				}


				playerAnnounce, found, err := containsKeyword(msg, "NewPlayer")
				if err != nil || !found || "" == hex.LobbyEvent.Originator {
					// log.Printf("Error checking for keyword in event: %v", err)
					continue
				}

				newMsg := hex.LobbyEvent{
					Timestamp:   playerAnnounce.Timestamp, // message to matchmaking service keeps the timestamp of the original message
					Originator:  "Lobby",
					Recipients:  "MM_Chan_submit",
					PlayerInfo:  playerAnnounce.PlayerInfo,
				}

				jsonMsg, err := json.Marshal(newMsg)
				if err != nil {
					panic(err)
		
			}
		}
	}()

}




func (l *Lobby) awaitAcknowledgments(ctx context.Context, playerUsernames [2]string) bool {
	acks := make(map[string]bool)
	timeout := time.After(5 * time.Second)

	for len(acks) < 2 {
		select {
		case <-ctx.Done():
			return false
		case ack := <-l.PlayerChans[playerUsernames[0]]:
			if isValidAck(ack, playerUsernames[0]) {
				acks[playerUsernames[0]] = true
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

func (l *Lobby) PublishPairing(playerIDs [2]string) (error) {
	

	announcePair := hex.PlayerLobbyEvent{
		Username:       playerID[0],
		Originator: "MM_chan",
		TimeStamp:  time.Now().UnixNano(),
		Recipients: "Lobby",
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
