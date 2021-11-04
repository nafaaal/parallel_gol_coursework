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
	ioOutput   chan<- uint8  //data is sent to this eg: ioOutput <- var
	ioInput    <-chan uint8 //data is received from this eg: var := <- ioInput
}

func isAlive(n byte) byte {
	if n == 255 {
		return 1
	}
	return 0
}


func neighbours(p Params, y, x int , data func(y, x int) uint8) uint8 {
	var num uint8
	for i := -1; i<2; i++{
		for j := -1; j<2; j++{
			if i != 0 || j != 0 {
				height := (y + p.ImageHeight + i) % p.ImageHeight
				width := (x + p.ImageWidth + j) % p.ImageWidth
				num += isAlive(data(height, width))
			}
		}
	}
	return num
}

func makeImmutableMatrix(matrix [][]uint8) func(y, x int) uint8 {
	return func(y, x int) uint8 {
		return matrix[y][x]
	}
}

func makeMatrix(height, width int) [][]uint8 {
	matrix := make([][]uint8, height)
	for i := range matrix {
		matrix[i] = make([]uint8, width)
	}
	return matrix
}

func readPgmData(p Params, c distributorChannels, world [][]uint8 )[][]uint8 {
	c.ioCommand <-ioInput
	c.ioFilename <- strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
			data :=  <- c.ioInput
			world[col][row] = data
			if data == 255 {
				c.events <- CellFlipped{0,  util.Cell{X: row, Y: col}}
			}
		}
	}
	return world
}

func writePgmData(p Params, c distributorChannels, turn int, world [][]uint8){
	filename := strconv.Itoa(p.ImageWidth)+"x"+strconv.Itoa(p.ImageHeight)+"x"+strconv.Itoa(p.Turns)
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

func tick(i chan int){
	for {
		i <- 1
		<-time.After(2*time.Second)
	}
}

func findAliveCells(p Params, world [][]uint8) []util.Cell{
	var a []util.Cell
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
			if world[col][row] == 255 {
				a = append(a, util.Cell{X: row, Y: col})
			}
		}
	}
	return a
}


func calculateNextState(p Params, c distributorChannels, startY, endY, endX, turn int, worldCopy func(y, x int) uint8,) [][]byte {
	height := endY - startY
	newWorld := makeMatrix(height, endX)
	for col := 0; col < height; col++ {
		for row := 0; row < endX; row++ {
			n := neighbours(p, startY+col , row , worldCopy) // would need to be modified to get correct neighbours based on position
			if worldCopy(startY+col,row) == 255 {
				if n == 2 || n == 3 {
					newWorld[col][row] = 255
				} else {
					c.events <- CellFlipped{CompletedTurns: turn, Cell: util.Cell{X: row, Y: startY+col}}
				}
			} else {
				if n == 3 {
					newWorld[col][row] = 255
					c.events <- CellFlipped{CompletedTurns: turn, Cell: util.Cell{X: row, Y: startY+col}}
				}
			}
		}
	}
	return newWorld
}


func worker (p Params, c distributorChannels, startY, endY, endX, turn int, worldCopy func(y, x int) uint8, out chan<- [][]uint8,){
	newPixelData := calculateNextState(p, c, startY, endY, endX, turn, worldCopy)
	out <- newPixelData
}

func playTurn(p Params, c distributorChannels, turn int, world [][]byte) [][]byte {
	worldCopy := makeImmutableMatrix(world)
	var newPixelData [][]uint8
	if p.Threads == 1 {
		newPixelData = calculateNextState(p, c,0, p.ImageHeight, p.ImageWidth, turn, worldCopy)
	} else {
		workerHeight := p.ImageHeight / p.Threads
		workerChannels := make([]chan [][]uint8, p.Threads)
		for i := 0; i < p.Threads; i++ {
			workerChannels[i] = make(chan [][]uint8)
		}
		for j := 0; j < p.Threads; j++ {
			if j == p.Threads - 1 { // send the extra part when p.ImageHeight / p.Threads is not a whole number
				extraHeight :=  workerHeight*(j+1) + (p.ImageHeight % p.Threads)
				go worker(p, c, workerHeight*j, extraHeight, p.ImageWidth, turn, worldCopy, workerChannels[j])
			} else {
				go worker(p, c, workerHeight*j, workerHeight*(j+1), p.ImageWidth, turn, worldCopy, workerChannels[j])
			}
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
	world := readPgmData(p, c, initialWorld)

	turn := 0

	i := make(chan int)
	go tick(i)

	//AliveCellsCount { CompletedTurns int, CellsCount int } FINISHED
	//ImageOutputComplete { CompletedTurns int, Filename string } FINISHED
	//StateChange { CompletedTurns int, NewState State } FINISHED
	//CellFlipped { CompletedTurns int, Cell State } // This even should be sent every time a cell changes state.
	//TurnComplete { CompletedTurns int }
	//FinalTurnComplete { CompletedTurns int, []util.Cell } FINISHED

NextTurnLoop:
	for turn <p.Turns {
		select {
		case <- i:
			c.events <- AliveCellsCount{turn, len(findAliveCells(p, world))}
		case key := <- keyPresses:
			if key == int32(115) { // 's'
				fmt.Println("Starting output")
				writePgmData(p, c, turn, world)
			}
			if key == int32(113) { // 'q'
				writePgmData(p, c, turn, world)
				c.events <- StateChange{turn, Quitting}
				break NextTurnLoop
			}
			if key == int32(112) { // 'p'
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
			c.events <- TurnComplete{ turn}
		}
	}

	c.events <- FinalTurnComplete{turn, findAliveCells(p,world)}
	writePgmData(p, c, turn, world) // This line needed if out/ does not have files

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}
	
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
