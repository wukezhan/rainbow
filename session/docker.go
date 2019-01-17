package session

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/wukezhan/rainbow/term"
	"github.com/wukezhan/ssh"
)

// Docker .
type Docker struct {
	Sess          *Instance
	UserName      string
	RoleName      string
	ContainerName string
	PodName       string
	NodeName      string
	NodeHost      string
	NodePort      string
	Cmd           string
	sftp          bool
	WsConn        *websocket.Conn
	lock          sync.Mutex
}

// Init .
func (dc *Docker) Init(conf map[string]string) {
	log.Println("init", conf)
	dc.ContainerName = conf["ContainerName"]
	dc.UserName = conf["UserName"]
	dc.RoleName = conf["RoleName"]
	dc.PodName = conf["PodName"]
	if conf["Cmd"] != "" {
		dc.Cmd = conf["Cmd"]
	} else {
		dc.Cmd = "sh"
	}
	if strings.Contains(dc.Cmd, "sftp") {
		dc.sftp = true
	}
	if conf["PodName"] != "" {
		dc.PodName = conf["PodName"]
	} else {
		dc.PodName = ""
	}
	if conf["NodeName"] != "" {
		dc.NodeName = conf["NodeName"]
	} else {
		dc.NodeName = "localhost"
	}
	if conf["NodeHost"] != "" {
		dc.NodeHost = conf["NodeHost"]
	} else {
		dc.NodeHost = "127.0.0.1"
	}
	if conf["NodePort"] != "" {
		dc.NodePort = conf["NodePort"]
	} else {
		dc.NodePort = "2356"
	}
}

// ResizeTTY .
func (dc *Docker) ResizeTTY(win ssh.Window) error {
	rs, _ := json.Marshal(win)
	return dc._write(term.ResizeTerminal, rs)
}

// WriteWebtty .
func (dc *Docker) WriteWebtty(data []byte) (int, error) {
	dc.lock.Lock()
	defer dc.lock.Unlock()
	if dc.WsConn == nil {
		return 0, errors.New("dc.WsConn is nil")
	}
	return 0, dc.WsConn.WriteMessage(websocket.TextMessage, data)
}

// Write .
func (dc *Docker) _write(t int, data []byte) error {
	dc.lock.Lock()
	defer dc.lock.Unlock()
	msg := []byte{byte(t)}
	return dc.WsConn.WriteMessage(websocket.TextMessage, append(msg, data...))
}

// Ping .
func (dc *Docker) Ping() (err error) {
	dc.lock.Lock()
	defer dc.lock.Unlock()
	msg := []byte{term.Ping}
	return dc.WsConn.WriteMessage(websocket.TextMessage, msg)
}

// Write .
func (dc *Docker) Write(data []byte) (int, error) {
	return 0, dc._write(term.Input, data)
}

// Read .
func (dc *Docker) Read() (mt int, p []byte, err error) {
	return dc.WsConn.ReadMessage()
}

// Close .
func (dc *Docker) Close() (err error) {
	dc.lock.Lock()
	defer dc.lock.Unlock()
	if dc.WsConn != nil {
		err = dc.WsConn.Close()
		dc.WsConn = nil
	}
	dc.Sess.BIO = nil
	return nil
}

// WritePipe .
func (dc *Docker) WritePipe() (err error) {
	buf := make([]byte, 1024)
	uio := dc.Sess.UIO
	uioKind := uio.Kind()
	for {
		n, err := uio.Read(buf)
		if err != nil {
			log.Println("exited", err)
			return err
		}

		// read from ssh, we need parse the raw payload
		//log.Println("writing to bio", dc.Sess.Mode, SFTP)
		if dc.Sess.Mode != SFTP {
			err = dc._write(term.Input, buf[:n])
		} else if uioKind == "ws" || dc.sftp {
			_, err = dc.WriteWebtty(buf[:n])
			//log.Println("writing to bio", n, (buf[:n]), err)
		}
		// 此处无问题
		if err != nil {
			return err
		}
	}
}

// sendEOL .
func (dc *Docker) sendEOL() (err error) {
	return dc._write(term.Input, []byte("\n"))
}

// Dial .
func (dc *Docker) Dial() (err error) {
	query := "pod=" + dc.PodName + "&name=" + dc.ContainerName + "&user=" + dc.UserName + "&role=" + dc.RoleName + "&cmd=" + dc.Cmd
	u := url.URL{Scheme: "ws", Host: dc.NodeHost + ":" + dc.NodePort, Path: "/term", RawQuery: query}
	var r *http.Response
	dc.WsConn, r, err = websocket.DefaultDialer.Dial(u.String(), nil)
	log.Println("proxy to", u.String(), err)
	if err != nil {
		log.Println("connect to backend error!", r)
	}

	return
}

// Running .
func (dc *Docker) Running() bool {
	return dc.WsConn != nil
}

// IsTTY .
func (dc *Docker) IsTTY() bool {
	return !dc.sftp
}

// Kind .
func (dc *Docker) Kind() string {
	return "docker"
}
