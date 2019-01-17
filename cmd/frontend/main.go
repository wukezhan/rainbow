package main

import (
	"encoding/json"
	"flag"
	"html/template"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/wukezhan/rainbow/pkey"
	sess "github.com/wukezhan/rainbow/session"

	"github.com/gorilla/websocket"
	"github.com/wukezhan/rainbow/term"
)

var addr = flag.String("addr", "0.0.0.0:9999", "http service address")

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	Subprotocols:    term.Protocols,
} // use default options

var homeTemplate *template.Template

func pubkey(w http.ResponseWriter, r *http.Request) {
	bitSize := 4096

	privateKey, err := pkey.GeneratePrivateKey(bitSize)
	if err != nil {
		log.Fatal(err.Error())
	}

	publicKeyBytes, err := pkey.GeneratePublicKey(&privateKey.PublicKey)
	if err != nil {
		log.Fatal(err.Error())
	}

	privateKeyBytes := pkey.EncodePrivateKeyToPEM(privateKey)

	pk := pkey.Pkey{
		PrivateKey: string(privateKeyBytes),
		PublicKey:  string(publicKeyBytes),
	}

	pkb, err := json.Marshal(pk)
	if err != nil {
		return
	}
	w.Write(pkb)
}

func echo(w http.ResponseWriter, r *http.Request) {
	ic, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Print("upgrade:", err)
		return
	}
	defer func() {
		log.Println("ic closed")
		ic.Close()
	}()

	mt, data, err := ic.ReadMessage()
	if err != nil {
		return
	}
	if mt != websocket.TextMessage {
		return
	}

	var postData map[string]string
	json.Unmarshal(data, &postData)
	rawQuery := strings.TrimLeft(postData["Arguments"], "?")
	m, _ := url.ParseQuery(rawQuery)

	if m.Get("token") == "" {
		return
	}
	name := m.Get("name")
	user := m.Get("user")
	if user == "" {
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

	sws := &sess.WsSess{
		Ws: ic,
	}
	uid, err := strconv.Atoi(m.Get("uid"))
	ss := sess.New()
	ss.Kind = "ws"
	ss.User = sess.User{
		ID:   uid,
		Name: user,
	}
	sws.Sess = ss
	ss.UIO = sws
	if name == "" {
		ss.Relay()
	} else {
		ss.Mode = sess.TTY
		ss.TTY(m)
	}
}

func home(w http.ResponseWriter, r *http.Request) {
	uri := strings.Split(r.RequestURI, "?")
	if uri[0] == "/" {
		homeTemplate.Execute(w, "ws://"+r.Host+"/ws")
	} else {
		//static(w, r)
	}
}

func main() {
	flag.Parse()
	log.SetFlags(log.Llongfile | log.Ltime | log.LstdFlags)
	fTpl, _ := ioutil.ReadFile("./app/index.html")
	homeTemplate = template.Must(template.New("").Parse(string(fTpl)))
	http.Handle("/static/", http.StripPrefix("/", http.FileServer(http.Dir("./app/"))))
	http.HandleFunc("/ws", echo)
	http.HandleFunc("/key", pubkey)
	http.HandleFunc("/", home)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
