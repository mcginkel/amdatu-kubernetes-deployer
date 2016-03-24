/*
Copyright (c) 2016 The Amdatu Foundation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cluster

import (
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"log"
	"net/http"
)

type HttpLogger struct {
	RespWriter http.ResponseWriter
	buffer     []string
}

func NewHttpLogger(responseWriter http.ResponseWriter) HttpLogger {
	return HttpLogger{responseWriter, []string{}}
}

func (logger HttpLogger) Println(v ...interface{}) {
	msg := fmt.Sprintln(v...)
	log.Println(msg)
	logger.buffer = append(logger.buffer, msg)
}

func (logger HttpLogger) Printf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Printf(msg)
	logger.buffer = append(logger.buffer, msg)
}

func (logger HttpLogger) Flush() {
	for _, msg := range logger.buffer {
		io.WriteString(logger.RespWriter, msg)
	}
}

type WebsocketLogger struct {
	Conn *websocket.Conn
}

func NewWebsocketLogger(conn *websocket.Conn) WebsocketLogger {
	return WebsocketLogger{conn}
}

func (logger WebsocketLogger) Println(v ...interface{}) {
	msg := fmt.Sprintln(v...)
	log.Println(msg)
	w, err := logger.Conn.NextWriter(websocket.TextMessage)
	if err != nil {
		log.Println(err)
		return
	}

	w.Write([]byte(msg))
	w.Close()
}

func (logger WebsocketLogger) Printf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Printf(msg)
	w, err := logger.Conn.NextWriter(websocket.TextMessage)
	if err != nil {
		log.Println(err)
		return
	}

	w.Write([]byte(msg))
}

func (logger WebsocketLogger) Flush() {
}
