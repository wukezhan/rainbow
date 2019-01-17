package session

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wukezhan/rainbow/api"
	"github.com/wukezhan/readline"
	"github.com/wukezhan/ssh"

	color "github.com/logrusorgru/aurora"
)

// UIO .
type UIO interface {
	// Write .
	Write(b []byte) (n int, err error)
	WriteWebtty(b []byte) (n int, err error)
	// WriteString .
	WriteString(str string) (err error)
	// Read .
	Read(buf []byte) (n int, err error)
	// Close .
	WritePipe() (err error)
	Close() (err error)
	Kind() string
}

// BIO .
type BIO interface {
	Init(conf map[string]string)
	// Write .
	Ping() (err error)
	Write(b []byte) (n int, err error)
	//WritePlain(t int, b []byte) (n int, err error)
	WriteWebtty(b []byte) (n int, err error)
	// WriteString .
	//WriteString(str string) (err error)
	// Read .
	Read() (n int, p []byte, err error)
	// Close .
	ResizeTTY(win ssh.Window) (err error)
	WritePipe() (err error)
	Dial() (err error)
	Close() (err error)
	Running() bool
	IsTTY() bool
	Kind() string
}

// User .
type User struct {
	ID   int
	Name string
	Mail string
}

// Instance .
type Instance struct {
	Kind  string
	User  User
	ri    *readline.Instance
	Winch chan ssh.Window
	win   ssh.Window
	pc    *readline.PrefixCompleter
	UIO   UIO
	BIO   BIO
	ulock sync.Mutex
	block sync.Mutex
	Mode  int
}

//
const (
	Init     = 0
	Relay    = 1
	TTY      = 2
	RelayTTY = 3
	SFTP     = 4
)

const _clear = "\x1b[H\x1b[J"

// New .
func New() *Instance {
	sess := Instance{
		Winch: make(chan ssh.Window, 1),
	}

	return &sess
}

func filterInput(r rune) (rune, bool) {
	switch r {
	// block CtrlZ feature
	case readline.CharCtrlZ:
		return r, false
	}
	return r, true
}

func noraw() func() error {
	return func() error { return nil }
}

// SetPrompt .
func (sess *Instance) SetPrompt() {
	sess.ri.SetPrompt(fmt.Sprintf("%s@%s %s ",
		color.Magenta(sess.User.Name).Bold(),
		color.Blue("recloud").Bold(),
		color.Brown("➤").Bold()))
	sess.ri.Operation.ForceRefresh()
}

// Exit .
func (sess *Instance) Exit() {
	//defer log.Println("ss exited")
	sess.CloseBIO()
	sess.CloseUIO()
}

// ShowUsage .
func (sess *Instance) ShowUsage() {
	sess.UIO.WriteString("\rcommands:\r\n")
	sess.UIO.WriteString(sess.pc.Tree("    "))
	sess.UIO.WriteString("\r\n")
}

// Completer .
func (sess *Instance) Completer() *readline.PrefixCompleter {
	return readline.NewPrefixCompleter(
		readline.PcItem("help"),
		readline.PcItem("list",
			readline.PcItem("pod"),
			/*readline.PcItem("node"),
			readline.PcItem("group"),*/
		),
		/*readline.PcItem("goto",
			readline.PcItemDynamic(func(s string) []string {
				return []string{"hello"}
				//return []string{ss.s.User() + ".test", ss.s.User() + ".demo"}
			}),
		),*/
		readline.PcItem("exit"),
	)
}

//CloseBIO .
func (sess *Instance) CloseBIO() {
	log.Println("bio closed")
	sess.block.Lock()
	defer sess.block.Unlock()
	if sess.BIO != nil {
		sess.BIO.Close()
	}
	if sess.ri != nil {
		sess.ri.Terminal.PipeWrite = nil
		sess.SetPrompt()
	}
}

//CloseUIO .
func (sess *Instance) CloseUIO() {
	//log.Println("uio closed")
	sess.UIO.Close()
}

