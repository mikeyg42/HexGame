package models

import (
	"time"
	"encodiong
	json"
	"github.com/google/uuid"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
)

const Delimiter = "#"
const SideLenGameboard = 15

type EvtData struct {
	EventData      string    `json:"event_data"`
	EventType      string    `json:"event_type"`
	Topic          Topic     `json:"-"`
	GameID         uuid.UUID `json:"game_id"`
	OriginatorUUID uuid.UUID `json:"original_sender"`
	TimeStamp      time.Time `json:"timestamp"`
	EventIDNUM     int       `json:"event_id"`
}

type Topic struct {
	TopicName string
}

type SliceOfSubscribeChans []chan []byte // an array of all the channels that subscribe to a given topic,
// when a message is published to a particular topic, its broadcasted to all the subscribers in this slice

// -,-,-,-,-,- Lobby stuff -`-`-`-`-`- \\
type PlayerInfo struct {
    PlayerID int    `json:"player_id"`
    Username string `json:"username"`
    Rank     int    `json:"rank"`
}

type LobbyEvent struct {
    PlayerInfo PlayerInfo `json:"player_info"` // Corrected field definition
    Timestamp  uint16     `json:"timestamp"`   
    Originator string     `json:"originator"`
    Recipients string     `json:"recipients"`
}


// SendEvent allows any part of the application to send an event to the central event stream.
func SendEvent_playerEnterLobby(playerInfo PlayerInf) {
    event := LobbyEvent{
		PlayerInfo:  playerInfo
		Timestamp: time.Now().UnixNano,
		Originator: "NewPlayer", // this needs to be a unique string so that the message can be easily scanned and properly directed
		Recipients: "Lobby",
	}
	
	err, msg = json.Marshal(event) 
   
	if err != nil {
    eventStream <- msg // Send the event to the channel
	}
}

// -,-,-,-,-,- CAST OF CHARACTERS IN EACH GAME -`-`-`-`-`- \\

type Player struct {
	PlayerID     string      // other info like the usernae and rank and stuff I will just store in a map that lives in LOBBY
	LobbyChannel chan []byte // the player receives on this channel from the lobby
	EventChannel chan []byte // the player broadcasts on this channel to the lobby
}

type TimerController interface {
	StartTimer()
	StopTimer()
	PublishToTimerTopic()
}

type LobbyController interface {
	PublishPairing(match []byte) ([2]string, error)    // match is a slice of bytes containiing game ID. the array of strings has 2 players
	LockPairIntoMatch() //partly done
	CleanupAfterMatch() // need to write
	MatchmakingLoop()   //done
}

type Referee interface {
	EvaluateProposedMoveLegality()
	EvaluateWinCondition()
	BroadcastGameEnd()
	BroadcastConnectionFail()   
	DemandPlayersAck()
}

type CacheManager interface {
	WriteToCache()
	FetchFromCache()
}

type MemoryInterface interface { // done
	AddMove_persist()
	CompleteGame_persist() // double check you did not miss any moves and then update the game status
	NewGame_persist()
	FetchSomeMoveList(pool *pgxpool.Pool, gameID uuid.UUID) ([]Move, error) // can be used to fetch the entire game history of BOTH or JUST 1 player
	DeleteGame_persist()
}

// there will be two of these, of course
type PlayerController interface {
	PostMove()
	RequestGamestate()
	Ack()
	AnnounceConnect() // this needs to happen when players are able to start game. and also after a disconnection
}

// database structs
type Vertex struct {
	X int
	Y int
}

type PooledConnections struct {
	PoolConfig       *pgxpool.Config
	MaxReadPoolSize  int
	MaxWritePoolSize int
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	ReadPool         *pgxpool.Pool
	WritePool        *pgxpool.Pool
}

type Game struct {
	GameID        uuid.UUID `db:"game_id"`
	PlayerAID     uuid.UUID `db:"playerA_id"`
	PlayerBID     uuid.UUID `db:"playerB_id"`
	Outcome       string    `db:"outcome"`
	GameStartTime time.Time`db:"game_start_time"`
}

type Move struct {
	MoveID          int       `db:"move_id"`
	GameID          uuid.UUID `db:"game_id"`
	PlayerID        uuid.UUID `db:"player_id"`
	PlayerCode      string    `db:"player_code"` // refers to player A or player B
	PlayerGameCode  string    `db:"player_game_code"`
	MoveDescription string    `db:"move_description"`
	MoveTime        time.Time `db:"move_time"`
	MoveCounter     int       `db:"move_counter"` // does not refer to turn #, is count of tiles played in game(possibly-1)
}


// CACHE

// CacheKey represents the key for each cache entry.
type CacheKey struct {
	GameID      int
	MoveCounter int // note, this is not turn #, but the move # for the game (ie corresponds to tiles played by either player)
}

// CacheValue represents a single entry in our cache.
type CacheValue struct {
	GameState  []Vertex // allMovesList
	Expiration int64 // UTC time in unix nano format
}