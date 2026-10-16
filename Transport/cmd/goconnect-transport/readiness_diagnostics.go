package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

func coreReadiness(client *http.Client, port uint16, token, device string) (bool, diagnosticDetails) {
	return coreReadinessWith(client, port, token, device, net.InterfaceByName)
}

func coreReadinessWith(client *http.Client, port uint16, token, device string, lookup func(string) (*net.Interface, error)) (bool, diagnosticDetails) {
	start := time.Now()
	d := diagnosticDetails{Stage: "health_core", Interface: device, TimeoutMS: client.Timeout.Milliseconds()}
	finish := func(reason string, err error) (bool, diagnosticDetails) {
		d.Reason = reason
		d.DurationMS = time.Since(start).Milliseconds()
		d.Errno = diagnosticError(err).Errno
		return false, d
	}
	iface, err := lookup(device)
	if err != nil || iface == nil {
		return finish("tun_missing", err)
	}
	if iface.Flags&net.FlagUp == 0 {
		return finish("tun_down", nil)
	}
	request, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/configs", port), nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		var timeout net.Error
		if errors.As(err, &timeout) && timeout.Timeout() {
			return finish("controller_timeout", err)
		}
		return finish("controller_connect_failed", err)
	}
	defer response.Body.Close()
	d.HTTPStatus = response.StatusCode
	if response.StatusCode != http.StatusOK {
		return finish("controller_http_failed", nil)
	}
	var body struct {
		Tun struct {
			Enable bool `json:"enable"`
		} `json:"tun"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 65536)).Decode(&body); err != nil {
		return finish("controller_decode_failed", err)
	}
	if !body.Tun.Enable {
		return finish("tun_disabled", nil)
	}
	d.Reason = "success"
	d.DurationMS = time.Since(start).Milliseconds()
	return true, d
}
