package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"gopkg.in/yaml.v3"
	"io"
	"os"
	"time"
)

func subscriptionFileCommand(ctx context.Context) error {
	var request struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if json.NewDecoder(io.LimitReader(os.Stdin, subscriptionLimit*2)).Decode(&request) != nil {
		return errors.New("invalid file request")
	}
	payload, err := localSubscriptionPayload(ctx, request.Content, request.Name)
	if err != nil {
		return err
	}
	emit([]subscriptionData{payload})
	return nil
}

func localSubscriptionPayload(ctx context.Context, content, name string) (subscriptionData, error) {
	nodes, err := parseSubscription([]byte(content))
	if err != nil {
		fail("需要包含 proxies 的 Clash YAML 文件。")
		return subscriptionData{}, err
	}
	var extension struct {
		GoConnect struct {
			SubscriptionURL string `yaml:"subscription-url"`
		} `yaml:"x-goconnect"`
	}
	if err = yaml.Unmarshal([]byte(content), &extension); err != nil {
		return subscriptionData{}, err
	}
	hash := sha256.Sum256([]byte(content))
	payload := subscriptionData{Name: name, URL: "local://file/" + hex.EncodeToString(hash[:]), Nodes: nodes, Updated: time.Now().Unix(), UnsupportedCount: subscriptionUnsupported([]byte(content), nodes)}
	if extension.GoConnect.SubscriptionURL != "" {
		payload, err = fetchSubscriptionData(ctx, extension.GoConnect.SubscriptionURL)
		if err != nil {
			return subscriptionData{}, err
		}
		payload.Name = name
	}
	return payload, nil
}
