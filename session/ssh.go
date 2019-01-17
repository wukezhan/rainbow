package session

import (
	"encoding/base64"
	"log"

	"github.com/wukezhan/ssh"
)

// SSHSess .
type SSHSess struct {
	Ss   ssh.Session
	Sess *Instance
}

func (ss *SSHSess) Write(b []byte) (n int, err error) {
	n, err = ss.Ss.Write(b)
	return
}

// WriteWebtty .
func (ss *SSHSess) WriteWebtty(b []byte) (n int, err error) {
	// the first byte is type, we should ignore it
	n, err = ss.Ss.Write(b[1:])
	return
}

// WriteString .
func (ss *SSHSess) WriteString(str string) (err error) {
	_, err = ss.Ss.Write([]byte(str))
	return
}

// WritePipe .
func (ss *SSHSess) WritePipe() (err error) {
	defer func() {
		ss.Sess.Mode = Relay
	}()
	initResized := false
	var p []byte
	var q []byte
	bio := ss.Sess.BIO
	bioKind := bio.Kind()
	for {
		if bioKind == "docker" {
			_, p, err = bio.Read()
			if err != nil {
				log.Println("exited", err)
				return
			}
			if ss.Sess.Mode == SFTP {
				//log.Println("ssh got", p)
				_, err = ss.Write(p)
			} else {
				if !initResized && bio.IsTTY() {
					// 必须在 exec attached 之后才能 resize，否则可能触发 no such exec 错误
					bio.ResizeTTY(ss.Sess.win)
					initResized = true
				}
				q, err = base64.StdEncoding.DecodeString(string(p[1:]))
				if err != nil {
					return
				}
				_, err = ss.Write(q)
			}
			if err != nil {
				return
			}
		} else {
			// some other backends
			/*_, p, err = bio.Read()
			_, err = ss.Write(p)
			if err != nil {
				return
			}*/
		}

	}
}

func (ss *SSHSess) Read(buf []byte) (n int, err error) {
	n, err = ss.Ss.Read(buf)
	return
}

// ReadPipe .
func (ss *SSHSess) ReadPipe() (err error) { return }

// Close .
func (ss *SSHSess) Close() (err error) {
	ss.Ss.Exit(0)
	return
}

// Kind .
func (ss *SSHSess) Kind() string {
	return "ssh"
}