//TTY .
func (sess *Instance) TTY(args url.Values) {
	var err error
	sess.BIO = &Docker{
		Sess: sess,
	}
	host := args.Get("host")
	if host == "" {
		host = "localhost"
	}
	sess.BIO.Init(map[string]string{
		"UserName":      sess.User.Name,
		"RoleName":      "root",
		"ContainerName": args.Get("name"),
		"PodName":       args.Get("pod"), // pass
		"NodeName":      host,            // pass
		"NodeHost":      host,            // pass
		"Cmd":           args.Get("cmd"), // config
	})
	err = sess.BIO.Dial()
	if err != nil {
		sess.UIO.Write([]byte("\rlogin error: " + err.Error() + "\r\n"))
		sess.BIO.Close()
		return
	}
	name := args.Get("name") + "@" + host
	if args.Get("pod") != "" {
		name = args.Get("pod") + ":" + name
	}
	sess.UIO.Write([]byte("\rlogin to " + name + "\r\n"))

	sess.BIO.Write([]byte("\n"))
	if sess.ri == nil {
		errs := make(chan error, 2)
		go func() {
			errs <- func() error {
				defer func() {
					//log.Println("backend -> user close")
					sess.CloseBIO()
				}()
				return sess.UIO.WritePipe()
			}()
		}()
		go func() {
			errs <- func() error {
				defer func() {
					//log.Println("backend -> user close")
					sess.CloseBIO()
				}()
				return sess.BIO.WritePipe()
			}()
		}()
		err := <-errs
		log.Println("err", err)
	} else {
		go func() {
			defer func() {
				//log.Println("backend -> user close")
				sess.CloseBIO()
			}()
			err := sess.UIO.WritePipe()
			log.Println("err", err)
		}()
		sess.ri.SetPrompt("")
		sess.ri.Terminal.PipeWrite = func(r *bufio.Reader) ([]byte, error) {
			//sess.BIO.Write([]byte(string(r)))
			p := make([]byte, 1024)
			for {
				n, err := r.Read(p)
				if err != nil {
					return p[:n], err
				}
				if sess.BIO != nil {
					sess.BIO.Write(p[:n])
				} else {
					return p[:n], nil
				}
			}
		}
	}
}

