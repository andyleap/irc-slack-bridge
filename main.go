// irc-slack-bridge project main.go
package main

import (
	"encoding/json"
	"html"
	"io/ioutil"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	irc "github.com/fluffle/goirc/client"
	"github.com/nlopes/slack"
)

type IRCUser struct {
	Conn   *irc.Conn
	ID     string
	Name   string
	Active bool
}

var ircClients atomic.Value
var ircClientsWMutex sync.Mutex

var mainClient *irc.Conn
var slackClient *slack.Client
var slackrtm *slack.RTM
var slackChannel slack.Channel

var myid string

var Config struct {
	Server  string
	Channel string
	Nick    string
	Suffix  string

	BlacklistUsers []string

	SlackAPI     string
	SlackChannel string
	SlackIcon    string
}

func main() {
	buf, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatalf("Could not read config file: %s", err)
	}
	err = json.Unmarshal(buf, &Config)
	if err != nil {
		log.Fatalf("Could not parse config file: %s", err)
	}
	c := irc.NewConfig(Config.Nick)
	c.Server = Config.Server
	mainClient = irc.Client(c)
	mainClient.EnableStateTracking()
	mainClient.HandleFunc(irc.CONNECTED, func(conn *irc.Conn, line *irc.Line) {
		log.Printf("IRC Connected")
		mainClient.Join(Config.Channel)
	})
	mainClient.HandleFunc(irc.ACTION, func(conn *irc.Conn, line *irc.Line) {
		SendMessageToSlack(line.Nick, "_"+line.Text()+"_")
	})
	mainClient.HandleFunc(irc.PRIVMSG, func(conn *irc.Conn, line *irc.Line) {
		SendMessageToSlack(line.Nick, line.Text())
	})
	mainClient.HandleFunc(irc.DISCONNECTED,
		func(conn *irc.Conn, line *irc.Line) {
			time.AfterFunc(time.Second*30, func() {
				mainClient.Connect()
			})
		})
	mainClient.Connect()

	slackClient = slack.New(Config.SlackAPI)
	slackChannels, _ := slackClient.GetChannels(true)
	for _, sc := range slackChannels {
		if sc.Name == Config.SlackChannel {
			slackChannel = sc
		}
	}

	atr, _ := slackClient.AuthTest()
	myid = atr.UserID

	slackrtm = slackClient.NewRTM()
	users, _ := slackClient.GetUsers()
	ircClients.Store(map[string]*IRCUser{})
	for _, user := range users {
		found := false
		for _, channeluser := range slackChannel.Members {
			if channeluser == user.ID {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		err := AddUser(user.Name, user.ID)
		if err != nil {
			log.Printf("Unable to add user %s: %s", user, err)
		}
	}

	go slackrtm.ManageConnection()
	for event := range slackrtm.IncomingEvents {
		switch ev := event.Data.(type) {
		case *slack.PresenceChangeEvent:
			if ircuser, ok := ircClients.Load().(map[string]*IRCUser)[ev.User]; ok {
				if ev.Presence == "away" {
					ircuser.Conn.Away("away on Slack")
				} else {
					ircuser.Conn.Away("")
				}
			}
		case *slack.MessageEvent: //func(text string, source User, channel string, response MessageTarget)
			if ev.Channel == slackChannel.ID {
				if ircuser, ok := ircClients.Load().(map[string]*IRCUser)[ev.User]; ok {
					text := html.UnescapeString(ev.Text)
					if ev.SubType == "" {
						ircuser.Conn.Privmsg(Config.Channel, text)
					} else if ev.SubType == "bot_message" {
						if ev.Username != "" {
							text = "<" + ev.Username + ">" + text
						}
						ircuser.Conn.Action(Config.Channel, text)
					} else if ev.SubType == "me_message" {
						ircuser.Conn.Action(Config.Channel, text)
					} else if ev.SubType == "channel_join" {
						user, err := slackClient.GetUserInfo(ev.User)
						if err != nil {
							log.Printf("Unable to get user info for %s: %s", ev.User, err)
						} else {
							AddUser(user.Name, user.ID)
						}
					} else if ev.SubType == "channel_leave" {
						RemoveUser(ev.User)
					}
				}
			}
		}
	}
}

func SendMessageToSlack(nick string, message string) {
	for _, client := range ircClients.Load().(map[string]*IRCUser) {
		if client.Conn.Me().Nick == nick {
			return
		}
	}

	pmp := slack.PostMessageParameters{}
	pmp.Username = nick
	pmp.IconURL = strings.Replace(Config.SlackIcon, "$username", nick, 1)
	pmp.Parse = "full"

	slackClient.PostMessage(slackChannel.ID, message, pmp)
}

func AddUser(username string, id string) error {
	if id == myid {
		return nil
	}
	for _, bluser := range Config.BlacklistUsers {
		if bluser == username {
			return nil
		}
	}

	ircuser := &IRCUser{ID: id, Name: username, Active: true}
	c := irc.NewConfig(username + Config.Suffix)
	c.Server = Config.Server
	userConn := irc.Client(c)
	userConn.EnableStateTracking()
	userConn.HandleFunc(irc.CONNECTED, func(conn *irc.Conn, line *irc.Line) {
		p, _ := slackClient.GetUserPresence(id)
		if p.Presence == "away" {
			userConn.Away("away on Slack")
		} else {
			ircuser.Conn.Away("")
		}
		userConn.Join(Config.Channel)
	})
	userConn.HandleFunc(irc.DISCONNECTED,
		func(conn *irc.Conn, line *irc.Line) {
			if ircuser.Active {
				time.AfterFunc(time.Second*30, func() {
					userConn.Connect()
				})
			}
		})
	userConn.Connect()
	ircuser.Conn = userConn
	ircClientsWMutex.Lock()
	defer ircClientsWMutex.Unlock()
	m1 := ircClients.Load().(map[string]*IRCUser)
	m2 := map[string]*IRCUser{}
	for k, v := range m1 {
		m2[k] = v
	}
	m2[id] = ircuser
	ircClients.Store(m2)
	return nil
}

func RemoveUser(id string) error {
	ircClientsWMutex.Lock()
	defer ircClientsWMutex.Unlock()
	m1 := ircClients.Load().(map[string]*IRCUser)
	m2 := map[string]*IRCUser{}
	for k, v := range m1 {
		if k == id {
			v.Active = false
			v.Conn.Close()
			continue
		}
		m2[k] = v
	}
	ircClients.Store(m2)
	return nil
}
