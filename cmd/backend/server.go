package main

import (
	"flag"
	"html/template"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/gorilla/websocket"
	"github.com/wukezhan/rainbow/term"
)

var addr = flag.String("addr", "0.0.0.0:2356", "http service address")

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	Subprotocols:    term.Protocols,
} // use default options

var homeTemplate *template.Template

func pty(w http.ResponseWriter, r *http.Request) {
	log.Println(r.RequestURI)
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer func() {
		log.Println("closed")
		c.Close()
	}()

	t := term.New()
	t.DockerInit("unix:///var/run/docker.sock",
		"v1.18", nil,
		map[string]string{"User-Agent": "rainbow-0.0.1"})

	u, _ := url.ParseRequestURI(r.RequestURI)
	m, _ := url.ParseQuery(u.RawQuery)

	pod := m.Get("pod")
	name := m.Get("name")
	if name == "" {
		return
	}
	role := m.Get("role")
	if role == "" {
		role = "root"
	}
	cmd := m.Get("cmd")
	if cmd == "" {
		cmd = "bash"
	}
	if pod != "" {
		containers, err := t.DockerGetK8sContainers(pod, name)
		if err != nil || len(containers) == 0 {
			return
		}
		container := containers[0]
		name = container.ID
	}
	t.User = m.Get("user")
	t.Role = role
	t.SFTP = strings.Contains(cmd, "sftp")
	ec := &types.ExecConfig{
		User:         role,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          !t.SFTP,
		Cmd:          []string{cmd},
	}
	err = t.DockerExecAttach(name, ec)

	if err == nil {
		t.Wc(&term.Wc{Conn: c})
		if !t.SFTP {
			mt, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			if mt != websocket.TextMessage {
				return
			}
			log.Println(string(data), "in")
		}

		t.Start()
	} else {
		//
	}
	c.WriteMessage(websocket.CloseMessage, []byte("\n"))
	c.Close()
}

func main() {
	log.SetFlags(log.Lshortfile)
	flag.Parse()
	http.HandleFunc("/term", pty)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
