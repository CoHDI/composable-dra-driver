/*
Copyright 2025 The CoHDI Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package client

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/url"
)

type Request struct {
	method  string
	scheme  string
	host    string
	path    string
	query   url.Values
	headers http.Header
	body    io.Reader
}

func newRequest(method string) *Request {
	return &Request{
		scheme: "https",
		method: method,
	}
}

func (r *Request) setHost(host string) *Request {
	r.host = host
	return r
}

func (r *Request) setPath(path string) *Request {
	r.path = path
	return r
}

func (r *Request) setQuery(query map[string]string) *Request {
	for queryName, value := range query {
		if r.query == nil {
			r.query = make(url.Values)
		}
		r.query[queryName] = append(r.query[queryName], value)
	}
	return r
}

func (r *Request) setBody(body string) *Request {
	r.body = bytes.NewReader([]byte(body))
	return r
}

func (r *Request) setHeader(key string, values ...string) *Request {
	if r.headers == nil {
		r.headers = http.Header{}
	}
	r.headers.Del(key)
	for _, value := range values {
		r.headers.Add(key, value)
	}
	return r
}

func (r *Request) url() *url.URL {
	url := &url.URL{}
	url.Scheme = r.scheme
	url.Host = r.host

	if len(r.path) != 0 {
		url.Path = r.path
	}

	url.RawQuery = r.query.Encode()
	return url
}

func newHTTPRequest(req *Request) (*http.Request, error) {
	httpReq, err := http.NewRequest(req.method, req.url().String(), req.body)
	if err != nil {
		slog.Error("failed to create http request")
		return nil, err
	}

	httpReq.Header = req.headers
	return httpReq, nil
}
