package main

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
	hex "github.com/mikeyg42/HexGame/HexGame/structures"

	SQL_namedPreparedStmts "github.com/mikeyg42/HexGame/sqlconstants"
	pgxUUID "github.com/vgarvardt/pgx-google-uuid/v5"
)

type MemoryPersister struct{}

const maxReadPoolSize = 10
const maxWritePoolSize = 10
const readTimeout = 2 * time.Second
const writeTimeout = 2 * time.Second
const maxRetries = 3
const retryDelay = 500 * time.Millisecond

const SideLenGameboard = 15

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

	// Check admin permissions
	tf, role, err1 := checkAdminPermissions(ctx, pool.WritePool)
	tf2, role2, err2 := checkAdminPermissions(ctx, pool.ReadPool)
	if role != "admin" || role2 != "admin" || err1 != nil || err2 != nil || !tf || !tf2 {
		panic(fmt.Errorf("admin permission check failed: %v, %v", err1, err2))
	}
	fmt.Println("Successfully initialized connection pools with admin permissions")
}

func testConnection(ctx context.Context, pool *pgxpool.Pool) error {
	var dummy int
	if err := pool.QueryRow(ctx, "SELECT 1").Scan(&dummy); err != nil {
		return err
	}
	return nil
}

func checkAdminPermissions(ctx context.Context, pool *pgxpool.Pool) (bool, string, error) {
	var currentUser string
	err := pool.QueryRow(ctx, "SELECT current_user").Scan(&currentUser)
	if err != nil {
		return false, "?", err
		// handle error
	}

	if currentUser == "admin" {
		return true, currentUser, nil
	}

	if strings.Contains(currentUser, "only") {
		fmt.Println("Current user is, %s, is trying to initialize a database connection despite being read-only ", currentUser)
		// indicates read-only permissions
		return false, currentUser, nil
	}
	fmt.Println("Current user is, %s, is trying to initialize a database connection withou admin priv. this shouldnt be allowed!!! ", currentUser)
	return false, currentUser, fmt.Errorf("unexpected user initializing database connection: %s", currentUser)
}

// check that the pools are working one at a time
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

func isDatabaseSetupCorrectly(ctx context.Context, p *hex.PooledConnections) (bool, error) {

	// 1. Check for table existence
	tables := []string{"users", "games", "moves"}
	for _, table := range tables {
		var tablename string
		err := p.ReadPool.QueryRow(ctx, "SELECT tablename FROM pg_tables WHERE schemaname = 'public' AND tablename = $1;", table).Scan(&tablename)
		if err != nil {
			if err == pgx.ErrNoRows {
				return false, fmt.Errorf("table %s does not exist", table)
			}
			return false, err
		}
	}

	// 2. Check for roles existence
	roles := []string{"app_read", "app_write", "app_auth"}
	for _, role := range roles {
		var rolname string
		err := p.ReadPool.QueryRow(ctx, "SELECT rolname FROM pg_roles WHERE rolname = $1;", role).Scan(&rolname)
		if err != nil {
			if err == pgx.ErrNoRows {
				return false, fmt.Errorf("role %s does not exist: see pgx.ErrNoRows: %v", role, err)
			}
			return false, err
		}
	}

	// ping connections
	errPool := PingPooledConnections(ctx, p)
	if errPool != nil {
		panic(fmt.Errorf("Unable to ping connection pool: %v\n", errPool))
	}

	// 4. Insert and retrieve dummy data, then delete
	// make a dummy game entry using AddNew Game prepared statement
	userID := uuid.Must(uuid.NewRandom()).String()
	var gameID uuid.UUID

	tx, err := p.WritePool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx) // This will rollback if tx.Commit() isn't called

	_, err = tx.Exec(ctx, "Add_new_user", userID, "password")
	if err != nil {
		return false, fmt.Errorf("failed to insert dummy data into users: %w", err)
	}
	_, err = tx.Exec(ctx, "Add_new_ugae", userID, "password")
	if err != nil {
		panic(fmt.Errorf("Failed to execute prepared statement: %v\n", err))
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
	batch.Queue("DeleteGame", gameID)

	// Send the batch to the server
	br := p.WritePool.SendBatch(ctx, batch)

	// Close the batch operation pool after processing
	defer br.Close()

	// Check results for the first command
	if _, err := br.Exec(); err != nil {
		return false, fmt.Errorf("failed to delete user data from users: %w", err)
	}

	// Check results for the second command
	if _, err := br.Exec(); err != nil {
		return false, fmt.Errorf("failed to delete game data: %w", err)
	}

	// If everything went fine
	return true, nil
	// Return true or any other result indicating success

}

