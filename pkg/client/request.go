package client

import (
	"io"
	"net/http"
	"net/url"
)

type Request struct {
	client *http.Client
	method string
	scheme string
	host   string
	path   string

	params url.Values
	body   io.Reader
}

func newRequest(method string) *Request {
	return &Request{
		method: method,
	}
}
