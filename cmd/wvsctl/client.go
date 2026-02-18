package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	baseURL string
}

func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL}
}

func (c *Client) Get(path string, out interface{}) error {
	resp, err := http.Get(c.baseURL + path)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return parseResponse(resp, out)
}

func (c *Client) Post(path string, body interface{}, out interface{}) error {
	var reqBody io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reqBody = bytes.NewReader(b)
	}
	resp, err := http.Post(c.baseURL+path, "application/json", reqBody)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return parseResponse(resp, out)
}

func (c *Client) Delete(path string, out interface{}) error {
	req, _ := http.NewRequest("DELETE", c.baseURL+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return parseResponse(resp, out)
}

func parseResponse(resp *http.Response, out interface{}) error {
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		var errResp struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		json.Unmarshal(b, &errResp)
		return fmt.Errorf("%s: %s", errResp.Code, errResp.Message)
	}
	if out != nil {
		return json.Unmarshal(b, out)
	}
	return nil
}
