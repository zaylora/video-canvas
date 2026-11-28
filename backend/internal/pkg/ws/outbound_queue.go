package ws

import (
	"slices"
	"sync"
)

type transientKey struct {
	kind    string
	subject string
}

type outboundQueue struct {
	mu        sync.Mutex
	ready     chan struct{}
	reliable  []outboundMessage
	transient []outboundMessage
	bytes     int
	maxCount  int
	maxBytes  int
}

func newOutboundQueue(maxCount, maxBytes int) *outboundQueue {
	return &outboundQueue{ready: make(chan struct{}, 1), maxCount: maxCount, maxBytes: maxBytes}
}

func (queue *outboundQueue) offer(message outboundMessage) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if !message.deliverable() || len(message.payload) > queue.maxBytes {
		return false
	}
	if message.key != (transientKey{}) {
		return queue.offerTransient(message)
	}
	if len(queue.reliable) == queue.maxCount {
		return false
	}
	for len(queue.transient) > 0 && (queue.count() == queue.maxCount || queue.bytes+len(message.payload) > queue.maxBytes) {
		queue.removeFront(&queue.transient)
	}
	if queue.bytes+len(message.payload) > queue.maxBytes {
		return false
	}
	queue.reliable = append(queue.reliable, message)
	queue.bytes += len(message.payload)
	queue.signal()
	return true
}

func (queue *outboundQueue) offerTransient(message outboundMessage) bool {
	for index, previous := range queue.transient {
		if previous.key != message.key || previous.subscription != message.subscription {
			continue
		}
		projected := queue.bytes - len(previous.payload) + len(message.payload)
		if projected > queue.maxBytes {
			return false
		}
		queue.transient[index] = message
		queue.bytes = projected
		return true
	}
	if queue.count() >= queue.maxCount || len(queue.transient) >= (queue.maxCount+1)/2 ||
		queue.bytes+len(message.payload) > queue.maxBytes {
		return false
	}
	queue.transient = append(queue.transient, message)
	queue.bytes += len(message.payload)
	queue.signal()
	return true
}

func (queue *outboundQueue) pop() (outboundMessage, bool) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.count() == 0 {
		return outboundMessage{}, false
	}
	source := &queue.reliable
	if len(*source) == 0 {
		source = &queue.transient
	}
	message := queue.removeFront(source)
	if queue.count() > 0 {
		queue.signal()
	}
	return message, true
}

func (queue *outboundQueue) removeFront(source *[]outboundMessage) outboundMessage {
	message := (*source)[0]
	*source = slices.Delete(*source, 0, 1)
	queue.bytes -= len(message.payload)
	return message
}

func (queue *outboundQueue) purge(subscription *roomSubscription) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	remove := func(message outboundMessage) bool {
		if message.subscription != subscription {
			return false
		}
		queue.bytes -= len(message.payload)
		return true
	}
	queue.reliable = slices.DeleteFunc(queue.reliable, remove)
	queue.transient = slices.DeleteFunc(queue.transient, remove)
}

func (queue *outboundQueue) stats() (count, bytes int) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	return queue.count(), queue.bytes
}

func (queue *outboundQueue) count() int {
	return len(queue.reliable) + len(queue.transient)
}

func (queue *outboundQueue) signal() {
	select {
	case queue.ready <- struct{}{}:
	default:
	}
}
