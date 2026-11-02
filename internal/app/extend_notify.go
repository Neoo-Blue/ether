package app

import (
	"github.com/braedonsaunders/ether/internal/extend"
	"github.com/braedonsaunders/ether/internal/notify"
)

// registerNotifyDescriptors registers built-in notification channel and event kinds (WS1c).
func (a *App) registerNotifyDescriptors(r *extend.Registry) error {
	ds := append(notify.ChannelKindDescriptors(), notify.EventKindDescriptors()...)
	return r.Register(ds...)
}
