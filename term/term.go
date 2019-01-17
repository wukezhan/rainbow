package term

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"syscall"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/gorilla/websocket"
)

// ExecConfig ...
type ExecConfig struct {
	types.ExecConfig
}

// DockerTty .
type DockerTty struct {
	User   string
	Role   string
	ID     string
	cli    *client.Client
	Hr     types.HijackedResponse
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer

	Kind     int
	SFTP     bool
	Writable bool
	Cols     int64
	Rows     int64

	wc *Wc

	Ctx context.Context
	Cf  context.CancelFunc
}

// Wc .
type Wc struct {
	Conn *websocket.Conn
}

// ResizeOption .
type ResizeOption struct {
	Width  int64
	Height int64
}

// Protocols string
var Protocols = []string{"webtty"}

const (
	// UnknownInput Unknown message type, maybe sent by a bug
	UnknownInput = '0'
	// Input User input typically from a keyboard
	Input = '1'
	// Ping to the server
	Ping = '2'
	// ResizeTerminal Notify that the browser size has been changed
	ResizeTerminal = '3'
)

const (
	// UnknownOutput unknown message type, maybe set by a bug
	UnknownOutput = '0'
	// Output Normal output to the terminal
	Output = '1'
	// Pong to the browser
	Pong = '2'
	// SetWindowTitle Set window title of the terminal
	SetWindowTitle = '3'
	// SetPreferences Set terminal preference
	SetPreferences = '4'
	// SetReconnect Make terminal to reconnect
	SetReconnect = '5'
)

func (wc *Wc) Read(p []byte) (n int, err error) {
	for {
		msgType, reader, err := wc.Conn.NextReader()
		if err != nil {
			return 0, err
		}

		if msgType != websocket.TextMessage {
			continue
		}

		return reader.Read(p)
	}
}

func (wc *Wc) read3() (mt int, p []byte, err error) {
	return wc.Conn.ReadMessage()
}

func (wc *Wc) Write(data []byte) (int, error) {
	writer, err := wc.Conn.NextWriter(websocket.TextMessage)
	if err != nil {
		return 0, err
	}
	defer writer.Close()
	return writer.Write(data)
}

// New returns a DockerTty
func New() *DockerTty {
	tty := &DockerTty{
		Writable: true,
	}
	tty.Ctx, tty.Cf = context.WithCancel(context.Background())

	return tty
}

// Close close the DockerTty
func (tty *DockerTty) Close() {
	tty.Cf()
}

// Stdio .
func (tty *DockerTty) Stdio(stdin io.Reader, stdout, stderr io.Writer) {
	if stdin != nil {
		tty.Stdin = stdin
	}
	if stdout != nil {
		tty.Stdout = stdout
	}
	if stderr != nil {
		tty.Stderr = stderr
	}
}

// Wc ..
func (tty *DockerTty) Wc(wc *Wc) *DockerTty {
	tty.wc = wc
	tty.Kind = 1
	return tty
}

// Wc ..
func (tty *DockerTty) wsHrRead(data []byte) error {
	//log.Println("docker responsed", data)
	safeMessage := base64.StdEncoding.EncodeToString(data)
	_, err := tty.wc.Write(append([]byte{Output}, []byte(safeMessage)...))
	if err != nil {
		log.Println("err", err)
		return err
	}

	return nil
}

