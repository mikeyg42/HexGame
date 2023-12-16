package main

import (
	"context"
	"fmt"
	"math"

	hex "github.com/mikeyg42/HexGame/structures"
	zap "go.uber.org/zap"
)

type GameInfo struct {
	WorkerID   string
	EvaluateProposedMoveLegality()
	EvaluateWinCondition()
	BroadcastGameEnd()
	BroadcastConnectionFail()
	DemandPlayersAck()
}

// game board is going to be 15x15, with 1,1 being the bottom left corner.
// rows 0 and 16 are going to be the hidden points for both players
// the x axis is the horizontal axis, and the y axis goes up and to the right. the z axis goes up and to the left
// z = -x - y
// hex.Vertex{X: 1, Y: 1} is the notation of vertices here
// move format: will be: [turn#][[playercode]].[X].[Y].[note]. note is not optional, fill with z if no note.
// each turn has two parts, a for player A and b for player B
// the opening 2 moves are part of turn #0 and are notated as 0a.X.Y.kept or 0a.X.Y.taken and then either 0b.X.Y.swap or 0b.F.H.noswap
// then the first move of turn 1 is 1a.X.Y.z and so on....
// when game is over, the last entry will be the 97[player code].99.99.[win condition]
// coordinates will all be logged as absolute coordinates. The validity of each  move will be done in absolute coordinates too.
// BUT, for player B, the win condition eval requires 1st swapping all the X's and Y's of both players so that the graph-based algorithm can be run always evaluating LEFT to RIGHT
// ^^ remember that ONLY win condition get non-absolute coordinates, all other moves are absolute coordinates

// imagine the 2x2 board with the top left and bottom right corners filled in by the same player, thenb

// used by memoryTool and refereeTool.
// newVert is in abolute coordinates, not relative to the identity of the player
func IncorporateNewVert(ctx context.Context, moveList []hex.Vertex, adjGraph [][]int, newVert hex.Vertex) ([][]int, []hex.Vertex) {
	// it is assumwed that the newVert is a valid move, so we don't need to check for that here

	// Update Moves
	updatedMoveList := append(moveList, newVert)
	moveCount := len(updatedMoveList)

	// Calculate new size having now appended a new vertex
	sizeNewAdj := moveCount + 2

	// Preallocate/new adjacency graph with initial values copied from the old adjacency graph
	newAdjacencyGraph := make([][]int, sizeNewAdj)
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
			if containsVert(moveList, potentialEdgePair) {
				// Edge found between new vertex and an existing vertex
				newAdjacencyGraph[k][moveCount] = 1
				newAdjacencyGraph[moveCount][k] = 1
			}
		}
	}

	// Check if new vertex is in the first column or last column
	if newVert.X == 1 {
		// Edge found between new vertex and the leftmost hidden point
		newAdjacencyGraph[0][sizeNewAdj-1] = 1
		newAdjacencyGraph[sizeNewAdj-1][0] = 1
	} else if newVert.X == SideLenGameboard-1 {
		// Edge found between new vertex and rightmost hidden point
		newAdjacencyGraph[1][sizeNewAdj-1] = 1
		newAdjacencyGraph[sizeNewAdj-1][1] = 1
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
	if isSymmetric(thinnedAdj) {
		return nil, fmt.Errorf("gamestate breakdown: %v", "Adjacency matrix is not symmetric, something went wrong, terribly wrong")
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

func RemoveVertices(s []hex.Vertex, indices []int) []hex.Vertex {
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
	for i, row := range slice {
		for j, val := range row {
			result[j][i] = val
		}
	}
	return result
}

// converts the x coordinate from a letter to an int as in A->1->0 (zero indexed!), B->->1, ect.
func convertToInt(xCoord string) (int, error) {
	// x coordinate will always be a letter between A and O, the 15th letter, and bc the board is 15x15, the max value for xCoord is 15
	if len(xCoord) != 1 {
		return -1, fmt.Errorf("invalid input length for xCoord, should be only one letter, not: %v", xCoord)
	}

	// coordinates on our game board are zero-indexed! So we want a--> 0, b--> 1, etc. until o --> 14
	return int(xCoord[0]) - int('A'), nil
}

// takes in the xcoordinate as a string (a,b,c,d...)+ y coord (0,1,2... ) and spits out a vertex (x,y) pair
func ConvertToTypeVertex(xCoord string, yCoord int) (hex.Vertex, error) {
	x, err := convertToInt(xCoord)
	if err != nil {
		return hex.Vertex{
			X: x, Y: yCoord}, err
	}
	return hex.Vertex{
		X: x,
		Y: yCoord}, nil
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

// moveList is the list of moves made by BOTH player, whereas the adjG is the adjacency matrix of the gameboard for ONE player
// new movelist INCLUDES the move that was just made by the player
func EvalWinCondition(playerID string, adjG [][]int, moveList []hex.Vertex) bool {

	// ascertain the # of moves made
	totalNumMoves := len(moveList)

	// eval if player A or player B -- flip all tile coords for player B only
	if totalNumMoves%2 == 0 {
		moveList = flipPlayerB(moveList)
	}

	numMoves := int(math.Floor(float64(totalNumMoves) / 2)) // refers to the moves of just this player, not both summed
	// These vars refer to cols and rows of the adjacency matrix
	numRows := numMoves + 2
	numCols := numRows

	// check coinditions where no win is possible
	// Condition 1: Check if enough tiles to traverse the whole game board
	// Condition 2: Check if at least 1 tile is placed in each column
	if checkCondition1(moveList) || checkCondition2(moveList) {
		return false
	}

	// Condition 3: Check if there is a path from the leftmost hidden point to the rightmost hidden point via thinning
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
		// If there are no degree =0 or =1 nodes, break the loop
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
		thinnedMoveList = RemoveVertices(thinnedMoveList, lowDegreeNodes)

		// Check if condition 1 is still met
		if len(thinnedMoveList) < SideLenGameboard {
			return false
		}

		// check condition 2: Iterate across the columns of gameboard, and any cols unrepresented in the columnSet map set to false
		if checkCondition1(thinnedMoveList) || checkCondition2(thinnedMoveList) {
			return false
		}
		// Iff we make it to here then we have not thinned enough, and so we proceed with another iteration of thinning
	}
}

func checkCondition1(moveList []hex.Vertex) bool {
	return len(moveList) < SideLenGameboard
}

func checkCondition2(moveList []hex.Vertex) bool {

	columnSet := make(map[int]bool)
	for _, move := range moveList {
		columnSet[move.X] = true
	}
	for k := 0; k < SideLenGameboard; k++ {
		if !columnSet[k] {
			return false
		}
	}
	return true
}
