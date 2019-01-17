package main

import (
	"context"
	"flag"
	"log"

	"github.com/wukezhan/rainbow/api"
	sess "github.com/wukezhan/rainbow/session"
	"github.com/wukezhan/ssh"
)

func main() {
	log.SetFlags(log.Lshortfile | log.Ldate | log.Ltime)
	ssh.Handle(func(s ssh.Session) {
		log.Print("user ", s.User(), " in ")
		_, winCh, isPty := s.Pty()
		ss := sess.New()
		ss.Kind = "ssh"
		ss.User = sess.User{
			Name: s.User(),
		}
		sss := &sess.SSHSess{
			Ss:   s,
			Sess: ss,
		}
		ss.UIO = sss
		if isPty {
			ctx, cf := context.WithCancel(context.TODO())
			go func() {
				for {
					select {
					case win := <-winCh:
						ss.Winch <- win
					case <-ctx.Done():
						return
					}
				}
			}()
			ss.Relay()
			cf()
		} else {
			subsys := s.SubSys()
			log.Println("subsys", subsys)
			if subsys == "sftp" {
				ss.SFTP()
			}
			log.Println("no-pty", s.Command())
			//io.WriteString(s, "No PTY requested.\n")
			s.Exit(1)
		}
	})

	publicKeyOption := ssh.PublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
		username := ctx.User()
		ra := api.New()
		err, uks := ra.GetKeys(username)
		ul := len(uks)
		if err == nil && ul > 0 {
			i := 0
			for i < ul {
				uk := uks[i]
				allowed, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(uk.PubKey))
				if ssh.KeysEqual(key, allowed) {
					//log.Println("true", key)
					return true
				}
				i++
			}
		}
		return false // allow all keys, or use ssh.KeysEqual() to compare against known keys
	})
	/*passwordOption := ssh.PasswordAuth(func(ctx ssh.Context, password string) bool {
		username := ctx.User()
		log.Println("password", username, password)
		return true
	})*/

	hostKeyOption := ssh.HostKeyFile("./conf/server.id_rsa")

	var ip string
	flag.StringVar(&ip, "ip", "172.16.165.137", "listen ip")
	flag.Parse()

	log.Println("starting ssh server on port " + ip + ":22..")
	log.Fatal(ssh.ListenAndServe(ip+":22", nil, publicKeyOption, hostKeyOption /*, passwordOption*/))
}