func (tty *DockerTty) wsWcRead(data []byte) error {
	if len(data) == 0 {
		return errors.New("unexpected zero length read from master")
	}

	switch data[0] {
	case Input: // input
		if !tty.Writable {
			return nil
		}

		if len(data) <= 1 {
			return nil
		}

		_, err := tty.Hr.Conn.Write(data[1:])
		if err != nil {
			//log.Println("read", (data), err.Error())
			return err // errors.Wrapf(err, "failed to write received data to slave")
		}

	case Ping: //Ping
		_, err := tty.wc.Write([]byte{Pong})
		if err != nil {
			return err //errors.Wrapf(err, "failed to return Pong message to master")
		}

	case ResizeTerminal: //
		if tty.Cols != 0 && tty.Rows != 0 {
			break
		}

		if len(data) <= 1 {
			return errors.New("received malformed remote command for terminal resize: empty payload")
		}

		var args ResizeOption
		err := json.Unmarshal(data[1:], &args)
		if err != nil {
			log.Println("resize", err.Error())
			return err //errors.Wrapf(err, "received malformed data for terminal resize")
		}
		rows := tty.Rows
		if rows == 0 {
			rows = int64(args.Height)
		}

		columns := tty.Cols
		if columns == 0 {
			columns = int64(args.Width)
		}

		log.Println("resize", columns, rows)
		err = tty.DockerExecResize(columns, rows)
		log.Println(err)
	default:
		return errors.New("unknown message type `" + string(data[0]) + "`")
	}

	return nil
}

func (tty *DockerTty) sendInitMessage() error {
	windowTitle := []byte("hello world")
	_, err := tty.wc.Write(append([]byte{SetWindowTitle}, windowTitle...))
	if err != nil {
		return err //errors.Wrapf(err, "failed to send window title")
	}

	if 0 /*wt.reconnect*/ > 0 {
		reconnect, _ := json.Marshal(1)
		_, err := tty.wc.Write(append([]byte{SetReconnect}, reconnect...))
		if err != nil {
			return err //errors.Wrapf(err, "failed to set reconnect")
		}
	}

	/*if wt.masterPrefs != nil {
		err := wt.masterWrite(append([]byte{SetPreferences}, wt.masterPrefs...))
		if err != nil {
			return errors.Wrapf(err, "failed to set preferences")
		}
	}*/

	return nil
}

// ttyStart .
func (tty *DockerTty) ttyStart() error {
	//tty.sendInitMessage()

	errs := make(chan error, 2)

	defer func() {
		log.Println("wc exited")
		err := tty.Hr.CloseWrite()
		log.Println("close write", err)
		tty.Hr.Close()
	}()

	go func() {
		errs <- func() error {
			var err error
			var n int
			defer log.Println("user", tty.User, "docker -> ws close", err)
			buffer := make([]byte, 1024)
			for {
				n, err = tty.Hr.Reader.Read(buffer)
				if err != nil {
					return err
				}

				err = tty.wsHrRead(buffer[:n])
				if err != nil {
					return err
				}
			}
		}()
	}()

	go func() {
		errs <- func() error {
			var err error
			var mt int
			var p []byte
			defer log.Println("user", tty.User, "ws -> docker close", err)
			for {
				//log.Println("ws in")
				mt, p, err = tty.wc.read3()
				if err != nil {
					log.Println("read error", mt, err.Error())
					return err
				}

				err = tty.wsWcRead(p)
				if err != nil {
					return err
				}
			}
		}()
	}()

	var err error
	select {
	case <-tty.Ctx.Done():
		err = tty.Ctx.Err()
		return err
	case err = <-errs:
		log.Println("wc end", err)
		return err
	}
}

