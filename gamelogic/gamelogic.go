package gamelogic

import (
	"context"
	"fmt"

	hex "github.com/mikeyg42/HexGame/structures"
)

type myKey string

const (
	CurrentPlayerIDKey myKey = "currentPlayerID"
)

/*
type GameInfo struct {
	WorkerID string

			EvaluateProposedMoveLegality func(context.Context, hex.Vertex) bool
		 	EvaluateWinCondition()
			BroadcastGameEnd()
			BroadcastConnectionFail()
			DemandPlayersAck()

}
*/

// game board is going to be 15x15 for now, with 1,1 being the bottom left corner.

// rows 0 and 16 are the hidden points for both players

// the x axis is the horizontal axis, and the y axis goes up and to the right. the z axis goes up and to the left
// z = -x - y
// hex.Vertex{X: 1, Y: 1} is the notation of vertices here
//
// move format: will be: [turn#][[playercode]].[X].[Y].[note] ---- note is not optional, fill with z if no note.
// each turn has two parts, "a" for player A and "b" for player B
// first turn = turn 0
// the opening 2 moves are part of turn #0 and are notated as 0a.X.Y.kept or 0a.X.Y.taken and then either 0b.X.Y.swap or 0b.F.H.noswap
// then the first move of turn 1 is 1a.X.Y.z and so on....
// when game is over, the last entry will be the 99[player code].99.99.[win condition]

// coordinates will all be logged as absolute coordinates. The validity of each  move will be done in absolute coordinates too.
// BUT, for player B, the win condition eval requires 1st swapping all the X's and Y's of both players so that the graph-based algorithm can be run always evaluating LEFT to RIGHT
// ^^ remember that ONLY win condition get non-absolute coordinates, all other moves are absolute coordinates

// used by memoryTool and refereeTool??
// newVert is in abolute coordinates, not relative to the identity of the player, ie player A and B's moves are in the same coord syst4em and there should be no overlaps
// updatedMoveList has the moves of BOTH players
func IncorporateNewVert(ctx context.Context, allMoveList []hex.Vertex, adjGraph [][]int, newVert hex.Vertex) (newAdjacencyGraph [][]int, updatedMoveList []hex.Vertex) {
	// it is assumed that the newVert is a valid move, so we don't need to check for that here

	// Update Moves
	updatedMoveList = append(allMoveList, newVert)

	playerMoveList := extractPlayerMovesFromAllMoves(ctx, updatedMoveList)
	moveCount := len(playerMoveList)

	// Calculate new size of adjMatrix, having now appended a new vertex
	sizeNewAdj := moveCount + 2

	// Preallocate/new adjacency graph with initial values copied from the old adjacency graph
	newAdjacencyGraph = make([][]int, sizeNewAdj)
	for i := 0; i < sizeNewAdj; i++ {
		newAdjacencyGraph[i] = make([]int, sizeNewAdj)
	}

	// Copy values from the old adjacency graph
	for i := 0; i < len(adjGraph); i++ {
		copy(newAdjacencyGraph[i], adjGraph[i])
	}

	// Find adjacent vertices by comparing the list of 6 adjacent vertex list to game state move list, if one of the new points neighbors is in the move list, then there is an edge between the new point and that existing point
	sixAdjacentVertices := getAdjacentVertices(newVert)
	for k := 2; k < moveCount+2; k++ {
		for _, potentialEdgePair := range sixAdjacentVertices {
			if containsVert(playerMoveList, potentialEdgePair) {
				// Edge found between new vertex and an existing vertex
				newAdjacencyGraph[k][moveCount] = 1
				newAdjacencyGraph[moveCount][k] = 1
			}
		}
	}

	// Check if new vertex is in the first column or last column
	playerID := ctx.Value(CurrentPlayerIDKey)
	if playerID == "A" {
		if newVert.X == 1 {
			// Edge found between new vertex and the leftmost hidden point
			newAdjacencyGraph[0][sizeNewAdj-1] = 1
			newAdjacencyGraph[sizeNewAdj-1][0] = 1
		} else if newVert.X == hex.SideLenGameboard-1 {
			// Edge found between new vertex and rightmost hidden point
			newAdjacencyGraph[1][sizeNewAdj-1] = 1
			newAdjacencyGraph[sizeNewAdj-1][1] = 1
		}
	} else if playerID == "B" {
		if newVert.Y == 1 {
			// Edge found between new vertex and the lowermost hidden point
			newAdjacencyGraph[0][sizeNewAdj-1] = 1
			newAdjacencyGraph[sizeNewAdj-1][0] = 1
		} else if newVert.Y == hex.SideLenGameboard-1 {
			// Edge found between new vertex and uppermost hidden point
			newAdjacencyGraph[1][sizeNewAdj-1] = 1
			newAdjacencyGraph[sizeNewAdj-1][1] = 1
		}
	} else {
		panic(fmt.Errorf("error in gamestate breakdown: %v", "playerID is not A or B"))
	}

	return newAdjacencyGraph, updatedMoveList
}

