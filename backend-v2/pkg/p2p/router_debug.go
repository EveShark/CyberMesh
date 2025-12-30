package p2p

import (
	"backend/pkg/utils"
	"github.com/libp2p/go-libp2p-pubsub"
)

func (r *Router) DebugConsume(topic string, sub *pubsub.Subscription) {
	r.log.Info("DEBUG consume() STARTED", utils.ZapString("topic", topic))
	for {
		msg, err := sub.Next(r.ctx)
		if err != nil {
			r.log.Warn("consume stopped", utils.ZapString("topic", topic), utils.ZapError(err))
			return
		}
		if msg.ReceivedFrom == "" || len(msg.Data) == 0 {
			continue
		}
		r.log.Info("MESSAGE RECEIVED!!!", utils.ZapString("topic", topic), utils.ZapString("from", msg.ReceivedFrom.String()), utils.ZapInt("bytes", len(msg.Data)))

		// Call original handler logic
		from := msg.ReceivedFrom
		if r.state != nil {
			r.state.OnMessage(topic, from, len(msg.Data))
		}
		r.mu.RLock()
		hs := append([]Handler(nil), r.Handlers[topic]...)
		r.mu.RUnlock()
		r.log.Info("Dispatching", utils.ZapInt("handlers", len(hs)))
		for _, h := range hs {
			select {
			case r.handlerSem <- struct{}{}:
				go func(h Handler) {
					defer func() { <-r.handlerSem }()
					defer func() {
						if rec := recover(); rec != nil {
							r.log.Error("handler panic", utils.ZapString("topic", topic), utils.ZapAny("panic", rec))
						}
					}()
					_ = h(r.ctx, from, msg.Data)
				}(h)
			case <-r.ctx.Done():
				return
			}
		}
	}
}