// sftpStart .
func (tty *DockerTty) sftpStart() error {
	errs := make(chan error, 2)

	defer func() {
		log.Println("wc exited")
		err := tty.Hr.CloseWrite()
		log.Println("close write", err)
		tty.Hr.Close()
	}()

	go func() {
		errs <- func() error {
			var err error
			var n int
			defer log.Println("user", tty.User, "docker -> ws close", err)
			buffer := make([]byte, 1024)
			var pbuf []byte
			var bl int
			for {
				pl := len(pbuf)
				if pl < 8 {
					n, err = tty.Hr.Reader.Read(buffer)
					if err != nil {
						log.Println("err", err)
						return err
					}
					if len(pbuf) > 0 {
						pbuf = append(pbuf, buffer[:n]...)
					} else {
						pbuf = buffer[:n]
					}
				}
				if bl == 0 {
					bl = int(binary.BigEndian.Uint32(pbuf[4:8]))
					pbuf = pbuf[8:]
				}
				pl = len(pbuf)
				if pl > bl {
					n, err = tty.wc.Write(pbuf[:bl])
					pbuf = pbuf[bl:]
					bl = 0
				} else {
					n, err = tty.wc.Write(pbuf[:pl])
					pbuf = make([]byte, 0)
					bl -= pl
				}
				if err != nil {
					return err
				}
			}
		}()
	}()

	go func() {
		errs <- func() error {
			var err error
			var mt int
			var p []byte
			defer log.Println("user", tty.User, "ws -> docker close", err)
			for {
				mt, p, err = tty.wc.Conn.ReadMessage()
				//log.Println("read from ws", n, buffer[:n], err)
				if err != nil {
					log.Println("read error", err.Error())
					return err
				}

				if mt != websocket.TextMessage {
					continue
				}

				_, err = tty.Hr.Conn.Write(p)
				//log.Println("write to docker", n, err)
				if err != nil {
					return err
				}
			}
		}()
	}()

	var err error
	select {
	case <-tty.Ctx.Done():
		err = tty.Ctx.Err()
		return err
	case err = <-errs:
		log.Println("wc end", err)
		return err
	}
}

// InitMessage .
type InitMessage struct {
	Arguments string `json:"Arguments,omitempty"`
	AuthToken string `json:"AuthToken,omitempty"`
}

// Start .
func (tty *DockerTty) Start() {
	if tty.Kind == 1 {
		var err error
		if tty.SFTP {
			err = tty.sftpStart()
			//err = tty.ttyStart()
		} else {
			err = tty.ttyStart()
		}
		log.Println("error", err)
		resp, err := tty.cli.ContainerExecInspect(tty.Ctx, tty.ID)
		if err != nil {
			// If we can't connect, then the daemon probably died.
			log.Println(err)
		}
		if resp.Running {
			err = syscall.Kill(resp.Pid, syscall.SIGKILL)
			log.Println("kill", resp.Pid, err)
		}
		resp, err = tty.cli.ContainerExecInspect(tty.Ctx, tty.ID)
		if err != nil {
			// If we can't connect, then the daemon probably died.
			log.Println(err)
		}
		tty.Cf()
	} else {
		// other kind
	}

	<-tty.Ctx.Done()
}

// DockerInit .
func (tty *DockerTty) DockerInit(host string, version string, httpClient *http.Client, httpHeaders map[string]string) error {
	var err error
	tty.cli, err = client.NewClient(host, version, httpClient, httpHeaders)
	return err
}

// DockerGetK8sContainers .
func (tty *DockerTty) DockerGetK8sContainers(pod, container string) ([]types.Container, error) {
	args := filters.NewArgs()
	args.Add("name", container+"_"+pod)
	return tty.cli.ContainerList(tty.Ctx, types.ContainerListOptions{
		Filters: args,
	})
}

// DockerExecAttach .
func (tty *DockerTty) DockerExecAttach(name string, ec *types.ExecConfig) error {
	execID, cerr := tty.cli.ContainerExecCreate(tty.Ctx, name, *ec)
	if cerr != nil {
		return cerr
	}
	tty.ID = execID.ID
	esc := types.ExecStartCheck{
		Detach: ec.Detach,
		Tty:    ec.Tty,
	}
	log.Println("DockerExecAttach", tty.ID)
	var aerr error
	tty.Hr, aerr = tty.cli.ContainerExecAttach(tty.Ctx, execID.ID, esc)
	if aerr != nil {
		log.Println("exec", aerr.Error())
		return aerr
	}
	return nil
}

// DockerExecResize .
func (tty *DockerTty) DockerExecResize(w, h int64) error {
	var err error
	log.Println("DockerExecResize", tty.ID)
	err = tty.cli.ContainerExecResize(tty.Ctx, tty.ID, types.ResizeOptions{
		Height: uint(h),
		Width:  uint(w),
	})
	return err
}
