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

func calculateNextState(p Params, world [][]byte) [][]byte {

	newWorld := make([][]byte, p.ImageHeight)
	for i := range newWorld {
		newWorld[i] = make([]byte, p.ImageWidth)
	}
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
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

// distributor divides the work between workers and interacts with other goroutines.
func distributor(p Params, c distributorChannels) {

	world := make([][]byte, p.ImageHeight)
	for i := range world {
		world[i] = make([]byte, p.ImageWidth)
	}

	c.ioCommand <-ioInput
	c.ioFilename <- strconv.Itoa(p.ImageWidth)+"x"+strconv.Itoa(p.ImageHeight)
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
			world[col][row] = <- c.ioInput
		}
	}

	final := world

	//Currently having issues with memory being overwriiten
	turn := 0
	for turn = 0 ; turn<p.Turns; turn++ {
		world = calculateNextState(p, final)
		copy(final, world)
		c.events <- TurnComplete{turn}
	}

	c.ioCommand <- ioOutput
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

	var a []util.Cell
	for col := 0; col < p.ImageHeight; col++ {
		for row := 0; row < p.ImageWidth; row++ {
			if world[col][row] == 255 {
				a = append(a, util.Cell{X: row, Y: col})
			}
		}
	}
	c.events <- FinalTurnComplete{turn, a}

	// Make sure that the Io has finished any output before exiting.
	c.ioCommand <- ioCheckIdle
	<-c.ioIdle

	c.events <- StateChange{turn, Quitting}
	
	// Close the channel to stop the SDL goroutine gracefully. Removing may cause deadlock.
	close(c.events)
}
