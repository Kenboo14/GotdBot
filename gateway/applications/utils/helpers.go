package utils

import (
	"errors"
	"net/http"
	"regexp"
	"strconv"
)

var publicKeyRe = regexp.MustCompile(`"publicKey":"(.+?)","keyId":(\d+?)\}\}`)

func GetPublicKey(data string) (publicKey string, keyID int, err error) {
	matches := publicKeyRe.FindStringSubmatch(data)

	if len(matches) != 3 {
		return "", 0, errors.New("publicKey or keyId not found")
	}

	publicKey = matches[1]
	keyID, err = strconv.Atoi(matches[2])
	if err != nil {
		return "", 0, err
	}

	return publicKey, keyID, nil
}

func MergeCookies(oldCookies, newCookies []*http.Cookie) []*http.Cookie {
	m := make(map[string]*http.Cookie)

	for _, c := range oldCookies {
		m[c.Name] = c
	}
	for _, c := range newCookies {
		m[c.Name] = c // overwrite jika sama
	}

	result := make([]*http.Cookie, 0, len(m))
	for _, c := range m {
		result = append(result, c)
	}
	return result
}
