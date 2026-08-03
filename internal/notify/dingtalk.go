// 本文件通过钉钉自定义机器人发送 Markdown 通知，并支持机器人加签。
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type DingTalk struct {
	Webhook string
	Secret  string
	Client  *http.Client
}

func (notifier DingTalk) Notify(ctx context.Context, event Event) error {
	if notifier.Webhook == "" {
		return nil
	}
	endpoint, err := url.Parse(notifier.Webhook)
	if err != nil {
		return err
	}
	if notifier.Secret != "" {
		timestamp := time.Now().UnixMilli()
		value := fmt.Sprintf("%d\n%s", timestamp, notifier.Secret)
		digest := hmac.New(sha256.New, []byte(notifier.Secret))
		_, _ = digest.Write([]byte(value))
		query := endpoint.Query()
		query.Set("timestamp", fmt.Sprintf("%d", timestamp))
		query.Set("sign", base64.StdEncoding.EncodeToString(digest.Sum(nil)))
		endpoint.RawQuery = query.Encode()
	}
	text := fmt.Sprintf("### %s\n\n- 仓库：`%s`\n- 类型：`%s`\n- 时间：%s\n\n%s",
		event.Title, event.Repository, event.Kind, event.At.Local().Format(time.RFC3339), event.Message)
	if len(event.Paths) > 0 {
		text += "\n\n涉及路径：\n- `" + strings.Join(event.Paths, "`\n- `") + "`"
	}
	payload := map[string]any{
		"msgtype":  "markdown",
		"markdown": map[string]string{"title": event.Title, "text": text},
	}
	content, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.String(), bytes.NewReader(content))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	client := notifier.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	var result struct {
		ErrorCode int    `json:"errcode"`
		ErrorMsg  string `json:"errmsg"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || result.ErrorCode != 0 {
		return fmt.Errorf("dingtalk returned HTTP %d, code %d: %s", response.StatusCode, result.ErrorCode, result.ErrorMsg)
	}
	return nil
}
