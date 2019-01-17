package api

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
)

// FormData ...
type FormData map[string]interface{}

// Strings ...
type Strings []string

func (ss Strings) Len() int {
	return len(ss)
}
func (ss Strings) Swap(i, j int) {
	ss[i], ss[j] = ss[j], ss[i]
}
func (ss Strings) Less(i, j int) bool {
	return ss[i] < ss[j]
}

// EncodeWithSign ...
func (data FormData) sortedKeys(tokenName string) []string {
	tmp := make(Strings, 0)
	for k := range data {
		// 去除数据本身可能存在 tokenName
		if k != tokenName {
			tmp = append(tmp, k)
		}
	}

	sort.Sort(tmp)

	return tmp
}

// Sign ...
func (data FormData) Sign(secret, tokenName string) string {
	tmp := data.sortedKeys(tokenName)

	s := ""
	for _, k := range tmp {
		v := data[k]
		// 兼容 apiclient 的签名算法
		s = s + k + "=" + fmt.Sprintf("%v", v) + "&"
	}

	s = s + secret
	ms := md5.Sum([]byte(s))
	sign := fmt.Sprintf("%x", ms)

	//log.Println("sign:", s, sign)

	return sign
}

// Check ...
func (data FormData) Check(secret, tokenName string) bool {
	userSign := data[tokenName]
	if userSign == nil {
		return false
	}

	calcSign := data.Sign(secret, tokenName)

	return calcSign == userSign.(string)
}

// URLEncode ..
func (data FormData) URLEncode() string {
	if len(data) == 0 {
		return ""
	}
	var buf bytes.Buffer
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := data[k]
		prefix := url.QueryEscape(k) + "="
		if buf.Len() > 0 {
			buf.WriteByte('&')
		}
		buf.WriteString(prefix)
		buf.WriteString(url.QueryEscape(fmt.Sprintf("%v", v)))
	}
	return buf.String()
}

// JSONEncode .
func (data FormData) JSONEncode() (string, error) {
	jd, err := json.Marshal(data)
	if err != nil {
		return "", err
	}
	return string(jd), nil
}
