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

func recvText() js.Func {
	var recvTextFunc = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) != 1 {
			return "Invalid number of arguments"
		}

		code := args[0].String()
		var output string
		go func() {
			output = RecvText(code)
		}()

		return output
	})

	return recvTextFunc
}

func main() {
	// make sendText and recvText available in JS
	js.Global().Set("sendText", sendText())
	js.Global().Set("recvText", recvText())
	<-make(chan bool)
}
