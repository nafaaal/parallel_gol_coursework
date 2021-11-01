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

func retOne(n byte) byte{
	if n>0{
		return 1;
	}
	return 0;
}


func neighbours(p Params, y, x int , world [][]byte) byte {
	imht := p.ImageHeight
	imwt := p.ImageWidth
	n := retOne(world[(y+imht-1)%imht][(x+imwt-1)%imwt]) + retOne(world[(y+imht-1)%imht][(x+imwt)%imwt]) + retOne(world[(y+imht-1)%imht][(x+imwt+1)%imwt])
	n += retOne(world[(y+imht)%imht][(x+imwt-1)%imwt]) +                                          		   retOne(world[(y+imht)%imht][(x+imwt+1)%imwt])
	n += retOne(world[(y+imht+1)%imht][(x+imwt-1)%imwt]) + retOne(world[(y+imht+1)%imht][(x+imwt)%imwt]) + retOne(world[(y+imht+1)%imht][(x+imwt+1)%imwt])
	return n;
}

func makeMatrix(height, width int) [][]uint8 {
	matrix := make([][]uint8, height)
	for i := range matrix {
		matrix[i] = make([]uint8, width)
	}
	return matrix
}

func loadPgmData(p Params, c distributorChannels, temp [][]uint8 )[][]uint8 {
	c.ioFilename <- strconv.Itoa(p.ImageWidth) + "x" + strconv.Itoa(p.ImageHeight)
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
			temp[col][row] = <-c.ioInput
		}
	}
	return temp
}

func writePgmData(p Params, c distributorChannels, temp [][]uint8){
	c.ioFilename <- strconv.Itoa(p.ImageWidth)+"x"+strconv.Itoa(p.ImageHeight)
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
			if temp[col][row] == 255 {
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

func calculateNextState(p Params, startY, endY, startX, endX int, world [][]uint8) [][]byte {
	height := endY - startY
	width := endX - startX
	
	newWorld := makeMatrix(height,width) // probs using some other calculation
	for col := 0; col < endY; col++ {
		for row := 0; row < endX; row++ {
			n := neighbours(p, col , row , world)
			if world[col][row] == 255 {
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


func worker (p Params, startY, endY, startX, endX int, world [][]uint8, out chan<- [][]uint8){
	newPixelData := calculateNextState(p, startY, endY, startX, endX, world)
	out <- newPixelData
}

func filter(p Params, world [][]byte) [][]byte {
	var newPixelData [][]uint8
	if p.Threads == 1 {
		newPixelData = calculateNextState(p,0, p.ImageHeight, 0, p.ImageWidth, world)
	} else {
		workerHeight := p.ImageHeight / p.Threads
		workerChannels := make([]chan [][]uint8, p.Threads)
		for i := 0; i < p.Threads; i++ {
			workerChannels[i] = make(chan [][]uint8)
		}
		for j := 0; j < p.Threads; j++ {
			go worker(p, workerHeight*j, workerHeight*(j+1), 0, p.ImageWidth, world, workerChannels[j])
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

	temp := makeMatrix(p.ImageHeight, p.ImageWidth)

	c.ioCommand <-ioInput
	world := loadPgmData(p,c, temp)

	final := world
	turn := 0
	for turn = 0 ; turn<p.Turns; turn++ {
		//implement parrelization work here and get final world order
		world = filter(p, final)
		copy(final, world)
		//c.events <- TurnComplete{turn}
	}
	// by the time that for loop ends, world has final state

	c.ioCommand <- ioOutput
	writePgmData(p,c,world)
	c.events <- FinalTurnComplete{turn, findAliveCells(p,world)}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}
	
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
