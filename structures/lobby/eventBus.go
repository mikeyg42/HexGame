package lobby

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	hex "github.com/mikeyg42/HexGame/structures"
)

type GameEventBus struct {
	GameID                  string
	AllSubscriberChannels   map[string]chan []byte
	Rwm                     sync.RWMutex
	SubscribersForEachTopic map[hex.Topic]hex.SliceOfSubscribeChans
	EventChan               chan hex.EvtData // this is the channel that all events are sent to. one per game
	Context                 context.Context
	CancelFn                context.CancelFunc
}

type GameEventBusManager struct {
	MapGameBuses map[string]*GameEventBus
	Mu           sync.RWMutex
}

func main() {
	manager := NewGameEventBusManager()

	lobby := manager.CreateNewGameLobby()

	ctx, cancelF := context.WithCancel(context.Background())

	// lobby starts listening for matchmaking events and broadcasting them to the players
	go lobby.ListenForMatchmaking(ctx)

	// to create a new GameEventBus instances

	game := manager.createAndRegisterNewGameBus(ctx)


	// Example usage: Subscribe a new client to a topic
	clientChan := make(chan []byte)
	game.Subscribe("TimerTopic", clientChan)

	// Example usage: Publish a message to a topic
	message := hex.EvtData{
		Topic: hex.Topic{TopicName: "TimerTopic"},
		Data:  "Some event data",
	}
	game.Publish(message)






	// wait for the signal...
	game.Shutdown(cancelF)
.
	// TBD

}

// manager code
func NewGameEventBusManager() *GameEventBusManager {
	return &GameEventBusManager{
		MapGameBuses: make(map[string]*GameEventBus),
		Mu:           sync.RWMutex{},
	}
}

func (manager *GameEventBusManager) createAndRegisterNewGameBus(ctx context.Context) *GameEventBus {

	newGameID := generateUniqueGameID()
	ctx, cancelFn := context.WithCancel(ctx)
	gameBus := &GameEventBus{
		GameID:                  newGameID,
		AllSubscriberChannels:   createAllGameChannels(),
		SubscribersForEachTopic: make(map[hex.Topic]hex.SliceOfSubscribeChans),
		EventChan:               make(chan hex.EvtData),
		Context:                 ctx,
		CancelFn:                cancelFn,
	}

	gameBus.DefineTopicSubscriptions(CreateTopics())

	manager.Mu.Lock()
	// add new gameBus to the map of all gameBuses
	manager.MapGameBuses[newGameID] = gameBus
	manager.Mu.Unlock()

	go gameBus.marshalAndForward(gameBus.EventChan)

	return gameBus
}

func CreateTopics() []hex.Topic {
	return []hex.Topic{
		hex.Topic{TopicName: "TestTopic"},
		hex.Topic{TopicName: "TimerTopic"},
		hex.Topic{TopicName: "GameLogicTopic"},
		hex.Topic{TopicName: "MetagameTopic"},
		hex.Topic{TopicName: "ResultsTopic"},
	}
}

func (manager *GameEventBusManager) CreateNewGameLobby() hex.Lobby {
	// create all the channels to act as a sort of organizing hub
	// create the array or slice that will hold all the players in the lobby

	return hex.Lobby{
		Players: make([]hex.Player, 0),
		//blah blah
	}
}

func (manager *GameEventBusManager) FetchGameBus(gameID string) (*GameEventBus, bool) {
	manager.Mu.RLock()
	defer manager.Mu.RUnlock()

	gameBus, found := manager.MapGameBuses[gameID]
	return gameBus, found
}

func generateUniqueGameID() string {
	//generate uuid of format e.g. 53aa35c8-e659-44b2-882f-f6056e443c99

	// uuid.Must returns the uuis if err== nil, otherwise it panics
	gameIDuuid := uuid.Must(uuid.NewRandom()) // returns a v4 uuid -- generates a random, unique string that is 16 characters long
	gameID := gameIDuuid.String()

	if gameID == "" {
		// if gameID is empty string then input to String() was invalid
		panic(fmt.Errorf("gameID is invalid uuid"))
	}

	return gameID
}

func createAllGameChannels() map[string]chan []byte {

	var folksAttendingGame = []string{"playerA", "playerB", "referee", "timer", "lobby", "memory"}
	allGameChannels := make(map[string]chan []byte) //this is the sole output, a map with keys of player IDs and values of their respective channels

	for _, person := range folksAttendingGame {
		allGameChannels[person] = make(chan []byte)
	}

	return allGameChannels
}

func (manager *GameEventBusManager) CreateRefereePool(maxGoroutines int) *RefereePool {
	p := &RefereePool{
		RefTasks: make(chan RefTask),
	}
	for i := 0; i < maxGoroutines; i++ {
		go p.RefRoutine()
	}
	return p
}



func (geb *GameEventBus) Shutdown(cancel context.CancelFunc) {
	defer cancel()
	defer geb.CancelFn() // Signal to all goroutines to stop

	// close each channel
	geb.Rwm.Lock()
	defer geb.Rwm.Unlock()
	for _, ch := range geb.AllSubscriberChannels {
		close(ch) // Close each subscriber channel
	}
	close(geb.EventChan) // Close the event channel
	// etc...
}