func getAdjacentVertices(vertex hex.Vertex) []hex.Vertex {
	return []hex.Vertex{
		{X: vertex.X - 1, Y: vertex.Y + 1},
		{X: vertex.X - 1, Y: vertex.Y},
		{X: vertex.X, Y: vertex.Y - 1},
		{X: vertex.X, Y: vertex.Y + 1},
		{X: vertex.X + 1, Y: vertex.Y},
		{X: vertex.X + 1, Y: vertex.Y - 1},
	}
}

func ThinAdjacencyMat(adj [][]int, indices []int) ([][]int, error) {
	temp := removeRows(adj, indices)
	temp = transpose(temp)
	thinnedAdj := removeRows(temp, indices)

	// Check for matrix symmetry
	if !isSymmetric(thinnedAdj) {
		return nil, fmt.Errorf("gamestate breakdown: %v", "Adjacency matrix is Not symmetric post thinning, something is terribly wrong")
	}

	return thinnedAdj, nil
}

func containsInt(items []int, item int) bool {
	for _, val := range items {
		if val == item {
			return true
		}
	}
	return false
}

func containsVert(vertices []hex.Vertex, target hex.Vertex) bool {
	for _, v := range vertices {
		if v.X == target.X && v.Y == target.Y {
			return true
		}
	}
	return false
}

func removeRows(s [][]int, indices []int) [][]int {
	result := make([][]int, 0)

	for i, row := range s {
		if containsInt(indices, i) {
			continue
		}

		newRow := make([]int, 0)

		for j, val := range row {
			if containsInt(indices, j) {
				continue
			}

			newRow = append(newRow, val)
		}
		result = append(result, newRow)
	}
	return result
}

func removeVerts_moveList(s []hex.Vertex, indices []int) []hex.Vertex {
	result := make([]hex.Vertex, 0)
	for i, vertex := range s {
		if containsInt(indices, i) {
			continue
		}
		result = append(result, vertex)
	}
	return result
}

// checks if a matrix is symmetric, an essential quality of an adjacency matrix
func isSymmetric(matrix [][]int) bool {
    rows := len(matrix)
    if rows == 0 {
        return true
    }
    cols := len(matrix[0])

    // Check if the matrix is square
    if rows != cols {
        return false
    }

    for i, row := range matrix {
        if len(row) != cols {
            return false
        }
        for j := i + 1; j < cols; j++ {
            if matrix[i][j] != matrix[j][i] {
                return false
            }
        }
    }

    return true
}

func transpose(slice [][]int) [][]int {
	numRows := len(slice)
	if numRows == 0 {
		return [][]int{}
	}

	numCols := len(slice[0])
	result := make([][]int, numCols)
	for i := range result {
		result[i] = make([]int, numRows)
	}

	for i := 0; i < numRows; i++ {
		for j := 0; j < numCols; j++ {
			result[j][i] = slice[i][j]
		}
	}

	return result
}

// ..... CHECKING FOR WIN CONDITION

// first thing we need to do is to flip the axes of the gameboard for player B, so that the win condition algorithm can be run in the same way for both players
func flipPlayerB(moveList []hex.Vertex) []hex.Vertex {
	flippedMoveList := make([]hex.Vertex, len(moveList))
	for j, move := range moveList {
		flippedMoveList[j] = hex.Vertex{X: move.Y, Y: move.X}
	}
	return flippedMoveList
}

func extractPlayerMovesFromAllMoves(ctx context.Context, allMoveList []hex.Vertex) []hex.Vertex {

	k := 0
	if ctx.Value(CurrentPlayerIDKey) == "B" {
		k = 1
	}

	movesList := []hex.Vertex{}
	for k < len(allMoveList) {
		movesList = append(movesList, allMoveList[k])
		k += 2
	}

	return movesList
}

