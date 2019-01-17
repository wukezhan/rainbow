package api

import (
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

type Api struct {
	Base string
	//URL    string
	Secret string
}

func New() *Api {
	return &Api{
        Base: "",
	}
}

type ApiResp struct {
	Error int             `json:"error"`
	Msg   string          `json:"msg"`
	Data  json.RawMessage `json:"data"`
}

type UserKey struct {
	Title       string `json:"title"`
	PubKey      string `json:"public_key"`
	FingerPrint string `json:"fingerprint"`
}

type UserContainer struct {
	PodName    string   `json:"pod_name"`
	NodeName   string   `json:"node_name"`
	Containers []string `json:"container_name"`
}

func (api *Api) Get(path string, data FormData) (err error, ret []byte) {
	tokenName := "token"
	data[tokenName] = data.Sign(api.Secret, tokenName)
	dataStr := data.URLEncode()
	log.Println("GetFromURL", api.Base+path, string(dataStr), err)

	if err == nil {
		var req *http.Request
		var resp *http.Response
		req, err = http.NewRequest("GET", api.Base+path+"?"+dataStr, nil)

		if err != nil {
			log.Printf("new request error: %s %s\n", api.Base+path, err.Error())
			return
		}

		// 暂仅保留超时设置，后续可加tls相关设置
		client := http.Client{
			Timeout: time.Second * 10,
			Transport: &http.Transport{
				DisableCompression: true,
			},
		}
		resp, err = client.Do(req)
		if err != nil {
			log.Printf("request err %s\n", err.Error())
			return
		}
		defer resp.Body.Close()
		body, _ := ioutil.ReadAll(resp.Body)
		ret = body
		//log.Printf("GetFromURL [%d] %s\n", resp.StatusCode, string(body))
		if resp.StatusCode != 200 {
			err = errors.New(string(body))
		}
	}

	return
}

func (api *Api) GetKeys(username string) (err error, uks []UserKey) {
	formData := FormData{
		"username": username,
	}

	e, ret := api.Get("/userinfo/keys", formData)
	err = e

	var ar ApiResp
	e = json.Unmarshal(ret, &ar)
	if e != nil {
		return
	}

	e = json.Unmarshal([]byte(ar.Data), &uks)

	return
}

func (api *Api) GetContainers(username string) (err error, ucs []UserContainer) {
	formData := FormData{
		"username": username,
	}

	e, ret := api.Get("/userinfo/pods", formData)
	err = e

	var ar ApiResp
	e = json.Unmarshal(ret, &ar)
	if e != nil {
		return
	}

	err = json.Unmarshal([]byte(ar.Data), &ucs)

	return
}
