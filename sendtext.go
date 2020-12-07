package main

import (
	"fmt"
	"strings"
	"log"
	"context"
	
	"github.com/psanford/wormhole-william/wormhole"
)

func SendText(message string) {
	c := newClient()

	var msg string
	msg = strings.TrimSpace(message)

	ctx := context.Background()
	codeFlag := ""
  	code, status, err := c.SendText(ctx, msg, wormhole.WithCode(codeFlag))
	if err != nil {
		log.Fatal(err)
	}

	printInstructions(code)

	s := <-status

	if s.Error != nil {
		log.Fatalf("Send error: %s", s.Error)
	} else if s.OK {
		fmt.Println("text message sent")
	} else {
		log.Fatalf("Hmm not ok but also not error")
	}
}

func newClient() wormhole.Client {
	c := wormhole.Client{
		RendezvousURL:             "", //relayURL,
		PassPhraseComponentLength: 2, //codeLen,
	}

	return c
}

func printInstructions(code string) {
	mwCmd := "wormhole receive"
	wwCmd := "wormhole-william recv"

	fmt.Printf("On the other computer, please run: %s (or %s)\n", mwCmd, wwCmd)
	fmt.Printf("Wormhole code is: %s\n", code)
}