// allMovesList is the list of moves made by BOTH player, whereas the adjG                                                                                                                                                                        r
func EvalWinCondition(ctx context.Context, adjG [][]int, allMovesList []hex.Vertex) bool {

	// ascertain the # of moves made
	totalNumMoves := len(allMovesList)

	// evaluate if there are enough moves to win, this is tantamount to checking condition #1
	if totalNumMoves < 2*hex.SideLenGameboard-1 {
		return false // if there are not enough moves to win, then return false
	}

	// eval if player A or player B -- flip all tile coords for player B only. also k will be the index of the players first move.
	if ctx.Value(CurrentPlayerIDKey) == "B" {
		allMovesList = flipPlayerB(allMovesList)
	}

	moveList := extractPlayerMovesFromAllMoves(ctx, allMovesList)

	numMoves := len(moveList)
	// These vars refer to cols and rows of the adjacency matrix
	numRows := numMoves + 2
	numCols := numRows

	// check conditions where no win is possible
	// Condition 1: Check if enough tiles to traverse the whole game board -- we already checked this earlier!!!
	// Condition 2: Check if at least 1 tile is placed in each column
	if !checkCondition2(moveList) {
		return false
	}

	// Condition 3: Check if there is a path from one hidden point to the other after repeatedly thinning degree 0 and 1 nodes from the list
	thinnedAdj := make([][]int, len(adjG))
	copy(thinnedAdj, adjG)

	thinnedMoveList := make([]hex.Vertex, numMoves)
	copy(thinnedMoveList, moveList)

	// thinning loop
	for {
		// Find degree 0 and 1 nodes (excluding end points)
		lowDegreeNodes := make([]int, 0)
		for i := 2; i < numRows; i++ {
			degree := 0
			for j := 0; j < numCols; j++ {
				degree += thinnedAdj[i][j]
			}
			if degree == 0 || degree == 1 {
				lowDegreeNodes = append(lowDegreeNodes, i)
			}
		}

		// THE ONLY WAY TO WIN: after exhausting thinning, if you still have have all notes w/ degree 2 (except the hidden ones...)
		// If there are no degree = 0 or = 1 nodes, break the loop
		if len(lowDegreeNodes) == 0 {
			return true
		}

		// else, thin again!
		thinnedAdj, err := ThinAdjacencyMat(thinnedAdj, lowDegreeNodes)
		if err != nil {
			panic(fmt.Errorf("error thinning matrix eval win cond.: %v", err))
		}

		// Update adjacency matrix and dimensions
		numRows = len(thinnedAdj)
		numCols = numRows

		// Update move list
		thinnedMoveList = removeVerts_moveList(thinnedMoveList, lowDegreeNodes)

		// Check condition 1: is moveList long enough after thinning? (if not, then no win is possible)
		// check condition 2: Iterate across the columns of gameboard, and any cols unrepresented in the columnSet map set to false
		if !checkCondition1(thinnedMoveList) || !checkCondition2(thinnedMoveList) {
			// if either condition is not met, then no win is possible
			return false
		}
		// Iff we make it to here then we have not thinned enough, and so we proceed with another iteration of thinning
	}
}

// returns true if the moveList is long enough to traverse the whole game board
func checkCondition1(moveList []hex.Vertex) bool {
	return len(moveList) >= hex.SideLenGameboard // if the moveList is long enough, then return true that condition 1 is met
}

// returns true if there is at least one tile in each column
func checkCondition2(moveList []hex.Vertex) bool {

	columnSet := make(map[int]struct{})
	// map in which each x value in the moveList is a key set to true.
	for _, move := range moveList {
		columnSet[move.X] = struct{}{}
	}
	// now we check each value 1->15 or whatever side length is set to.
	for k := 1; k <= hex.SideLenGameboard; k++ {
		if _, exists := columnSet[k]; !exists {
			return false // if any column is not represented, then return false
		}
	}
	return true // if we make it to here, then all columns are represented and condition is met
}

// TestWinCondition is a test function for the win condition algorithm
func TestWinCondition() bool {

	player1moves := []hex.Vertex{

		{X: 2, Y: 1},
		{X: 2, Y: 2},
		{X: 2, Y: 3},
		{X: 3, Y: 3},
		{X: 4, Y: 2},
		{X: 4, Y: 4},
		{X: 2, Y: 4},
		{X: 2, Y: 5},
		{X: 2, Y: 6},
		{X: 2, Y: 7},
		{X: 2, Y: 8},
		{X: 2, Y: 9},
		{X: 2, Y: 10},
		{X: 2, Y: 11},
		{X: 2, Y: 12},
		{X: 2, Y: 13},
		{X: 2, Y: 14},
	}
	p1_newMove := hex.Vertex{X: 1, Y: 15}

	player2moves := []hex.Vertex{
		{X: 1, Y: 1},
		{X: 1, Y: 2},
		{X: 1, Y: 3},
		{X: 1, Y: 4},
		{X: 1, Y: 5},
		{X: 1, Y: 6},
		{X: 1, Y: 7},
		{X: 1, Y: 8},
		{X: 10, Y: 5},
		{X: 9, Y: 5},
		{X: 10, Y: 4},
		{X: 1, Y: 9},
		{X: 1, Y: 10},
		{X: 1, Y: 11},
		{X: 1, Y: 12},
		{X: 1, Y: 13},
		{X: 1, Y: 14},
	}

	moveList := []hex.Vertex{}
	p1adj := [][]int{{0, 0}, {0, 0}}
	p2adj := [][]int{{0, 0}, {0, 0}}

	ctxA := context.WithValue(context.Background(), CurrentPlayerIDKey, "A")
	ctxB := context.WithValue(context.Background(), CurrentPlayerIDKey, "B")
	k := 0
	for k < len(player1moves) {
		p1adj, moveList = IncorporateNewVert(ctxA, moveList, p1adj, player1moves[k])
		p1 := EvalWinCondition(ctxA, p1adj, moveList)
		fmt.Printf("on turn %d, playerA win = %v", k, p1)

		p2adj, moveList = IncorporateNewVert(ctxB, moveList, p2adj, player2moves[k])
		p2 := EvalWinCondition(ctxB, p2adj, moveList)
		fmt.Printf("on turn %d, playerB win = %v", k, p2)
		if !p1 || !p2 {
			return false
		}
	}

	p1adj, moveList = IncorporateNewVert(ctxA, moveList, p1adj, p1_newMove)
	p1fin := EvalWinCondition(ctxA, p1adj, moveList)
	fmt.Printf("on turn %d, playerA win = %v", k, p1fin)

	return p1fin
}
