// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package apim

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// PostMultipart uploads one file with string fields beside it, the shape of
// the publisher's import-openapi endpoint.
func (c *Client) PostMultipart(ctx context.Context, path string, file io.Reader, filename string,
	fields map[string]string, into any) error {
	var buffer bytes.Buffer
	writer := multipart.NewWriter(&buffer)
	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, file); err != nil {
		return err
	}
	for name, value := range fields {
		if err := writer.WriteField(name, value); err != nil {
			return err
		}
	}
	if err := writer.Close(); err != nil {
		return err
	}
	call, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(call, http.MethodPost, c.Base+path, &buffer)
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+c.Token)
	return c.read(request, into)
}

// PostForm sends a URL-encoded form, optionally with HTTP basic credentials:
// the shape of the key manager's token endpoint.
func (c *Client) PostForm(ctx context.Context, path string, form url.Values, user, password string,
	into any) error {
	call, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(call, http.MethodPost, c.Base+path,
		strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Accept", "application/json")
	if user != "" {
		request.SetBasicAuth(user, password)
	} else if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	return c.read(request, into)
}

// PostBasic sends JSON under HTTP basic credentials: the shape of the
// dynamic client registration endpoint.
func (c *Client) PostBasic(ctx context.Context, path string, user, password string, body, into any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	call, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(call, http.MethodPost, c.Base+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.SetBasicAuth(user, password)
	return c.read(request, into)
}

// read performs a prepared request and decodes the answer the way do does.
func (c *Client) read(request *http.Request, into any) error {
	response, err := c.HTTP.Do(request)
	if err != nil {
		return classifyTransport(c.Base, err)
	}
	defer response.Body.Close()
	answer, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return &Refusal{Status: response.StatusCode, Body: string(answer)}
	}
	if into == nil || len(bytes.TrimSpace(answer)) == 0 {
		return nil
	}
	if err := json.Unmarshal(answer, into); err != nil {
		return &unreadable{err: err}
	}
	return nil
}
