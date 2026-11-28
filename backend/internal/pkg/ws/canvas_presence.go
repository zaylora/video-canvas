package ws

import (
	"context"
	"encoding/json"
	"math"
	"time"
	"unicode/utf8"

	"video-canvas/internal/pkg/idcodec"
)

// TypeCanvasPresence carries temporary canvas presence without edit content.
const TypeCanvasPresence = "canvas.presence"

// TypeCanvasAttendance carries an online heartbeat without viewport or editing state.
const TypeCanvasAttendance = "canvas.attendance"

const canvasPresenceAuthorizationTTL = 750 * time.Millisecond
const canvasPresenceUsernameRetry = 5 * time.Second

type canvasPresencePoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type canvasPresenceDrag struct {
	ID string `json:"id"`
	canvasPresencePoint
}

type canvasPresenceView struct {
	canvasPresencePoint
	Zoom float64 `json:"zoom"`
}

type canvasPresenceState struct {
	Epoch      uint64               `json:"epoch"`
	OnlineOnly bool                 `json:"onlineOnly,omitempty"`
	Cursor     *canvasPresencePoint `json:"cursor,omitempty"`
	Selection  []string             `json:"selection,omitempty"`
	Drag       *canvasPresenceDrag  `json:"drag,omitempty"`
	Editing    string               `json:"editing,omitempty"`
	View       *canvasPresenceView  `json:"view,omitempty"`
}

type canvasPresence struct {
	canvasPresenceState
	ActorID       string `json:"actorId"`
	ActorUsername string `json:"actorUsername,omitempty"`
}

type canvasPresenceEnvelope struct {
	Room  string         `json:"room"`
	State canvasPresence `json:"state"`
}

type canvasPresenceAccess struct {
	readUntil time.Time
	editUntil time.Time
}

func (bus *CanvasCollaborationBus) receivePresence(ctx context.Context, sender *client, room string, raw json.RawMessage, onlineOnly bool) {
	if canvasID, ok := CanvasCollaborationID(room); !ok || canvasID == "" || !validChannel(room) {
		return
	}
	now := time.Now()
	if now.Sub(sender.presenceAt) < 80*time.Millisecond {
		return
	}
	sender.presenceAt = now
	var state canvasPresenceState
	if len(raw) > 3072 || json.Unmarshal(raw, &state) != nil {
		return
	}
	state.OnlineOnly = onlineOnly
	if !state.valid() {
		return
	}
	authorize := bus.editAccess
	if state.OnlineOnly {
		authorize = bus.hub.authorizer
	}
	if !sender.canExchangePresence(ctx, room, authorize, !state.OnlineOnly) {
		return
	}
	event := canvasPresenceEnvelope{Room: room, State: canvasPresence{
		canvasPresenceState: state,
		ActorID:             idcodec.Encode(sender.userID),
		ActorUsername:       bus.resolvePresenceUsername(ctx, sender, now),
	}}
	if bus.redis != nil {
		data, err := json.Marshal(event)
		if err == nil {
			deadline, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
			published := bus.redis.Publish(deadline, canvasPresenceTopic, data).Err() == nil
			cancel()
			if published {
				return
			}
		}
	}
	bus.hub.Publish(ctx, room, event.State.message())
}

func (state canvasPresence) message() Message {
	kind := TypeCanvasPresence
	if state.OnlineOnly {
		kind = TypeCanvasAttendance
	}
	return Message{Type: kind, Data: state}
}

func (bus *CanvasCollaborationBus) resolvePresenceUsername(ctx context.Context, sender *client, now time.Time) string {
	if bus.presenceUsername == nil || sender.presenceUsername != "" || now.Before(sender.presenceUsernameRetryAt) {
		return sender.presenceUsername
	}
	deadline, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()
	sender.presenceUsername = bus.presenceUsername(deadline, sender.userID)
	sender.presenceUsernameRetryAt = now.Add(canvasPresenceUsernameRetry)
	return sender.presenceUsername
}

func (state canvasPresenceState) valid() bool {
	if state.OnlineOnly {
		return state.Epoch > 0 && state.Cursor == nil && len(state.Selection) == 0 && state.Editing == "" && state.View == nil && state.Drag == nil
	}
	if state.Epoch == 0 || len(state.Selection) > 16 || !validPresencePoint(state.Cursor) ||
		(state.Editing != "" && !validPresenceID(state.Editing)) || !validPresenceView(state.View) {
		return false
	}
	for _, id := range state.Selection {
		if !validPresenceID(id) {
			return false
		}
	}
	return state.Drag == nil || (validPresenceID(state.Drag.ID) && validPresencePoint(&state.Drag.canvasPresencePoint))
}

func validPresenceView(view *canvasPresenceView) bool {
	return view == nil || (view.Zoom > 0 && view.Zoom <= 4 && validPresencePoint(&view.canvasPresencePoint))
}

func validPresenceID(id string) bool {
	return id != "" && utf8.RuneCountInString(id) <= 128
}

func validPresencePoint(point *canvasPresencePoint) bool {
	return point == nil || (math.Abs(point.X) <= 1e9 && math.Abs(point.Y) <= 1e9)
}

func (bus *CanvasCollaborationBus) deliverRemotePresence(ctx context.Context, payload string) {
	var event canvasPresenceEnvelope
	if len(payload) > 4096 || json.Unmarshal([]byte(payload), &event) != nil || event.State.ActorID == "" || !event.State.valid() {
		return
	}
	if id, ok := CanvasCollaborationID(event.Room); ok && id != "" && validChannel(event.Room) {
		bus.hub.Publish(ctx, event.Room, event.State.message())
	}
}

func (h *Hub) publishCanvasPresence(ctx context.Context, room string, targets []publishTarget, data any, payload []byte) {
	state, ok := data.(canvasPresence)
	if !ok {
		return
	}
	key := transientKey{kind: state.message().Type, subject: state.ActorID}
	for _, target := range targets {
		if idcodec.Encode(target.userID) == state.ActorID {
			continue
		}
		if target.canExchangePresence(ctx, room, h.authorizer, false) {
			target.enqueueRoomMessage(payload, key)
		}
	}
}

func (c *client) canExchangePresence(ctx context.Context, room string, authorize Authorizer, edit bool) bool {
	h := c.hub
	h.mu.RLock()
	subscription := c.subs[room]
	cached := c.presenceAuth[room]
	if subscription == nil || authorize == nil {
		h.mu.RUnlock()
		return false
	}
	accessVersion := subscription.presenceAccessVersion
	h.mu.RUnlock()
	if (!edit && time.Now().Before(cached.readUntil)) || (edit && time.Now().Before(cached.editUntil)) {
		return true
	}
	check, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	ok := authorize(check, c.userID, room)
	cancel()
	if !ok {
		return false
	}
	return c.cachePresenceAccess(room, subscription, accessVersion, edit)
}

func (c *client) cachePresenceAccess(room string, subscription *roomSubscription, accessVersion uint64, edit bool) bool {
	h := c.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.subs[room] != subscription || subscription.presenceAccessVersion != accessVersion {
		return false
	}
	if c.presenceAuth == nil {
		c.presenceAuth = make(map[string]canvasPresenceAccess)
	}
	cached := c.presenceAuth[room]
	until := time.Now().Add(canvasPresenceAuthorizationTTL)
	if edit {
		cached.editUntil = until
	} else {
		cached.readUntil = until
	}
	c.presenceAuth[room] = cached
	return true
}