func NEW_UUID() (string, error) {
	newUUID := uuid.Must(uuid.NewRandom())

	return newUUID.String(), nil
}

// uses a transaction to delete from both tables all at once or not at all/
func DeleteGameMemory(ctx context.Context, gameID string, writePool *pgxpool.Pool) error {
	tx, err := writePool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) // This will rollback if tx.Commit() isn't called

	// Delete from moves table
	deleteMovesQuery := `DELETE FROM moves WHERE game_id = $1;`
	_, err = tx.Exec(ctx, deleteMovesQuery, gameID)
	if err != nil {
		return err
	}

	// Delete from games table
	deleteGameQuery := `DELETE FROM games WHERE game_id = $1;`
	_, err = tx.Exec(ctx, deleteGameQuery, gameID)
	if err != nil {
		return err
	}

	// Commit the transaction if everything went fine
	err = tx.Commit(ctx)
	if err != nil {
		return err
	}

	return nil
}

func InitiateNewGame(playerA, playerB uuid.UUID, writePool *pgxpool.Pool) (uuid.UUID, error) {
	var newGameID uuid.UUID

	// Use the name of the prepared statement, provide the required parameters, and scan the result into newGameID
	err := writePool.QueryRow(context.Background(), "AddNewGame", playerA, playerB).Scan(&newGameID)
	if err != nil {
		fmt.Println("Failed to execute prepared statement AddNewGame: %v\n", err)
		return uuid.Nil, err
	}
	fmt.Printf("New game initiated with ID: %s\n", newGameID)
	return newGameID, nil
}

// AddMoveToMemory() adds a move to the moves table
func AddMoveToMemory(move hex.Move, pool *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sql := "INSERT INTO moves(gameID, playerID, move_info) VALUES($1, $2, $3)"
	_, err := pool.Exec(ctx, sql, move.GameID, move.PlayerID, move.MoveDescription)

	if err != nil {
		time.Sleep(retryDelay)
		_, err = pool.Exec(ctx, sql, move.GameID, move.PlayerID, move.MoveDescription)

		if err != nil {
			panic(fmt.Errorf("Unable to add move to moves table: %v\n", err))
		}
	}

	return nil
}

// this is a wrapper function for the exported Fetch commands fro the postgresQL database
func FetchSomeMoveList(readPool *pgxpool.Pool, game hex.Game, whatToFetch string) ([]hex.Move, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	switch whatToFetch {
	case "entireGame":
		return FetchLatestThreeMovesInGame(ctx, game.GameID, readPool)

	case "playerA":
		return FetchParticularPlayersMoves(ctx, game.PlayerAID, readPool)

	case "playerB":
		return FetchParticularPlayersMoves(ctx, game.PlayerBID, readPool)

	case "latestThree":
		return FetchLatestThreeMovesInGame(ctx, game.GameID, readPool)

	default:
		err := fmt.Errorf("invalid input to FetchSomeMoveList(): %v\n", whatToFetch)
		return nil, err
	}
}

func FetchLatestThreeMovesInGame(ctx context.Context, gameID uuid.UUID, readPool *pgxpool.Pool) ([]hex.Move, error) {

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

func FetchParticularPlayersMoves(ctx context.Context, playerGameCode uuid.UUID, readPool *pgxpool.Pool) ([]hex.Move, error) {

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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var newUserID uuid.UUID
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

var _Persister = &MemoryPersister{}
