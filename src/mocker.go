package main

import (
	"fmt"
	"math/rand"
	"os"
	"time"

	"mocker"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println("FAIL.", r)
		}
	}()

	rand.Seed(time.Now().UTC().UnixNano())
	if len(os.Args) < 2 {
		panic("Not enought args. Use 'mocker help' for detailed info")
	}
	mocker := mocker.InitMocker()
	if command, ok := mocker.Commands[os.Args[1]]; ok {
		command(os.Args[2:])
	} else {
		panic("Unknown command")
	}
}
