package main

import (
	"log"

	"poker_bank/internal/ep"
)

func main() {
	if err := ep.Run(); err != nil {
		log.Fatal(err)
	}
}
