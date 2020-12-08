package main

import (
	"syscall/js"
)

func sendText() js.Func {
	// interface{} can return anything and Go being like C,
	// different paths can return different values! :-(
	var sendTextFunc = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) != 1 {
			return "Invalid number of arguments"
		}

		input := args[0].String()
		go SendText(input)

		return nil
	})

	return sendTextFunc
}

func main() {
	// make sendText available in JS
	js.Global().Set("sendText", sendText())
	<-make(chan bool)
}
