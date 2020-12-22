package main

import (
	"syscall/js"

	"github.com/psanford/wormhole-william/wasm"
)

//type exportMap map[string]js.Func
//
//var exportRegistry exportMap
//
//func init() {
//	exportRegistry = make(exportMap)
//	exportRegistry["newWormholeClient"] = js.FuncOf(wasm.NewClient)
//	exportRegistry["client_sendText"] = js.FuncOf(wasm.Client_SendText)
//	exportRegistry["client_recvText"] = js.FuncOf(wasm.Client_RecvText)
//	exportRegistry["client_free"] = js.FuncOf(wasm.Client_free)
//}

//func ObjectSet(_ js.Value, args []js.Value) interface{} {
//	assignee := args[0]
//	exportName := args[1].String()
//	propertyName := args[2].String()
//	exportFunc := exportRegistry[exportName]
//
//	assignee.Set(propertyName, exportFunc)
//	return nil
//}

func main() {
	// make things available in JS global scope.
	//js.Global().Set("ObjectSet", js.FuncOf(ObjectSet))

	js.Global().Set("newWormholeClient", js.FuncOf(wasm.NewClient))
	js.Global().Set("client_free", js.FuncOf(wasm.Client_free))
	js.Global().Set("client_sendText", js.FuncOf(wasm.Client_SendText))
	js.Global().Set("client_recvText", js.FuncOf(wasm.Client_RecvText))

	// block to keep the wasm module API available
	// (see: )
	<-make(chan bool)
}