// Relay .
func (sess *Instance) Relay() {
	sess.pc = sess.Completer()
	config := &readline.Config{
		AutoComplete:        sess.pc,
		InterruptPrompt:     "^C",
		EOFPrompt:           "exit",
		HistorySearchFold:   true,
		FuncFilterInputRune: filterInput,
		Stdin:               sess.UIO,
		Stdout:              sess.UIO,
		Stderr:              sess.UIO,
		FuncMakeRaw:         noraw(),
		FuncExitRaw:         noraw(),
		ForceUseInteractive: true,
	}
	config.FuncGetWidth = func() func() int {
		return func() int {
			//log.Println("return width", sess.win.Width)
			return sess.win.Width
		}
	}()
	ctx, cf := context.WithCancel(context.TODO())
	config.FuncOnWidthChanged = func(winCb func()) {
		go func() {
			//defer log.Println("winch exit")
			for {
				select {
				case win := <-sess.Winch:
					sess.win.Width = win.Width
					sess.win.Height = win.Height
					winCb()
					//log.Println("winch!", win)
					sess.block.Lock()
					if sess.BIO != nil {
						sess.BIO.ResizeTTY(win)
					}
					sess.block.Unlock()
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	l, err := readline.NewEx(config)
	if err != nil {
		panic(err)
	}
	defer l.Close()
	sess.ri = l

	l.Write([]byte(fmt.Sprintf(
		"\r\nwelcome, %s!\r\n\r\n",
		color.Magenta(sess.User.Name).Bold().String(),
	)))
	l.Write([]byte(color.Green("# type `help` to get started!\r\n").String()))
	sess.SetPrompt()
	var line string
	var ucs []api.UserContainer
	ra := api.New()
	err, ucs = ra.GetContainers(sess.User.Name)
	for {
		line, err = l.Readline()
		if err == readline.ErrInterrupt {
			if len(line) == 0 {
				break
			} else {
				continue
			}
		} else if err == io.EOF {
			break
		}

		line = strings.TrimSpace(line)
		//log.Println("line:", line)
		if len(ucs) > 0 {
			m, e := regexp.MatchString("^((\\d+)|(\\d+)\\.(\\d+))$", strings.Trim(line, " "))
			//log.Println("match?", m, e)
			if e == nil && m {
				sess.Mode = RelayTTY
				is := strings.SplitN(strings.Trim(line, " "), ".", 2)
				idx, e := strconv.Atoi(is[0])
				if e != nil {
					log.Println("id error:", e.Error())
					l.Write([]byte("\rinvalid id\n"))
					continue
				}
				idx2 := 0
				if len(is) > 1 {
					idx2, e = strconv.Atoi(is[1])
					if e != nil {
						l.Write([]byte("\rinvalid id\r\n"))
						continue
					}
				}
				if e != nil || idx > len(ucs) || idx < 1 {
					l.Write([]byte("\rinvalid id\r\n"))
					continue
				}
				uc := ucs[idx-1]
				if idx2 >= len(uc.Containers) {
					l.Write([]byte("\rinvalid id\r\n"))
					continue
				}
				name := uc.Containers[idx2]
				if uc.PodName != "" {
					name = name + "_" + uc.PodName
				}
				params := url.Values{
					"host": []string{uc.NodeName},
					"pod":  []string{uc.PodName},
					"name": []string{uc.Containers[idx2]},
					"cmd":  []string{"bash"},
				}
				log.Println(uc)
				sess.TTY(params)
				//l.Write([]byte("\rgoto " + name + "@" + uc.NodeName + "\r\n"))
				continue
			}
		}
		switch {
		case line == "login":
			pswd, err := l.ReadPassword("please enter your password: ")
			if err != nil {
				break
			}
			l.Write([]byte("you enter:" + strconv.Quote(string(pswd)+"\r\n")))
			sess.SetPrompt()
		case line == "help":
			sess.ShowUsage()
		case strings.HasPrefix(line, "setprompt"):
			if len(line) <= 10 {
				log.Println("setprompt <prompt>")
				break
			}
			l.SetPrompt(line[10:])
		case strings.HasPrefix(line, "list"):
			if strings.Contains(line, "group") {
			} else if strings.Contains(line, "node") {
			} else {
				l.Write([]byte("\r"))
				err, ucs = ra.GetContainers(sess.User.Name)
				if err == nil {
					ul := len(ucs)
					i := 0
					for i < ul {
						uc := ucs[i]
						i++
						if len(uc.Containers) > 0 {
							l.Write([]byte(fmt.Sprintf(
								"\r%s) %s@%s 🐳\n",
								color.Green(i).Bold().String(),
								color.Magenta(uc.PodName).Bold(),
								color.Blue(uc.NodeName).Bold(),
							)))
							for idx := 0; idx < len(uc.Containers); idx++ {
								name := uc.Containers[idx]
								l.Write([]byte(fmt.Sprintf(
									"\r    %s.%s) %s\n",
									color.Green(i).Bold().String(),
									color.Green(idx).Bold().String(),
									color.Red(name).String(),
								)))
							}
						} else {
							/*name := uc.Containers[0]
							if uc.PodName != "" {
								name = name + "~" + uc.PodName
							}
							l.Write([]byte(fmt.Sprintf(
								"\r%s) %s@%s\n", //  💎
								color.Green(i).Bold().String(),
								color.Magenta(name).Bold(),
								color.Blue(uc.NodeName).Bold(),
							)))*/
						}
						//l.Write([]byte(uc.PodName + ":" + uc.Containers[0] + "\n"))
					}
				}
			}
		case strings.HasPrefix(line, "goto"):
			//l.Write([]byte(_clear + "\n"))
			sess.Mode = RelayTTY
			sess.TTY(url.Values{
				"name": []string{line[5:]},
				"cmd":  []string{"bash"}, // @TODO pass by params
			})
		case line == "clear":
			l.Write([]byte(_clear))
			l.Operation.ForceRefresh()
		case line == "exit":
			l.Write([]byte("\r"))
			goto exit
		case line == "sleep":
			//log.Println("sleep 4 second")
			time.Sleep(4 * time.Second)
		case line == "":
		default:
			l.Write([]byte("\r>>>> " + strconv.Quote(line) + "\n"))
			//log.Println("n, err")
		}
	}
exit:
	l.Clean()
	cf()
	sess.Exit()
}

// SFTP .
func (sess *Instance) SFTP() {
	//sess.docker.ContainerName = sess.User.Name + ".data." + sess.docker.NodeName
	sess.Mode = SFTP
	var err error
	sess.BIO = &Docker{
		Sess: sess,
	}
	sess.BIO.Init(map[string]string{
		"UserName":      "root",
		"PodName":       sess.User.Name,
		"ContainerName": "data",
		"NodeName":      "dx-dev-test176",           // @TODO pass
		"NodeHost":      "dx-dev-test176",           // @TODO pass
		"Cmd":           "/usr/lib/ssh/sftp-server", // config
	})
	err = sess.BIO.Dial()
	if err != nil {
		sess.UIO.Write([]byte("error:" + err.Error()))
		sess.CloseUIO()
		return
	}

	errs := make(chan error, 2)
	go func() {
		errs <- func() error {
			defer func() {
				//log.Println("backend -> user close")
				sess.CloseBIO()
			}()
			return sess.UIO.WritePipe()
		}()
	}()
	go func() {
		errs <- func() error {
			defer func() {
				//log.Println("backend -> user close")
				sess.CloseBIO()
			}()
			return sess.BIO.WritePipe()
		}()
	}()
	<-errs
}
