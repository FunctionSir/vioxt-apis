/*
 * @Author: FunctionSir
 * @License: AGPLv3
 * @Date: 2026-05-25 21:39:54
 * @LastEditTime: 2026-05-25 22:39:04
 * @LastEditors: FunctionSir
 * @Description: Fortune.
 * @FilePath: /fortune/main.go
 */

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"time"
)

const FortuneChanBufferSize int = 128
const CountOfFortuneMakers int = 4

var FortuneChan = make(chan string, FortuneChanBufferSize)

func FortuneGenerator() {
	for {
		cmd := exec.Command("fortune")
		fortune, err := cmd.Output()
		if err != nil {
			log.Println("Can not generate new fortune:", err)
			continue
		}
		FortuneChan <- string(fortune)
	}
}

func FortuneHandler(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	defer func() { log.Println("Request", r.URL.String(), "processed in", time.Since(now)) }()
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(<-FortuneChan))
}

func main() {
	fmt.Println("Fortune API Backend")
	fmt.Println("Version: 0.1.0")
	if len(os.Args) <= 1 {
		panic("no enough args")
	}
	for range CountOfFortuneMakers {
		go FortuneGenerator()
	}
	http.HandleFunc("GET /", FortuneHandler)
	err := http.ListenAndServe(os.Args[1], nil)
	panic(err)
}
