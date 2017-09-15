package main

import (
	"encoding/base64"
	"time"
)

type Client struct {
	Transport     RoundTripper
	CheckRedirect func(req *Request, via []*Request) error
	Jar           CookieJar
	Timeout       time.Duration
}

var DefaultClient = &Client{}

type RoundTripper interface {
	RoundTrip(*Request) (*Response, error)
}

func basicAuth(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
