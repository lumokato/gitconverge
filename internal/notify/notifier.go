// 本文件定义同步事件通知接口和统一事件结构。
package notify

import (
	"context"
	"fmt"
	"time"
)

type Severity string

const (
	SeverityInfo    Severity = "info"
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

type Event struct {
	Repository string
	Severity   Severity
	Kind       string
	Title      string
	Message    string
	Paths      []string
	At         time.Time
}

func (event Event) Key() string {
	return fmt.Sprintf("%s:%s", event.Repository, event.Kind)
}

type Notifier interface {
	Notify(context.Context, Event) error
}

type Noop struct{}

func (Noop) Notify(context.Context, Event) error { return nil }
