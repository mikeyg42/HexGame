package database

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	pgx "github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	hex "github.com/mikeyg42/HexGame/structures"
	"golang.org/x/crypto/bcrypt"

	SQL_namedPreparedStmts "github.com/mikeyg42/HexGame/database/sqlconstants"
	pgxUUID "github.com/vgarvardt/pgx-google-uuid/v5"
)

// global
type MemoryPersister struct{}

const maxReadPoolSize = 10
const maxWritePoolSize = 10
const readTimeout = 3 * time.Second
const writeTimeout = 3 * time.Second
const maxRetries = 3
const retryDelay = 200 * time.Millisecond

// ----------------------------------//
func InitializePostgres() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Define and initialize connection pools
	pool, err := definePooledConnections(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to define pooled connections: %w", err))
	}
	defer pool.WritePool.Close()
	defer pool.ReadPool.Close()

	// Test the connection pools
	if err := testConnection(ctx, pool.WritePool); err != nil {
		panic(fmt.Errorf("write pool failed connection test: %w", err))
	}
	if err := testConnection(ctx, pool.ReadPool); err != nil {
		panic(fmt.Errorf("read pool failed connection test: %w", err))
	}

	// Initialize database schema
	if err := initializeDatabaseSchema(ctx, pool.WritePool); err != nil {
		panic(fmt.Errorf("failed to initialize write database schema: %w", err))
	}
	if err := initializeDatabaseSchema(ctx, pool.ReadPool); err != nil {
		panic(fmt.Errorf("failed to initialize read database schema: %w", err))
	}

	// check if tables exist yet...
	missingTables, err := check3Tables(ctx, pool.ReadPool, []string{"users", "games", "moves"})
	if err != nil {
		panic(fmt.Errorf("failed to check for existance of tables: %w", err))
	}
	// ... and if tables do not yet exist, make them
	if len(missingTables) > 0 {
		err := createMissingTables(ctx, pool.WritePool, missingTables)
		if err != nil {
			panic(fmt.Errorf("failed to create missing tables: %w", err))
		}
	}

	// Perform comprehensive database setup check
	if ok, err := isDatabaseSetupCorrectly(ctx, pool); err != nil || !ok {
		panic(fmt.Errorf("database setup check failed: %v", err))
	}
	fmt.Println("Successfully initialized and verified the database setup")

}

func testConnection(ctx context.Context, pool *pgxpool.Pool) error {
	var dummy int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&dummy); err != nil {
		return err
	}
	return nil
}

// check that both pools are working, one at a time
func PingPooledConnections(ctx context.Context, p *hex.PooledConnections) error {
	pool := p.WritePool

	err := pool.Ping(ctx)
	if err != nil {
		err = fmt.Errorf("Unable to ping connection writepool: %v\n", err)
		time.Sleep(retryDelay)
		err2 := pool.Ping(ctx)
		if err2 != nil {
			err = fmt.Errorf("Unable to ping connection readpool after retry: %v\n", err2)
		}
	}
	return err
}

