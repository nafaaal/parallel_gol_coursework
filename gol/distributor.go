package gol

import (
	"strconv"
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

func loadPgmData(p Params, c distributorChannels, world [][]uint8 )[][]uint8 {
	c.ioFilename <- strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
			world[col][row] = <-c.ioInput
		}
	}
	return world
}

func writePgmData(p Params, c distributorChannels, world [][]uint8){
	c.ioFilename <- strconv.Itoa(p.ImageWidth)+"x"+strconv.Itoa(p.ImageHeight)
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
			if world[col][row] == 255 {
				c.ioOutput <- 255
			} else {
				c.ioOutput <- 0
			}
		}
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

func calculateNextState(p Params, startY, endY, endX int, worldCopy func(y, x int) uint8) [][]byte {
	height := endY - startY
	newWorld := makeMatrix(height, endX)
	for col := 0; col < height; col++ {
		for row := 0; row < endX; row++ {
			n := neighbours(p, startY+col , row , worldCopy) // would need to be modified to get correct neighbours based on position
			if worldCopy(startY+col,row) == 255 {
				if n == 2 || n == 3 {
					newWorld[col][row] = 255
				}
			} else {
				if n == 3 {
					newWorld[col][row] = 255
				}
			}
		}
	}
	return newWorld
}


func worker (p Params, startY, endY, endX int, worldCopy func(y, x int) uint8, out chan<- [][]uint8){
	newPixelData := calculateNextState(p, startY, endY, endX, worldCopy)
	out <- newPixelData
}

func playTurn(p Params, world [][]byte) [][]byte {
	worldCopy := makeImmutableMatrix(world)
	var newPixelData [][]uint8
	if p.Threads == 1 {
		newPixelData = calculateNextState(p,0, p.ImageHeight, p.ImageWidth, worldCopy)
	} else {
		workerHeight := p.ImageHeight / p.Threads
		workerChannels := make([]chan [][]uint8, p.Threads)
		for i := 0; i < p.Threads; i++ {
			workerChannels[i] = make(chan [][]uint8)
		}
		for j := 0; j < p.Threads; j++ {
			if j == p.Threads - 1 { // send the extra part when p.ImageHeight / p.Threads is not a whole number
				extraHeight :=  workerHeight*(j+1) + (p.ImageHeight % p.Threads)
				go worker(p, workerHeight*j, extraHeight, p.ImageWidth, worldCopy, workerChannels[j])
			} else {
				go worker(p, workerHeight*j, workerHeight*(j+1), p.ImageWidth, worldCopy, workerChannels[j])
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
func distributor(p Params, c distributorChannels) {

	initialWorld := makeMatrix(p.ImageHeight, p.ImageWidth)

	c.ioCommand <-ioInput
	world := loadPgmData(p, c, initialWorld)

	turn := 0
	for turn = 0 ; turn<p.Turns; turn++ {
		world = playTurn(p, world)
		c.events <- TurnComplete{turn}
	}

	c.ioCommand <- ioOutput
	writePgmData(p, c, world)
	c.events <- FinalTurnComplete{turn, findAliveCells(p,world)}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}
	
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
