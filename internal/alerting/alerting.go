// Package alerting 은 룰 평가와 발송을 이어 붙인다.
//
// storage 가 tsdb 와 promql 을 잇는 것과 같은 자리다 — rules 는 전송 방식을
// 모르고 notify 는 룰을 모른다. 둘 다 상대를 모르면 누군가는 알아야 하고,
// 그게 여기다.
package alerting

import (
	"github.com/KeiaiLab/nodevitals-observatory/internal/notify"
	"github.com/KeiaiLab/nodevitals-observatory/internal/rules"
)

// NewSink 는 Notifier 를 rules.Sink 로 감싼다.
func NewSink(n *notify.Notifier) rules.Sink { return &sink{n: n} }

type sink struct{ n *notify.Notifier }

func (s *sink) Notify(ns []rules.Notification) error {
	alerts := make([]notify.Alert, 0, len(ns))
	for _, n := range ns {
		alerts = append(alerts, notify.Alert{
			Labels:      n.Labels,
			Annotations: n.Annotations,
			Status:      notify.Status(n.Status),
			Value:       n.Value,
		})
	}
	return s.n.Send(alerts)
}