func (geb *GameEventBus) marshalAndForward(inChan chan hex.EvtData) {
	for {
		select {
		case evt, ok := <-inChan:
			if !ok {
				return // channel closed
			}

				jsonData, err = json.Marshal(evt)
				if err != nil {
					// log.Printf("Error marshaling event data: %v", err)
					continue // Skip this iteration and
			}

			geb.Rwm.RLock()
			subscribers := geb.SubscribersForEachTopic[evt.Topic]
			geb.Rwm.RUnlock()

			for _, ch := range subscribers {
				select {
				case ch <- jsonData:
				default:
					// log.Printf("Subscriber too slow, skipping message dispatch")
					// Optionally: handle slow subscribers more gracefully
				}
			}

		case <-geb.Context.Done():
			return
		}
	}
}

func (geb *GameEventBus) DefineTopicSubscriptions(topics []hex.Topic) {
	geb.Rwm.Lock()
	defer geb.Rwm.Unlock()

	// iterates through each of the topics and appends to the slice of subscribers for that topic those players
	for _, topic := range topics {
		subscriberSlice := hex.SliceOfSubscribeChans{}
		switch topic.TopicName {
		case "TimerTopic":
			// the timer will be the sole publisher to this topic, and the players, and ref will listen
			subscriberSlice = append(subscriberSlice, geb.AllSubscriberChannels["playerA"], geb.AllSubscriberChannels["playerB"], geb.AllSubscriberChannels["referee"])

		case "GameLogicTopic":
			// the players+ref will publish and subscribe to this topic, as will the memory subscribe
			subscriberSlice = append(subscriberSlice, geb.AllSubscriberChannels["playerA"], geb.AllSubscriberChannels["playerB"], geb.AllSubscriberChannels["referee"], geb.AllSubscriberChannels["memory"])
	
		case "MetagameTopic":
			// publishers will be the referee. timer and 2 players will listen (timer so it can stop the timer if need be, and players so front end can pause and ack reconnect)
			subscriberSlice = append(subscriberSlice, geb.AllSubscriberChannels["playerA"], geb.AllSubscriberChannels["playerB"], geb.AllSubscriberChannels["timer"])

		case "ResultsTopic":
			// referee will announce start and end of the game, memory and lobby will listen
			subscriberSlice = append(subscriberSlice, geb.AllSubscriberChannels["memory"], geb.AllSubscriberChannels["lobby"])

		case "TestingTopic":
			// no idea yet
		}

		geb.SubscribersForEachTopic[topic] = subscriberSlice
	}

	return
}

type MessageStruct struct {
	Topic string
	Msg   string
	Sender string
}

func struct2byteMsg(msg MessageStruct) []byte {
	// messages are formatted initially as MessageStructs, I convert to []bytes before sending
	return []byte(strings.Join([]string{msg.Topic, msg.Msg, msg.Sender}, hex.Delimiter))
}

func byteMsg2struct([]byte) (MessageStruct, error) {
	// check there are precisely 2 substrings. set n=-1 so that all substrings are extracted, then panic if that does not result in 2 substrings
	parts := strings.SplitN(string(rawMsg), hex.Delimiter, -1)
	
	switch len(parts) {
	case 2: 
		return MessageStruct{
			Topic: parts[0],
			Msg:   string(parts[1]),
			Sender: "",
		}, nil
	case 3: 
		return MessageStruct{
			Topic: parts[0],
			Msg:   string(parts[1]),
			Sender:string(parts[2]),
		}, nil
	default:
		return nil, fmt.Errorf("message is not formatted correctly. expected 2 or 3 substrings, not %d", len(parts))
	}
}

// This function assumes that messages are formatted as `topic#actual_message`. 
func (geb *GameEventBus) DispatchMessage(rawMsg []byte) {

	msg, err := byteMsg2struct(rawMsg)
	if err != nil {
		// log.Printf("Error parsing message: %v", err)
	}

	channels, ok := geb.getChannelsForTopic(msg.Topic)
	if !ok {
		// Handle these cases: 1. The topic doesn't exist or 2. the channel slice is empty.
		return
	}

	for _, ch := range channels {
		ch <- []byte(msg)
	}
}

// uses read/write mutex to allow concurrent reads but exclusive writes
func (geb *GameEventBus) getChannelsForTopic(topic string) ([]chan []byte, bool) {
	geb.Rwm.RLock()
	defer geb.Rwm.RUnlock()

	chs, ok := geb.SubscribersForEachTopic[hex.Topic{TopicName: topic}]
	return chs, ok
}


// type RefereePool struct {
// 	RefTasks chan RefTask
// }

// type RefTask struct {
// 	Process func([]byte) error
// 	Data    []byte
// }

// func (manager *GameEventBusManager) RunRefPool() {
// 	refPool := m.CreateRefereePool(maxNGamesConcurrently)
	
// }




// func (rp *RefereePool) RefRoutine() {
// 	for task := range rp.RefTasks {
// 		err := task.Process(task.Data) // ?????
// 		if err != nil {
// 			// Handle the error or send it to a results channel
// 		}
// 	}
// }