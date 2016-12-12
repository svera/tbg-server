package room

import (
	"encoding/json"
	"errors"

	"github.com/svera/sackson-server/interfaces"
	"github.com/svera/sackson-server/messages"
)

func (r *Room) kickPlayerAction(m *interfaces.IncomingMessage) error {
	var err error
	if m.Author != r.owner {
		return errors.New(Forbidden)
	}
	var parsed interfaces.MessageKickPlayerParams
	if err = json.Unmarshal(m.Content.Params, &parsed); err == nil {
		return r.kickClient(parsed.PlayerNumber)
	}
	return err
}

func (r *Room) kickClient(number int) error {
	if number < 0 || number >= len(r.clients) {
		return errors.New(InexistentClient)
	}
	cl := r.clients[number]
	if cl == r.owner {
		return errors.New(OwnerNotRemovable)
	}
	cl.SetRoom(nil)
	r.RemoveClient(r.clients[number])
	response := messages.New(interfaces.TypeMessageClientOut, interfaces.ReasonPlayerKicked)
	go r.emitter.Emit("messageCreated", []interfaces.Client{cl}, response)
	return nil
}