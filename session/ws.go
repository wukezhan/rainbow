package session

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/wukezhan/rainbow/term"
	"github.com/wukezhan/ssh"
)

// WsSess .
type WsSess struct {
	Ws       *websocket.Conn
	Sess     *Instance
	p        []byte
	l        int
	lock     sync.Mutex
	bioReady bool
}

// Write used to convert plain text to webtty
func (ws *WsSess) Write(b []byte) (int, error) {
	//log.Println("UIO writing")
	ws.lock.Lock()
	defer ws.lock.Unlock()
	writer, err := ws.Ws.NextWriter(websocket.TextMessage)
	if err != nil {
		return 0, err
	}
	defer writer.Close()
	safeMessage := base64.StdEncoding.EncodeToString(b)
	return writer.Write(append([]byte{term.Output}, []byte(safeMessage)...))
}

// WriteWebtty .
func (ws *WsSess) WriteWebtty(b []byte) (n int, err error) {
	ws.lock.Lock()
	defer ws.lock.Unlock()
	if ws.Ws == nil {
		return 0, errors.New("ws is nil")
	}
	err = ws.Ws.WriteMessage(websocket.TextMessage, b)
	return
}

// WriteString .
func (ws *WsSess) WriteString(str string) (err error) {
	//log.Println("wstr", str)
	_, err = ws.Write([]byte(str))
	return
}

func (ws *WsSess) Read(buf []byte) (n int, err error) {
	lb := len(buf)
	lp := len(ws.p)
	if ws.l < lp {
		m := lp
		if lp-ws.l > lb {
			m = ws.l + lb
		}
		copy(buf, ws.p[ws.l:m])
		n = m - ws.l
		ws.l = m
		return
	}
	for {
		_, p, e := ws.Ws.ReadMessage()
		if e != nil {
			err = e
			return
		}
		if p[0] == term.ResizeTerminal {
			var args struct {
				Width  int `json:"columns"`
				Height int `json:"rows"`
			}
			err := json.Unmarshal(p[1:], &args)
			if err == nil {
				log.Println("win", args, string(p[1:]))
				ws.Sess.Winch <- ssh.Window{
					Width:  args.Width,
					Height: args.Height,
				}
			}
			continue
		} else if p[0] == term.Ping {
			if ws.Sess.BIO != nil && ws.Sess.BIO.Kind() == "docker" {
				log.Println("ping")
				ws.Sess.BIO.Ping()
			}
			continue
		}
		if ws.Sess.BIO != nil {
			log.Println("loop write webtty")
			ws.Sess.BIO.WriteWebtty(p)
			continue
		}

		ws.p = p
		ws.l = 1
		return ws.Read(buf)
	}
}

// WritePipe .
func (ws *WsSess) WritePipe() (err error) {
	defer func() {
		ws.Sess.Mode = Relay
	}()
	ctx, cf := context.WithCancel(context.TODO())
	defer cf()
	if ws.Sess.Mode == TTY {
		go func() {
			sess := ws.Sess
			defer log.Println("winch exit")
			for {
				select {
				case win := <-sess.Winch:
					sess.win.Width = win.Width
					sess.win.Height = win.Height
					log.Println("winch!", win)
					if ws.bioReady {
						err = sess.BIO.ResizeTTY(win)
					}
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	var p []byte
	bio := ws.Sess.BIO
	bioKind := bio.Kind()
	for {
		if bioKind == "docker" {
			_, p, err = bio.Read()
			if err != nil {
				//log.Println("exited", err)
				return
			}
			if !ws.bioReady {
				// 必须在 exec attached 之后才能 resize，否则可能触发 no such exec 错误
				ws.bioReady = true
				bio.ResizeTTY(ws.Sess.win)
			}
			log.Println("ws write", p)
			_, err = ws.WriteWebtty(p)
			if err != nil {
				//log.Println("ws write error", err)
				return
			}
		}
	}
}

// ReadPipe .
func (ws *WsSess) ReadPipe() (err error) { return }

// Close .
func (ws *WsSess) Close() (err error) {
	if ws.Ws != nil {
		ws.Ws.Close()
	}
	return
}

// Kind .
func (ws *WsSess) Kind() string {
	return "ws"
}
