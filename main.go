package main

import (
	"syscall/js"
)

// interface{} can return anything and Go being like C,
// different paths can return different values! :-(
func sendText(this js.Value, args []js.Value) interface{} {
    if len(args) != 2 {
        return "Invalid number of arguments"
    }

    input := args[0].String()
    promise := args[1]
    var reject = func(err error) {
        promise.Call("reject", err.Error())
    }
    go func() {
        code := SendText(input, reject)
        promise.Call("resolve", code)
    }()
    return nil
}

func recvText(this js.Value, args []js.Value) interface{} {
    if len(args) != 2 {
        return "Invalid number of arguments"
    }

    code := args[0].String()
    promise := args[1]
    var reject = func(err error) {
        promise.Call("reject", err.Error())
    }
    go func() {
        output := RecvText(code, reject)
        promise.Call("resolve", output)
    }()
    return nil
}

func main() {
	// make sendText and recvText available in JS
	js.Global().Set("sendText", js.FuncOf(sendText))
	js.Global().Set("recvText", js.FuncOf(recvText))
	<-make(chan bool)
}