func initializeDatabaseSchema(ctx context.Context, pool *pgxpool.Pool) error {

	// Define map of statement name to SQL
	initTables := []string{
		SQL_namedPreparedStmts.CreateRoles,
		SQL_namedPreparedStmts.LoadCryptoPkg,
		SQL_namedPreparedStmts.CreateGameOutcomeEnum,
		SQL_namedPreparedStmts.CreateTableSQL_moves,
		SQL_namedPreparedStmts.CreateGameOutcomeEnum,
		SQL_namedPreparedStmts.CreateGameOutcomeEnum,
		SQL_namedPreparedStmts.Revoke,
		SQL_namedPreparedStmts.GrantAccess,
		SQL_namedPreparedStmts.GrantExecute,
	}

	// Start a new transaction
	tx, err := pool.Begin(ctx)
	if err != nil {
		pingError := pool.Ping(ctx)
		if pingError != nil {
			return fmt.Errorf("before transaction begins, stopped bc unstable connection %v, : %v\n", pool, pingError)

		} else {
			// try to reconnect and ping again

		}

		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() {
		if err != nil {
			tx.Rollback(ctx)
		}
	}()

	// Execute each SQL statement within the transaction
	for _, sql := range initTables {
		_, err := tx.Exec(ctx, sql)
		if err != nil {
			return fmt.Errorf("failed to execute SQL statement in transaction: %w", err)
		}
	}

	// Commit the transaction if all SQL statements executed successfully
	err = tx.Commit(ctx)
	if err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func definePooledConnections(ctx context.Context) (*hex.PooledConnections, error) {

	// Parse the DSN to a pgxpool.Config struct -- DSN will be regular dsn pulled from env
	// e.g. //	user=jack password=secret host=pg.example.com port=5432 dbname=mydb sslmode=verify-ca pool_max_conns=10

	databaseDSN := os.Getenv("DATABASE_DSN")
	poolConfig, err := pgxpool.ParseConfig(databaseDSN)
	if err != nil {
		panic(fmt.Errorf("Unable to parse connection string: %v\n", err))
	}

	// Configure TLS
	poolConfig.ConnConfig.TLSConfig = &tls.Config{
		ServerName:         " 0.0.0.0", //???,
		InsecureSkipVerify: false,      //???,
	}

	poolConfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		// log.Println("Connected to database!")
		// provide type support for google/uuid for your postgresql driver by registering the typemap
		pgxUUID.Register(conn.TypeMap())

		// prepare your prepared statements
		// see this re: prepared statements https://github.com/jackc/pgx/issues/791#issuecomment-660508309
		return makeAvail_NamedPreparedStatements(ctx, conn)
	}

	poolConfig.BeforeClose = func(c *pgx.Conn) {
		// this should instead be a log!
		fmt.Println("Closed the connection pool to the database!!")
	}
	readPool, err := createReadPool(ctx, poolConfig, maxReadPoolSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create read pool: %w", err)
	}
	writePool, err := createWritePool(ctx, poolConfig, maxWritePoolSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create write pool: %w", err)
	}

	return &hex.PooledConnections{
		PoolConfig:       poolConfig,
		MaxReadPoolSize:  maxReadPoolSize,
		MaxWritePoolSize: maxWritePoolSize,
		ReadTimeout:      time.Duration(readTimeout),
		WriteTimeout:     time.Duration(writeTimeout),
		ReadPool:         readPool,
		WritePool:        writePool,
	}, nil
}

func createReadPool(ctx context.Context, poolConfig *pgxpool.Config, maxReadPoolSize int) (*pgxpool.Pool, error) {
	readPool, err := poolWithMaxSize(ctx, poolConfig, maxReadPoolSize)
	if err != nil {
		return nil, err
	}
	return readPool, nil
}

func createWritePool(ctx context.Context, poolConfig *pgxpool.Config, maxWritePoolSize int) (*pgxpool.Pool, error) {
	writePool, err := poolWithMaxSize(ctx, poolConfig, maxWritePoolSize)
	if err != nil {
		return nil, err
	}
	return writePool, nil
}

func poolWithMaxSize(ctx context.Context, poolConfig *pgxpool.Config, maxConns int) (pool *pgxpool.Pool, err error) {
	if maxConns != 0 {
		poolConfig.MaxConns = int32(maxConns)
	}

	return pgxpool.NewWithConfig(ctx, poolConfig)
}

func makeAvail_NamedPreparedStatements(ctx context.Context, conn *pgx.Conn) error {
	var prepStmts map[string]string

	// Define map of statement name to SQL
	prepStmts = map[string]string{
		"FetchOngoingGames":  SQL_namedPreparedStmts.Fetch_ongoing_games,
		"FetchMovesByGameID": SQL_namedPreparedStmts.Fetch_all_moves_for_gameID,
		"FetchAPlayersMoves": SQL_namedPreparedStmts.Fetch_a_particular_players_moves,
		"Fetch3recentMoves":  SQL_namedPreparedStmts.Fetch_latest_three_moves_in_game,
		"AddNewUser":         SQL_namedPreparedStmts.Add_new_user,
		"UpdateGameOutcomes": SQL_namedPreparedStmts.Update_game_outcome,
		"AddNewGame":         SQL_namedPreparedStmts.Add_new_game,
		"AddNewMove":         SQL_namedPreparedStmts.Add_new_move,
		"DeleteGame":         SQL_namedPreparedStmts.Delete_game,
		"CreateUserTable":    SQL_namedPreparedStmts.CreateTableSQL_users,
		"CreateGameTable":    SQL_namedPreparedStmts.CreateTableSQL_games,
		"CreateMoveTable":    SQL_namedPreparedStmts.CreateTableSQL_moves,
	}

	for name, sql := range prepStmts {
		var err error

		for i := 0; i < maxRetries; i++ {
			// calling Prepare here is how we make the prepared statement available on the connection
			_, err = conn.Prepare(ctx, name, sql)
			if err == nil {
				break // Break if no error
			}

			// Log error

			//wait before retrying
			time.Sleep(retryDelay)
		}
		// If error still present after retries, handle it
		if err != nil {
			panic(fmt.Errorf("Failed to prepare statement %v after %v retries: %v", name, maxRetries, err))
		}
	}
	return nil
}

func check3Tables(ctx context.Context, pool *pgxpool.Pool, tables []string) ([]string, error) {
	missingTables := []string{}

	for _, table := range tables {
		var tablename string
		err := pool.QueryRow(ctx, "SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename = $1;", table).Scan(&tablename)
		if err != nil {
			if err == pgx.ErrNoRows {
				missingTables = append(missingTables, table)
			} else {
				return nil, fmt.Errorf("error checking table %s: %w", table, err)
			}
		}
	}

	return missingTables, nil
}

func createMissingTables(ctx context.Context, pool *pgxpool.Pool, missingTables []string) error {
	for _, table := range missingTables {
		var preparedStatementName string
		switch table {
		case "users":
			preparedStatementName = "CreateUserTable"
		case "games":
			preparedStatementName = "CreateGameTable"
		case "moves":
			preparedStatementName = "CreateMoveTable"
		default:
			continue
		}

		_, err := pool.Exec(ctx, preparedStatementName)
		if err != nil {
			return fmt.Errorf("failed to create table %s: %w", table, err)
		}
	}
	return nil
}

// isDatabaseSetupCorrectly tests if the database is set up correctly. Should be called whenenever the server starts.
//  1. Checks for the existence of the "server_admin" role. Also pings conns.
//  2. Runs a test: inserts and retrieves dummy data to ensure proper data manipulation.
//     We then delete the inserted dummy data and confirm the success of batch operations.
func isDatabaseSetupCorrectly(ctx context.Context, p *hex.PooledConnections) (bool, error) {
	// Parameters are context, and
	// - p: A pointer to the hex.PooledConnections object representing the database connections.
	//
	// Returns a boolean indicating whether the database setup is correct and an error, if any

	// Check for the existence of the server_admin role
	var rolname string
	err := p.ReadPool.QueryRow(ctx, "SELECT rolname FROM pg_roles WHERE rolname = $1;", "server_admin").Scan(&rolname)
	if err != nil {
		if err == pgx.ErrNoRows {
			return false, fmt.Errorf("role server_admin does not exist: %v", err)
		}
		return false, err
	}

	// ping connections
	errPool := PingPooledConnections(ctx, p)
	if errPool != nil {
		panic(fmt.Errorf("unable to ping connection pool: %v\n", errPool))
	}

	// 3. Insert and retrieve dummy data, then delete
	// make a dummy game entry using AddNew Game prepared statement
	userID, err := New_UUIDstring()
	if err != nil {
		return false, fmt.Errorf("failed to generate dummy userID: %w", err)
	}

	tx, err := p.WritePool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // This will rollback if tx.Commit() isn't called

	_, err = tx.Exec(ctx, "Add_new_user", userID, "password")
	if err != nil {
		return false, fmt.Errorf("failed to insert dummy data into users: %w", err)
	}

	// create a dummy user entry using AddNewUser prepared statement

	var retrievedID string
	err = p.ReadPool.QueryRow(ctx, "SELECT user_id FROM users WHERE username = 'dummyuser';").Scan(&retrievedID)
	if err != nil {
		return false, fmt.Errorf("failed to retrieve dummy data from users: %w", err)
	}

	if retrievedID != userID {
		return false, errors.New("retrieved dummy data does not match inserted data")
	}

	// Delete the entries you made to preserve the table integrity and also confirm that batch work fine
	// Start a new batch
	batch := &pgx.Batch{}

	// Queue up your commands
	batch.Queue("DELETE FROM users WHERE user_id = $1", userID)

	// Send the batch to the server
	br := p.WritePool.SendBatch(ctx, batch)

	// Close the batch operation pool after processing
	defer br.Close()

	// Check results for the first command
	if _, err := br.Exec(); err != nil {
		return false, fmt.Errorf("failed to delete user data from users: %w", err)
	}

	return true, nil
}

func twoTries(f1 func() (interface{}, error)) (interface{}, error) {
	result, err := f1()
	if err != nil {
		time.Sleep(retryDelay) // Delay before retrying
		result, err = f1()     // Capture the result and error of the second attempt
		if err != nil {
			return nil, fmt.Errorf("failed to execute function twice: %w", err)
		}
	}
	return result, nil
}

// Wraps the uuid.NewRandom() function to return a string instead of a uuid.UUID, and also to retry once if it fails
func New_UUIDstring() (string, error) {
	// this function twoTries will repeat a call to a function if and only if the first call fails, adding a tiny delay
	userID, err := twoTries(func() (interface{}, error) {
		return uuid.NewRandom()
	})

	if err != nil {
		return "", err // Return the error instead of panicking
	}

	// uuid type assertion, because userID is an interface{} and needs to be asserted to uuid.UUID
	uuidVal, ok := userID.(uuid.UUID)
	if !ok {
		return "", fmt.Errorf("failed to assert UUID type")
	}

	return uuidVal.String(), nil
}

// uses a transaction to delete from both tables all at once or not at all/
func DeleteGame(ctx context.Context, gameID uuid.UUID, pool *pgxpool.Pool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // This will rollback if tx.Commit() isn't called

	var wasDeleted bool
	err = tx.QueryRow(ctx, "SELECT delete_game($1)", gameID).Scan(&wasDeleted)
	if err != nil {
		return err
	}

	if !wasDeleted {
		return errors.New("game deletion failed: game not found")
	}

	// Commit the transaction if everything went fine
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

func InitiateNewGame(playerA, playerB uuid.UUID, writePool *pgxpool.Pool) (uuid.UUID, error) {
	var newGameID uuid.UUID
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	// Use the name of the prepared statement, provide the required parameters, and scan the result into newGameID
	err := writePool.QueryRow(ctx, "AddNewGame", playerA, playerB).Scan(&newGameID)
	if err != nil {
		fmt.Println("Failed to execute prepared statement AddNewGame: %v\n", err)
		return uuid.Nil, err
	}
	fmt.Printf("New game initiated with ID: %s\n", newGameID)
	return newGameID, nil
}

// AddMoveToMemory() adds a move to the database, using the prepared statement AddNewMove, returns the moveCounter stored in SQL
func AddMoveToMemory(ctx context.Context, move hex.Move, pool *pgxpool.Pool) (hex.Move, error) {
	// Extract the player code from the move (ie A or B)
	playerCode := move.PlayerCode

	var moveCounter int
	err := pool.QueryRow(ctx, "AddNewMove", move.GameID, playerCode, move.MoveDescription).Scan(&moveCounter)
	if err != nil {
		return move, fmt.Errorf("unable to add move to moves table: %w", err)
	}

	if !checkMoveNumber(moveCounter, move) {
		fmt.Printf("Move counter mismatch: SQL says: %v while golang says: %v\n", moveCounter, move.MoveCounter)
		if moveCounter > move.MoveCounter {

			// check SQL for nearly duplicate entries?

			move.MoveCounter = moveCounter

		} else {
			return move, fmt.Errorf("PostgreSQL did not persist everymove it should have properly bc its counter is too low \n")
		}
	}

	return move, nil
}

func checkMoveNumber(moveCounter int, move hex.Move) bool {
	if moveCounter != move.MoveCounter {
		return false
	}
	return true

}

// this is a wrapper function for the exported Fetch commands fro the postgresQL database
func FetchSomeMoveList(p *hex.PooledConnections, gameID, whatToFetch string) ([]hex.Move, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()

	switch whatToFetch {
	case "entireGame":
		return FetchLatestThreeMovesInGame(ctx, gameID, p.ReadPool)

	case "playerA":
		playerGameCode := gameID + "-A"
		return FetchParticularPlayersMoves(ctx, playerGameCode, p.ReadPool)

	case "playerB":
		playerGameCode := gameID + "-B"
		return FetchParticularPlayersMoves(ctx, playerGameCode, p.ReadPool)

	case "latestThree":
		return FetchLatestThreeMovesInGame(ctx, gameID, p.ReadPool)

	default:
		err := fmt.Errorf("invalid input to FetchSomeMoveList(): %v\n", whatToFetch)
		return nil, err
	}
}

func FetchLatestThreeMovesInGame(ctx context.Context, gameID string, readPool *pgxpool.Pool) ([]hex.Move, error) {
	
	rows, err := readPool.Query(ctx, "Fetch3recentMoves", gameID)
	if err != nil {
		time.Sleep(retryDelay)
		rows, err = readPool.Query(ctx, "Fetch3recentMoves", gameID)
		if err != nil {
			panic(fmt.Errorf("Unable to fetch latest three moves in game: %v\n", err))
		}
	}
	defer rows.Close()

	var latestMoves []hex.Move
	for rows.Next() {
		var move hex.Move
		err := rows.Scan(&move.PlayerGameCode, &move.MoveCounter, &move.MoveDescription, &move.MoveTime)
		if err != nil {
			return nil, err
		}
		latestMoves = append(latestMoves, move)
	}

	return latestMoves, nil
}

func FetchParticularPlayersMoves(ctx context.Context, playerGameCode string, readPool *pgxpool.Pool) ([]hex.Move, error) {

	rows, err := readPool.Query(ctx, "FetchAPlayersMoves", playerGameCode)
	if err != nil {
		time.Sleep(retryDelay)
		rows, err = readPool.Query(ctx, "FetchAPlayersMoves", playerGameCode)
		if err != nil {
			panic(fmt.Errorf("Unable to fetch particular player's moves: %v\n", err))
		}
	}
	defer rows.Close()

	var moves []hex.Move
	for rows.Next() {
		var move hex.Move
		err := rows.Scan(&move.MoveID, &move.GameID, &move.PlayerID, &move.PlayerCode, &move.PlayerGameCode, &move.MoveDescription, &move.MoveTime)
		if err != nil {
			return nil, err
		}
		moves = append(moves, move)
	}

	return moves, nil
}

func FetchAllMovesForGameID(ctx context.Context, gameID uuid.UUID, readPool *pgxpool.Pool) ([]hex.Move, error) {

	rows, err := readPool.Query(ctx, "fetch_all_moves_for_game", gameID)
	if err != nil {
		time.Sleep(retryDelay)
		rows, err = readPool.Query(ctx, "fetch_all_moves_for_game", gameID)
		if err != nil {
			panic(fmt.Errorf("Unable to fetch all moves for gameID: %v\n", err))
		}
	}
	defer rows.Close()

	var moves []hex.Move
	for rows.Next() {
		var move hex.Move // Move is a struct representing a move, define it accordingly.
		err := rows.Scan(&move.MoveID, &move.GameID, &move.PlayerID, &move.PlayerCode, &move.PlayerGameCode, &move.MoveDescription, &move.MoveTime)
		if err != nil {
			return nil, err
		}
		moves = append(moves, move)
	}

	return moves, nil
}

func FetchOngoingGames(gameID, playerID *uuid.UUID, readPool *pgxpool.Pool) ([]hex.Game, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rows, err := readPool.Query(ctx, "fetch_ongoing_games", gameID, playerID)
	if err != nil {
		time.Sleep(retryDelay)
		rows, err = readPool.Query(ctx, "fetch_ongoing_games", gameID, playerID)
		if err != nil {
			panic(fmt.Errorf("Unable to fetch ongoing games: %v\n", err))
		}
	}
	defer rows.Close()

	var games []hex.Game
	for rows.Next() {
		var game hex.Game // Game is a struct representing a game, define it accordingly.
		err := rows.Scan(&game.GameID, &game.PlayerAID, &game.PlayerBID, &game.Outcome, &game.GameStartTime)
		if err != nil {
			return nil, err
		}
		games = append(games, game)
	}

	return games, nil
}

func UpdateGameOutcome(gameID uuid.UUID, outcome string, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := pool.Exec(ctx, "update_game_outcome", gameID, outcome)

	if err != nil {
		time.Sleep(retryDelay)
		_, err = pool.Exec(ctx, "update_game_outcome", gameID, outcome)

		if err != nil {
			panic(fmt.Errorf("Unable to update game outcome: %v\n", err))
		}
	}

	return err
}

func AddNewUser(username, passwordHash string, pool *pgxpool.Pool) (uuid.UUID, error) {
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()

	var newUserID uuid.UUID
	// i need to define this still	
	err := pool.QueryRow(ctx, "add_new_user", username, passwordHash).Scan(&newUserID)

	if err != nil {
		time.Sleep(retryDelay)
		err = pool.QueryRow(ctx, "add_new_user", username, passwordHash).Scan(&newUserID)

		if err != nil {
			panic(fmt.Errorf("Unable to add new user: %v\n", err))
		}
	}

	return newUserID, err
}

func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func Persist_EndGame(lastMove hex.Move, pool *pgxpool.Pool) error {
	// this function will be called after evaluate win condition determines indeed there was a winner!!!

	moveStrings := strings.Split(lastMove.MoveDescription, ".")
	gameOutcome := moveStrings[len(moveStrings)-1]

	if moveStrings[1] != "99" || moveStrings[2] != "99" || gameOutcome == "z" {
		return fmt.Errorf("persist_endgame function is called but the game is not yet over! last move fmt should be 999x.99.99.wincondition, not: %v\n", lastMove.MoveDescription)
	}

	// update the game outcome
	_, err := pool.Exec(context.Background(), "UpdateGameOutcomes", lastMove.GameID, gameOutcome)
	//options for outcome are: 'ongoing', 'forfeit', 'true_win', 'timeout', 'crash', 'veryshort'

	return err
}

// SANITYCHECK! this ensure that MemoryPersister implements all the methods of the _Persister interface,
// even though the _Persister global variable is not used anywhere in the code.
var _Persister = &MemoryPersister{}
