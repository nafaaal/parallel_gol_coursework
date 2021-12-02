package gol

import (
	"fmt"
	"strconv"
	"time"

	"uk.ac.bris.cs/gameoflife/util"
)

type distributorChannels struct {
	events     chan<- Event
	ioCommand  chan<- ioCommand
	ioIdle     <-chan bool
	ioFilename chan<- string
	ioOutput   chan<- uint8
	ioInput    <-chan uint8
}

func calculateNeighbours(width, y, x int, haloWorld [][]uint8) int {
	height := len(haloWorld)
	neighbours := 0
	for i := -1; i <= 1; i++ {
		for j := -1; j <= 1; j++ {
			if i != 0 || j != 0 {
				h := (y + height + i) % height
				w := (x + width + j) % width
				if haloWorld[h][w] == 255 {
					neighbours++
				}
			}
		}
	}
	return neighbours
}

func makeMatrix(height, width int) [][]uint8 {
	matrix := make([][]uint8, height)
	for i := range matrix {
		matrix[i] = make([]uint8, width)
	}
	return matrix
}

func readPgmData(p Params, c distributorChannels, turn int, world [][]uint8) [][]uint8 {
	c.ioCommand <- ioInput
	c.ioFilename <- strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
			data := <-c.ioInput
			world[col][row] = data
			if data == 255 {
				c.events <- CellFlipped{turn, util.Cell{X: row, Y: col}}
			}
		}
	}
	return world
}

func writePgmData(p Params, c distributorChannels, turn int, world [][]uint8) {
	filename := strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight) + "x" + strconv.Itoa(p.Turns)
	c.ioCommand <- ioOutput
	c.ioFilename <- filename
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
			if world[col][row] == 255 {
				c.ioOutput <- 255
			} else {
				c.ioOutput <- 0
			}
		}
	}
	c.events <- ImageOutputComplete{turn, filename}
}

func findAliveCells(p Params, world [][]uint8) []util.Cell {
	var alive []util.Cell
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
			if world[col][row] == 255 {
				alive = append(alive, util.Cell{X: row, Y: col})
			}
		}
	}
	return alive
}


func calculateNextState(p Params, c distributorChannels, startY, endY, turn int, worldCopy [][]uint8) [][]byte {
	height := len(worldCopy)-2
	width := p.ImageWidth
	newWorld := makeMatrix(height, width)

	for col, col1 := 0, 1; col < height; col, col1 = col+1, col1+1 {
		for row := 0; row < width; row++ {

			//startY+col gets the absolute y position when there is more than 1 worker
			n := calculateNeighbours(width, col1, row, worldCopy)
			currentState := worldCopy[col1][row]

			if currentState == 255 {
				if n == 2 || n == 3 {
					newWorld[col][row] = 255
				} else {
					c.events <- CellFlipped{CompletedTurns: turn, Cell: util.Cell{X: row, Y: startY + col}}
				}
			}

			if currentState == 0 {
				if n == 3 {
					newWorld[col][row] = 255
					c.events <- CellFlipped{CompletedTurns: turn, Cell: util.Cell{X: row, Y: startY + col}}
				}
			}
		}
	}

	return newWorld
}

func worker(p Params, c distributorChannels, startY, endY, turn int, worldCopy [][]uint8, out chan<- [][]uint8) {
	newPixelData := calculateNextState(p, c, startY, endY, turn, worldCopy)
	out <- newPixelData
}

func getWorkerSlice(world [][]uint8, startY, endY, workerIndex, numberOfWorkers int)[][]uint8{
	var workerSlice [][]uint8
	worldHeight := len(world)
	if workerIndex == 0 {
		workerSlice = append(workerSlice, world[worldHeight-1])
		workerSlice = append(workerSlice, world[startY:endY]...)
		workerSlice = append(workerSlice, world[endY])
	} else if workerIndex == numberOfWorkers-1 {
		workerSlice = append(workerSlice, world[startY-1])
		workerSlice = append(workerSlice, world[startY:endY]...)
		workerSlice = append(workerSlice, world[0])
	} else {
		workerSlice = append(workerSlice, world[startY-1])
		workerSlice = append(workerSlice, world[startY:endY]...)
		workerSlice = append(workerSlice, world[endY])
	}
	return workerSlice
}

func playTurn(p Params, c distributorChannels, turn int, world [][]byte) [][]byte {
	var newPixelData, worldCopy [][]uint8
	if p.Threads == 1 {
		worldCopy = append(worldCopy, world[len(world)-1])
		worldCopy = append(worldCopy, world...)
		worldCopy = append(worldCopy, world[0])
		newPixelData = calculateNextState(p, c, 0, p.ImageHeight, turn, worldCopy)
	} else {

		workerChannels := make([]chan [][]uint8, p.Threads)
		for i := 0; i < p.Threads; i++ {
			workerChannels[i] = make(chan [][]uint8)
		}

		workerHeight := p.ImageHeight / p.Threads

		for j := 0; j < p.Threads; j++ {
			startHeight := workerHeight * j
			endHeight := workerHeight * (j + 1)
			if j == p.Threads-1 { // send the extra part when workerHeight is not a whole number in last iteration
				endHeight += p.ImageHeight % p.Threads
			}
			worldCopy = getWorkerSlice(world, startHeight, endHeight, j, p.Threads)
			go worker(p, c, startHeight, endHeight, turn, worldCopy, workerChannels[j])
		}

		for k := 0; k < p.Threads; k++ {
			result := <-workerChannels[k]
			newPixelData = append(newPixelData, result...)
		}
	}

	return newPixelData
}

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels, keyPresses <-chan rune) {

	initialWorld := makeMatrix(p.ImageHeight, p.ImageWidth)
	turn := 0
	world := readPgmData(p, c, turn, initialWorld)
	ticker := time.NewTicker(2 * time.Second) //send something down ticker.C channel every 2 seconds

NextTurnLoop:
	for turn < p.Turns {
		select {
		case <-ticker.C:
			c.events <- AliveCellsCount{turn, len(findAliveCells(p, world))}
		case key := <-keyPresses:
			if key == 's' {
				fmt.Println("Starting output")
				writePgmData(p, c, turn, world)
			}
			if key == 'q' {
				writePgmData(p, c, turn, world)
				c.events <- StateChange{turn, Quitting}
				break NextTurnLoop
			}
			if key == 'p' {
				c.events <- StateChange{turn, Paused}
				for {
					await := <-keyPresses
					if await == int32(112) {
						c.events <- StateChange{turn, Executing}
						break
					}
				}
			}
		default:
			world = playTurn(p, c, turn, world)
			turn++
			c.events <- TurnComplete{turn}
		}
	}
	
	c.events <- FinalTurnComplete{turn, findAliveCells(p, world)}
	writePgmData(p, c, turn, world) // This line needed if out/ does not have files

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}

	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
