package hub

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/svera/acquire"
	"github.com/svera/acquire-server/client"
	"github.com/svera/acquire/board"
	"github.com/svera/acquire/corporation"
	"github.com/svera/acquire/fsm"
	"github.com/svera/acquire/player"
	"github.com/svera/acquire/tile"
	"github.com/svera/acquire/tileset"
	"strconv"
	"strings"
)

type Hub struct {
	// Registered clients
	clients []*client.Client

	// Inbound messages
	Messages chan *client.Message

	// Register requests
	Register chan *client.Client

	// Unregister requests
	Unregister chan *client.Client

	game *acquire.Game
}

func New() *Hub {
	return &Hub{
		Messages:   make(chan *client.Message),
		Register:   make(chan *client.Client),
		Unregister: make(chan *client.Client),
		clients:    []*client.Client{},
	}
}

func (h *Hub) Run() {
	for {
		select {
		case c := <-h.Register:
			h.clients = append(h.clients, c)
			if len(h.clients) == 3 {
				h.newGame()
				h.sendInitialHand()
			}
			break

		case c := <-h.Unregister:
			for _, val := range h.clients {
				if val == c {
					h.removeClient(c)
					close(c.Incoming)
				}
			}
			break

		case m := <-h.Messages:
			if m.Author.Pl != h.game.CurrentPlayer() {
				break
			}
			if m.Content.Typ == "ply" {
				var response []byte
				coords := m.Content.Det["til"]
				tl, err := coordsToTile(coords)
				if err == nil {
					err = h.game.PlayTile(tl)
					if err == nil {
						h.broadcastUpdate()
						h.playerUpdate(m.Author)
					}
				}

				if err != nil {
					res := &ErrorMessage{
						Type:    "err",
						Content: err.Error(),
					}
					response, _ = json.Marshal(res)
					h.sendMessage(m.Author, response)
				}
			}

			if m.Content.Typ == "ncp" {
				var response []byte
				corpName := m.Content.Det["cor"]
				corp, err := h.findCorpByName(corpName)
				if err == nil {
					err := h.game.FoundCorporation(corp)
					if err == nil {
						h.broadcastUpdate()
						h.playerUpdate(m.Author)
					}
				}

				if err != nil {
					res := &ErrorMessage{
						Type:    "err",
						Content: err.Error(),
					}
					response, _ = json.Marshal(res)
					h.sendMessage(m.Author, response)
				}
			}

			if m.Content.Typ == "buy" {
				fmt.Println(m.Content.Det)
			}

			break
		}
	}
}

func (h *Hub) broadcastUpdate() {
	commonMsg := CommonMessage{
		Type:  "upd",
		Board: h.boardOwnership(),
	}
	for _, c := range h.clients {
		if c.Pl == h.game.CurrentPlayer() {
			commonMsg.Enabled = true
		} else {
			commonMsg.Enabled = false
		}
		response, _ := json.Marshal(commonMsg)
		h.sendMessage(c, response)
	}
}

func (h *Hub) playerUpdate(c *client.Client) {
	directMsg := &DirectMessage{
		Type:          "dir",
		Hand:          h.tilesToSlice(c.Pl),
		State:         h.game.GameStateName(),
		InactiveCorps: corpNames(h.game.InactiveCorporations()),
		ActiveCorps:   corpNames(h.game.ActiveCorporations()),
	}
	response, _ := json.Marshal(directMsg)
	h.sendMessage(c, response)
}

func (h *Hub) tilesToSlice(pl player.Interface) []string {
	var hnd []string
	for _, tl := range pl.Tiles() {
		hnd = append(hnd, strconv.Itoa(tl.Number())+tl.Letter())
	}
	return hnd
}

func corpNames(corps []corporation.Interface) []string {
	names := []string{}
	for _, corp := range corps {
		names = append(names, corp.Name())
	}
	return names
}

func (h *Hub) findCorpByName(name string) (corporation.Interface, error) {
	for _, corp := range h.game.Corporations() {
		if strings.ToLower(corp.Name()) == name {
			return corp, nil
		}
	}
	return &corporation.Corporation{}, errors.New("corporation not found")
}

func (h *Hub) boardOwnership() map[string]string {
	cells := make(map[string]string)
	var letters = [9]string{"A", "B", "C", "D", "E", "F", "G", "H", "I"}
	for number := 1; number < 13; number++ {
		for _, letter := range letters {
			cell := h.game.Board().Cell(number, letter)
			if cell.Owner().Type() == "corporation" {
				cells[strconv.Itoa(number)+letter] = strings.ToLower(cell.Owner().(*corporation.Corporation).Name())
			} else {
				cells[strconv.Itoa(number)+letter] = cell.Owner().Type()
			}
		}
	}
	//fmt.Printf("%v", cells)
	return cells
}

func coordsToTile(tl string) (tile.Interface, error) {
	if len(tl) < 2 {
		return &tile.Tile{}, errors.New("Not a valid tile")
	}
	number, _ := strconv.Atoi(tl[:len(tl)-1])
	letter := string(tl[len(tl)-1 : len(tl)])
	return tile.New(number, letter, tile.Unincorporated{}), nil
}

func (h *Hub) sendInitialHand() {
	for i, c := range h.clients {
		tiles := h.game.Player(i).Tiles()
		hnd := []string{}
		for _, tl := range tiles {
			hnd = append(hnd, strconv.Itoa(tl.Number())+tl.Letter())
		}
		res := &DirectMessage{
			Type:  "dir",
			Hand:  h.tilesToSlice(c.Pl),
			State: h.game.GameStateName(),
		}
		response, _ := json.Marshal(res)
		h.sendMessage(c, response)
	}
}

func (h *Hub) sendMessage(c *client.Client, message []byte) {
	select {
	case c.Incoming <- message:
		break

	// We can't reach the client
	default:
		close(c.Incoming)
		h.removeClient(c)
	}
}

func (h *Hub) players() []player.Interface {
	var players []player.Interface
	for _, c := range h.clients {
		players = append(players, c.Pl)
	}
	return players
}

func (h *Hub) removeClient(c *client.Client) {
	for i := range h.clients {
		if h.clients[i] == c {
			h.clients = append(h.clients[:i], h.clients[i+1:]...)
			break
		}
	}
}

func (h *Hub) newGame() {
	corp1, _ := corporation.New("Sackson", 0)
	corp2, _ := corporation.New("Zeta", 0)
	corp3, _ := corporation.New("Hydra", 1)
	corp4, _ := corporation.New("Fusion", 1)
	corp5, _ := corporation.New("America", 1)
	corp6, _ := corporation.New("Phoenix", 2)
	corp7, _ := corporation.New("Quantum", 2)
	h.game, _ = acquire.New(
		board.New(),
		h.players(),
		[7]corporation.Interface{
			corp1,
			corp2,
			corp3,
			corp4,
			corp5,
			corp6,
			corp7,
		},
		tileset.New(),
		&fsm.PlayTile{},
	)
}
