package discord

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/denverquane/amongusdiscord/game"
	"github.com/denverquane/amongusdiscord/storage"
	socketio "github.com/googollee/go-socket.io"
	"github.com/gorilla/mux"
)

type GameOrLobbyCode struct {
	gameCode    string
	connectCode string
}

type BcastMsgType int

const (
	GRACEFUL_SHUTDOWN BcastMsgType = iota
	FORCE_SHUTDOWN
)

type BroadcastMessage struct {
	Type BcastMsgType
	Data int
}

type LobbyStatus struct {
	GuildID string
	Lobby   game.Lobby
}

type SocketStatus struct {
	GuildID   string
	Connected bool
}

type SessionManager struct {
	PrimarySession *discordgo.Session
	AltSession     *discordgo.Session
	count          int
	countLock      sync.Mutex
}

func NewSessionManager(primary, secondary *discordgo.Session) SessionManager {
	return SessionManager{
		PrimarySession: primary,
		AltSession:     secondary,
		count:          0,
		countLock:      sync.Mutex{},
	}
}

func (sm *SessionManager) GetPrimarySession() *discordgo.Session {
	return sm.PrimarySession
}

func (sm *SessionManager) GetSessionForRequest() *discordgo.Session {
	if sm.AltSession == nil {
		return sm.PrimarySession
	}
	sm.countLock.Lock()
	defer sm.countLock.Unlock()

	sm.count++
	if sm.count%2 == 0 {
		log.Println("Primärsitzung für Anfrage verwenden")
		return sm.PrimarySession
	} else {
		log.Println("Verwenden der sekundären Sitzung zur Anforderung")
		return sm.AltSession
	}
}

func (sm *SessionManager) Close() {
	if sm.PrimarySession != nil {
		sm.PrimarySession.Close()
	}

	if sm.AltSession != nil {
		sm.AltSession.Close()
	}
}

type Bot struct {
	url                     string
	socketPort              string
	extPort                 string
	AllConns                map[string]string
	AllGuilds               map[string]*GuildState
	LinkCodes               map[GameOrLobbyCode]string
	GamePhaseUpdateChannels map[string]*chan game.Phase

	PlayerUpdateChannels map[string]*chan game.Player

	SocketUpdateChannels map[string]*chan SocketStatus

	GlobalBroadcastChannels map[string]*chan BroadcastMessage

	LobbyUpdateChannels map[string]*chan LobbyStatus

	LinkCodeLock sync.RWMutex

	ChannelsMapLock sync.RWMutex

	SessionManager SessionManager

	StorageInterface storage.StorageInterface
}

func (bot *Bot) PushGuildSocketUpdate(guildID string, status SocketStatus) {
	bot.ChannelsMapLock.RLock()
	*(bot.SocketUpdateChannels)[guildID] <- status
	bot.ChannelsMapLock.RUnlock()
}

func (bot *Bot) PushGuildPlayerUpdate(guildID string, status game.Player) {
	bot.ChannelsMapLock.RLock()
	*(bot.PlayerUpdateChannels)[guildID] <- status
	bot.ChannelsMapLock.RUnlock()
}

func (bot *Bot) PushGuildPhaseUpdate(guildID string, status game.Phase) {
	bot.ChannelsMapLock.RLock()
	*(bot.GamePhaseUpdateChannels)[guildID] <- status
	bot.ChannelsMapLock.RUnlock()
}

func (bot *Bot) PushGuildLobbyUpdate(guildID string, status LobbyStatus) {
	bot.ChannelsMapLock.RLock()
	*(bot.LobbyUpdateChannels)[guildID] <- status
	bot.ChannelsMapLock.RUnlock()
}

var Version string

// MakeAndStartBot does what it sounds like
//TODO collapse these fields into proper structs?
func MakeAndStartBot(version, token, token2, url, port, extPort, emojiGuildID string, numShards, shardID int, storageClient storage.StorageInterface) *Bot {
	Version = version

	var altDiscordSession *discordgo.Session = nil

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		log.Println("Fehler beim Erstellen der Discord-Sitzung,", err)
		return nil
	}
	if token2 != "" {
		altDiscordSession, err = discordgo.New("Bot " + token2)
		if err != nil {
			log.Println("Fehler beim Erstellen der 2. Discord-Sitzung,", err)
			return nil
		}
	}

	if numShards > 1 {
		log.Printf("Identifizieren mit der Discord-API mit %d Gesamt-Shards und Shard-ID =%d\n", numShards, shardID)
		dg.ShardCount = numShards
		dg.ShardID = shardID
		if altDiscordSession != nil {
			log.Printf("Identifizieren der Discord-API für den 2. Bot mit %d Gesamt-Shards und Shard-ID =%d\n", numShards, shardID)
			altDiscordSession.ShardCount = numShards
			altDiscordSession.ShardID = shardID
		}
	}

	bot := Bot{
		url:                     url,
		socketPort:              port,
		extPort:                 extPort,
		AllConns:                make(map[string]string),
		AllGuilds:               make(map[string]*GuildState),
		LinkCodes:               make(map[GameOrLobbyCode]string),
		GamePhaseUpdateChannels: make(map[string]*chan game.Phase),
		PlayerUpdateChannels:    make(map[string]*chan game.Player),
		SocketUpdateChannels:    make(map[string]*chan SocketStatus),
		GlobalBroadcastChannels: make(map[string]*chan BroadcastMessage),
		LobbyUpdateChannels:     make(map[string]*chan LobbyStatus),
		LinkCodeLock:            sync.RWMutex{},
		ChannelsMapLock:         sync.RWMutex{},
		SessionManager:          NewSessionManager(dg, altDiscordSession),
		StorageInterface:        storageClient,
	}

	dg.AddHandler(bot.voiceStateChange())
	// Register the messageCreate func as a callback for MessageCreate events.
	dg.AddHandler(bot.messageCreate())
	dg.AddHandler(bot.reactionCreate())
	dg.AddHandler(bot.newGuild(emojiGuildID))

	dg.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuildVoiceStates | discordgo.IntentsGuildMessages | discordgo.IntentsGuilds | discordgo.IntentsGuildMessageReactions)

	//Open a websocket connection to Discord and begin listening.
	err = dg.Open()
	if err != nil {
		log.Println("Bot konnte mit Fehler nicht mit den Discord-Servern verbunden werden:", err)
		return nil
	}

	if altDiscordSession != nil {
		altDiscordSession.AddHandler(newAltGuild)
		altDiscordSession.Identify.Intents = discordgo.MakeIntent(discordgo.IntentsGuilds)
		err = altDiscordSession.Open()
		if err != nil {
			log.Println("Der 2. Bot konnte fehlerhaft nicht mit den Discord-Servern verbunden werden:", err)
			return nil
		}
	}

	// Wait here until CTRL-C or other term signal is received.

	bot.Run()

	return &bot
}

func (bot *Bot) Run() {
	go bot.socketioServer(bot.socketPort)
}

func (bot *Bot) Close() {
	bot.SessionManager.Close()
}

func (bot *Bot) guildIDForCode(code string) string {
	if code == "" {
		return ""
	}
	bot.LinkCodeLock.RLock()
	defer bot.LinkCodeLock.RUnlock()
	for codes, gid := range bot.LinkCodes {
		if code != "" {
			if codes.gameCode == code || codes.connectCode == code {
				return gid
			}
		}
	}
	return ""
}

func (bot *Bot) socketioServer(port string) {
	server, err := socketio.NewServer(nil)
	if err != nil {
		log.Fatal(err)
	}
	server.OnConnect("/", func(s socketio.Conn) error {
		s.SetContext("")
		log.Println("verbunden:", s.ID())
		return nil
	})
	server.OnEvent("/", "connectCode", func(s socketio.Conn, msg string) {
		log.Printf("Verbindungscode erhalten: \"%s\"", msg)
		guildID := bot.guildIDForCode(msg)
		if guildID == "" {
			log.Printf("Keine Gilde hat den aktuellen Verbindungscode von %s\n", msg)
			return
		}
		//only link the socket to guilds that we actually have a record of
		if guild, ok := bot.AllGuilds[guildID]; ok {
			bot.AllConns[s.ID()] = guildID
			guild.Linked = true

			bot.PushGuildSocketUpdate(guildID, SocketStatus{
				GuildID:   guildID,
				Connected: true,
			})
		}

		log.Printf("Zugehörige Websocket-ID %s mit guildID %s unter Verwendung von Code %s\n", s.ID(), guildID, msg)
		//s.Emit("reply", "set guildID successfully")
	})
	server.OnEvent("/", "lobby", func(s socketio.Conn, msg string) {
		log.Println("lobby:", msg)
		lobby := game.Lobby{}
		err := json.Unmarshal([]byte(msg), &lobby)
		if err != nil {
			log.Println(err)
		} else {
			guildID := ""

			//TODO race condition
			if gid, ok := bot.AllConns[s.ID()]; ok {
				guildID = gid
			} else {
				guildID = bot.guildIDForCode(lobby.LobbyCode)
			}

			if guildID != "" {
				if guild, ok := bot.AllGuilds[guildID]; ok { // Game is connected -> update its room code
					log.Println("Raumcode erhalten", msg, "für die Gilde", guild.PersistentGuildData.GuildID, "von der Erfassung")
				} else {
					bot.PushGuildSocketUpdate(guildID, SocketStatus{
						GuildID:   guildID,
						Connected: true,
					})
					log.Println("Assoziierte Lobby mit bestehendem Spiel!")
				}
				//we went to lobby, so set the phase. Also adds the initial reaction emojis
				bot.PushGuildPhaseUpdate(guildID, game.LOBBY)
				if bot.AllConns[s.ID()] != guildID {
					bot.AllConns[s.ID()] = guildID
				}
				bot.PushGuildLobbyUpdate(guildID, LobbyStatus{
					GuildID: guildID,
					Lobby:   lobby,
				})
			} else {
				log.Println("Ich habe keine Aufzeichnung von Spielen mit einer Lobby oder einem Verbindungscode von " + lobby.LobbyCode)
			}
		}
	})
	server.OnEvent("/", "state", func(s socketio.Conn, msg string) {
		log.Println("Phase von der Erfassung erhalten: ", msg)
		phase, err := strconv.Atoi(msg)
		if err != nil {
			log.Println(err)
		} else {
			if gid, ok := bot.AllConns[s.ID()]; ok && gid != "" {
				log.Println("Phasenereignis auf Kanal schieben")
				bot.PushGuildPhaseUpdate(gid, game.Phase(phase))
			} else {
				log.Println("Dieser Websocket ist keiner Gilde zugeordnet")
			}
		}
	})
	server.OnEvent("/", "player", func(s socketio.Conn, msg string) {
		log.Println("Spieler von Capture erhalten: ", msg)
		player := game.Player{}
		err := json.Unmarshal([]byte(msg), &player)
		if err != nil {
			log.Println(err)
		} else {
			if gid, ok := bot.AllConns[s.ID()]; ok && gid != "" {
				bot.PushGuildPlayerUpdate(gid, player)
			} else {
				log.Println("Dieser Websocket ist keiner Gilde zugeordnet")
			}
		}
	})
	server.OnError("/", func(s socketio.Conn, e error) {
		log.Println("Fehler:", e)
	})
	server.OnDisconnect("/", func(s socketio.Conn, reason string) {
		log.Println("Client-Verbindung geschlossen: ", reason)

		previousGid := bot.AllConns[s.ID()]
		delete(bot.AllConns, s.ID())
		bot.LinkCodeLock.Lock()
		for i, v := range bot.LinkCodes {
			//delete the association between the link code and the guild
			if v == previousGid {
				delete(bot.LinkCodes, i)
				break
			}
		}
		bot.LinkCodeLock.Unlock()

		for gid, guild := range bot.AllGuilds {
			if gid == previousGid {
				bot.LinkCodeLock.Lock()
				guild.Linked = false
				bot.LinkCodeLock.Unlock()
				bot.PushGuildSocketUpdate(gid, SocketStatus{
					GuildID:   gid,
					Connected: false,
				})

				log.Printf("Zugeordnete Websocket-ID %s mit guildID %s\n", s.ID(), gid)
			}
		}
	})
	go server.Serve()
	defer server.Close()

	//http.Handle("/socket.io/", server)

	router := mux.NewRouter()
	router.Handle("/socket.io/", server)

	log.Printf("Serving at localhost:%s...\n", port)
	log.Fatal(http.ListenAndServe(":"+port, router))
}

func MessagesServer(port string, bots []*Bot) {

	http.HandleFunc("/graceful", func(w http.ResponseWriter, r *http.Request) {
		for _, bot := range bots {
			bot.ChannelsMapLock.RLock()
			for _, v := range bot.GlobalBroadcastChannels {
				*v <- BroadcastMessage{
					Type: GRACEFUL_SHUTDOWN,
					Data: 30,
				}
			}
			bot.ChannelsMapLock.RUnlock()
		}
	})

	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func (bot *Bot) updatesListener() func(dg *discordgo.Session, guildID string, socketUpdates *chan SocketStatus, phaseUpdates *chan game.Phase, playerUpdates *chan game.Player, lobbyUpdates *chan LobbyStatus, globalUpdates *chan BroadcastMessage) {
	return func(dg *discordgo.Session, guildID string, socketUpdates *chan SocketStatus, phaseUpdates *chan game.Phase, playerUpdates *chan game.Player, lobbyUpdates *chan LobbyStatus, globalUpdates *chan BroadcastMessage) {
		for {
			select {

			case phase := <-*phaseUpdates:

				log.Printf("PhaseUpdate-Nachricht für die Gilde erhalten %s\n", guildID)
				if guild, ok := bot.AllGuilds[guildID]; ok {
					if !guild.GameRunning {
						//completely ignore events if the game is ended/paused
						break
					}
					switch phase {
					case game.MENU:
						if guild.AmongUsData.GetPhase() == game.MENU {
							break
						}
						log.Println("Übergang zum Menü erkannt")
						guild.AmongUsData.SetRoomRegion("Unprovided", "Unprovided")
						guild.AmongUsData.SetPhase(phase)
						guild.GameStateMsg.Edit(dg, gameStateResponse(guild))
						guild.GameStateMsg.RemoveAllReactions(dg)
						break
					case game.LOBBY:
						if guild.AmongUsData.GetPhase() == game.LOBBY {
							break
						}
						log.Println("Übergang zur Lobby festgestellt")

						delay := guild.PersistentGuildData.Delays.GetDelay(guild.AmongUsData.GetPhase(), game.LOBBY)

						guild.AmongUsData.SetAllAlive()
						guild.AmongUsData.SetPhase(phase)

						//going back to the lobby, we have no preference on who gets applied first
						guild.handleTrackedMembers(&bot.SessionManager, delay, NoPriority)

						guild.GameStateMsg.Edit(dg, gameStateResponse(guild))

						guild.GameStateMsg.AddAllReactions(dg, guild.StatusEmojis[true])
						break
					case game.TASKS:
						if guild.AmongUsData.GetPhase() == game.TASKS {
							break
						}
						log.Println("Übergang zu Aufgaben erkannt")
						oldPhase := guild.AmongUsData.GetPhase()
						delay := guild.PersistentGuildData.Delays.GetDelay(oldPhase, game.TASKS)
						//when going from discussion to tasks, we should mute alive players FIRST
						priority := AlivePriority

						if oldPhase == game.LOBBY {
							//when we go from lobby to tasks, mark all users as alive to be sure
							guild.AmongUsData.SetAllAlive()
							priority = NoPriority
						}

						guild.AmongUsData.SetPhase(phase)

						guild.handleTrackedMembers(&bot.SessionManager, delay, priority)

						guild.GameStateMsg.Edit(dg, gameStateResponse(guild))
						break
					case game.DISCUSS:
						if guild.AmongUsData.GetPhase() == game.DISCUSS {
							break
						}
						log.Println("Übergang zur Diskussion festgestellt")

						delay := guild.PersistentGuildData.Delays.GetDelay(guild.AmongUsData.GetPhase(), game.DISCUSS)

						guild.AmongUsData.SetPhase(phase)

						guild.handleTrackedMembers(&bot.SessionManager, delay, DeadPriority)

						guild.GameStateMsg.Edit(dg, gameStateResponse(guild))
						break
					default:
						log.Printf("Unentdeckter neuer Zustand: %d\n", phase)
					}
				}

			case player := <-*playerUpdates:
				log.Printf("PlayerUpdate-Nachricht für Gilde erhalten %s\n", guildID)
				if guild, ok := bot.AllGuilds[guildID]; ok {
					if !guild.GameRunning {
						break
					}

					//	this updates the copies in memory
					//	(player's associations to amongus data are just pointers to these structs)
					if player.Name != "" {
						if player.Action == game.EXILED {
							log.Println("Erkanntes Spieler-EXILE-Ereignis, als tot markiert")
							player.IsDead = true
						}
						if player.IsDead == true && guild.AmongUsData.GetPhase() == game.LOBBY {
							log.Println("Ich habe ein totes Ereignis erhalten, aber wir sind in der Lobby, also ignoriere ich es")
							player.IsDead = false
						}

						if player.Disconnected || player.Action == game.LEFT {
							log.Println("Ich habe entdeckt, dass " + player.Name + " die Verbindung getrennt hat oder verlassen hat! " +
								"Ich entferne die verknüpften Spieldaten. Sie müssen neu verknüpfen")

							guild.UserData.ClearPlayerDataByPlayerName(player.Name)
							guild.AmongUsData.ClearPlayerData(player.Name)
							guild.GameStateMsg.Edit(dg, gameStateResponse(guild))
						} else {
							updated, isAliveUpdated := guild.AmongUsData.ApplyPlayerUpdate(player)

							if player.Action == game.JOINED {
								log.Println("Es wurde festgestellt, dass ein Spieler beigetreten ist und die Benutzerdatenzuordnungen aktualisiert wurden")
								data := guild.AmongUsData.GetByName(player.Name)
								if data == nil {
									log.Println("Keine Spielerdaten gefunden für " + player.Name)
								}

								guild.UserData.UpdatePlayerMappingByName(player.Name, data)
							}

							if updated {
								data := guild.AmongUsData.GetByName(player.Name)
								paired := guild.UserData.AttemptPairingByMatchingNames(player.Name, data)
								if paired {
									log.Println("Erfolgreich verknüpfter Discord-Benutzer mit übereinstimmenden Namen mit dem Spieler verbunden!")
								}

								//log.Println("Player update received caused an update in cached state")
								if isAliveUpdated && guild.AmongUsData.GetPhase() == game.TASKS {
									if guild.PersistentGuildData.UnmuteDeadDuringTasks {
										// unmute players even if in tasks because UnmuteDeadDuringTasks is true
										guild.handleTrackedMembers(&bot.SessionManager, 0, NoPriority)
										guild.GameStateMsg.Edit(dg, gameStateResponse(guild))
									} else {
										log.Println("NICHT die Discord-Statusmeldung aktualisieren; würde Infos leaken")
									}
								} else {
									guild.GameStateMsg.Edit(dg, gameStateResponse(guild))
								}
							} else {
								//log.Println("Player update received did not cause an update in cached state")
							}
						}

					}
				}
				break
			case socketUpdate := <-*socketUpdates:
				if guild, ok := bot.AllGuilds[socketUpdate.GuildID]; ok {
					//this automatically updates the game state message on connect or disconnect
					guild.GameStateMsg.Edit(dg, gameStateResponse(guild))
				}
				break

			case worldUpdate := <-*globalUpdates:
				if guild, ok := bot.AllGuilds[guildID]; ok {
					if worldUpdate.Type == GRACEFUL_SHUTDOWN {
						log.Printf("Es wurde eine ordnungsgemäße Meldung zum Herunterfahren empfangen, in %d Sekunden wird heruntergefahren", worldUpdate.Data)

						go bot.gracefulShutdownWorker(dg, guild, worldUpdate.Data)
					}
				}

			case lobbyUpdate := <-*lobbyUpdates:
				if guild, ok := bot.AllGuilds[lobbyUpdate.GuildID]; ok {
					guild.Linked = true
					guild.AmongUsData.SetRoomRegion(lobbyUpdate.Lobby.LobbyCode, lobbyUpdate.Lobby.Region.ToString()) // Set new room code
					guild.GameStateMsg.Edit(dg, gameStateResponse(guild))                                             // Update game state message
				}
			}
		}
	}
}

func (bot *Bot) gracefulShutdownWorker(s *discordgo.Session, guild *GuildState, seconds int) {
	if guild.GameStateMsg.message != nil {
		sendMessage(s, guild.GameStateMsg.message.ChannelID, fmt.Sprintf("**Ich muss offline gehen, um ein Upgrade durchzuführen! Euer Spiel/eure Lobby wird in %d Sekunden beendet!**", seconds))
	}

	time.Sleep(time.Duration(seconds) * time.Second)

	bot.handleGameEndMessage(guild, s)
}

// Gets called whenever a voice state change occurs
func (bot *Bot) voiceStateChange() func(s *discordgo.Session, m *discordgo.VoiceStateUpdate) {
	return func(s *discordgo.Session, m *discordgo.VoiceStateUpdate) {
		for id, socketGuild := range bot.AllGuilds {
			if id == m.GuildID {
				socketGuild.voiceStateChange(s, m)
				break
			}
		}
	}
}

// This function will be called (due to AddHandler above) every time a new
// message is created on any channel that the authenticated bot has access to.
func (bot *Bot) messageCreate() func(s *discordgo.Session, m *discordgo.MessageCreate) {
	return func(s *discordgo.Session, m *discordgo.MessageCreate) {
		for id, socketGuild := range bot.AllGuilds {
			if id == m.GuildID {
				bot.handleMessageCreate(socketGuild, s, m)
				break
			}
		}
	}
}

//this function is called whenever a reaction is created in a guild
func (bot *Bot) reactionCreate() func(s *discordgo.Session, m *discordgo.MessageReactionAdd) {
	return func(s *discordgo.Session, m *discordgo.MessageReactionAdd) {
		for id, socketGuild := range bot.AllGuilds {
			if id == m.GuildID {
				bot.handleReactionGameStartAdd(socketGuild, s, m)
				break
			}
		}
	}
}

func (bot *Bot) newGuild(emojiGuildID string) func(s *discordgo.Session, m *discordgo.GuildCreate) {
	return func(s *discordgo.Session, m *discordgo.GuildCreate) {

		var pgd *PersistentGuildData = nil

		data, err := bot.StorageInterface.GetGuildData(m.Guild.ID)
		if err != nil {
			log.Printf("Gilden-Daten für %s konnten nicht aus storageDriver geladen werden. Verwenden Sie stattdessen die Standardkonfiguration\n", m.Guild.ID)
			log.Printf("Genauer Fehler: %s", err)
		} else {
			tempPgd, err := FromData(data)
			if err != nil {
				log.Printf("Gilden-Daten für %s konnten nicht gemarshallt werden. Verwende stattdessen die Standardkonfiguration\n", m.Guild.ID)
			} else {
				log.Printf("Konfiguration von storagedriver für erfolgreich geladen für %s\n", m.Guild.ID)
				pgd = tempPgd
			}
		}
		if pgd == nil {
			pgd = PGDDefault(m.Guild.ID)
			data, err := pgd.ToData()
			if err != nil {
				log.Printf("Fehler beim Marshalling von %s PGD zur Zuordnung(!): %s\n", m.Guild.ID, err)
			} else {
				err := bot.StorageInterface.WriteGuildData(m.Guild.ID, data)
				if err != nil {
					log.Printf("Fehler beim Schreiben von %s PGD in die Speicherschnittstelle: %s\n", m.Guild.ID, err)
				} else {
					log.Printf("%s PGD wurde erfolgreich in die Speicherschnittstelle geschrieben!", m.Guild.ID)
				}
			}
		}

		log.Printf("Zur neuen Gilde hinzugefügt, ID %s, Name %s", m.Guild.ID, m.Guild.Name)
		bot.AllGuilds[m.ID] = &GuildState{
			PersistentGuildData: pgd,

			Linked: false,

			UserData:     MakeUserDataSet(),
			Tracking:     MakeTracking(),
			GameStateMsg: MakeGameStateMessage(),

			StatusEmojis:  emptyStatusEmojis(),
			SpecialEmojis: map[string]Emoji{},

			GameRunning: false,

			AmongUsData: game.NewAmongUsData(),
		}

		if emojiGuildID == "" {
			log.Println("[Dies ist kein Fehler] Für Emojis wurde keine explizite Gilden-ID bereitgestellt. mit dem aktuellen Gildenstandard")
			emojiGuildID = m.Guild.ID
		}
		allEmojis, err := s.GuildEmojis(emojiGuildID)
		if err != nil {
			log.Println(err)
		} else {
			bot.AllGuilds[m.Guild.ID].addAllMissingEmojis(s, m.Guild.ID, true, allEmojis)

			bot.AllGuilds[m.Guild.ID].addAllMissingEmojis(s, m.Guild.ID, false, allEmojis)

			bot.AllGuilds[m.Guild.ID].addSpecialEmojis(s, m.Guild.ID, allEmojis)
		}

		socketUpdates := make(chan SocketStatus)
		playerUpdates := make(chan game.Player)
		phaseUpdates := make(chan game.Phase)
		lobbyUpdates := make(chan LobbyStatus)
		globalUpdates := make(chan BroadcastMessage)

		bot.ChannelsMapLock.Lock()
		bot.SocketUpdateChannels[m.Guild.ID] = &socketUpdates
		bot.PlayerUpdateChannels[m.Guild.ID] = &playerUpdates
		bot.GamePhaseUpdateChannels[m.Guild.ID] = &phaseUpdates
		bot.LobbyUpdateChannels[m.Guild.ID] = &lobbyUpdates
		bot.GlobalBroadcastChannels[m.Guild.ID] = &globalUpdates
		bot.ChannelsMapLock.Unlock()

		go bot.updatesListener()(s, m.Guild.ID, &socketUpdates, &phaseUpdates, &playerUpdates, &lobbyUpdates, &globalUpdates)

	}
}

func newAltGuild(s *discordgo.Session, m *discordgo.GuildCreate) {
	//TODO ensure that the 2nd bot is also present in the same guilds as the original bot (to ensure it can also issue requests)
}

func (bot *Bot) handleMessageCreate(guild *GuildState, s *discordgo.Session, m *discordgo.MessageCreate) {
	// Ignore all messages created by the bot itself
	if m.Author.ID == s.State.User.ID {
		return
	}

	g, err := s.State.Guild(guild.PersistentGuildData.GuildID)
	if err != nil {
		log.Println(err)
		return
	}

	contents := m.Content

	if strings.HasPrefix(contents, guild.PersistentGuildData.CommandPrefix) {
		//either BOTH the admin/roles are empty, or the user fulfills EITHER perm "bucket"
		perms := len(guild.PersistentGuildData.AdminUserIDs) == 0 && len(guild.PersistentGuildData.PermissionedRoleIDs) == 0
		if !perms {
			perms = guild.HasAdminPermissions(m.Author.ID) || guild.HasRolePermissions(s, m.Author.ID)
		}
		if !perms && g.OwnerID != m.Author.ID {
			s.ChannelMessageSend(m.ChannelID, "Der Benutzer verfügt nicht über die erforderlichen Berechtigungen, um diesen Befehl auszuführen!")
		} else {
			oldLen := len(contents)
			contents = strings.Replace(contents, guild.PersistentGuildData.CommandPrefix+" ", "", 1)
			if len(contents) == oldLen { //didn't have a space
				contents = strings.Replace(contents, guild.PersistentGuildData.CommandPrefix, "", 1)
			}

			if len(contents) == 0 {
				if len(guild.PersistentGuildData.CommandPrefix) <= 1 {
					// prevent bot from spamming help message whenever the single character
					// prefix is sent by mistake
					return
				} else {
					s.ChannelMessageSend(m.ChannelID, helpResponse(Version, guild.PersistentGuildData.CommandPrefix))
				}
			} else {
				args := strings.Split(contents, " ")

				for i, v := range args {
					args[i] = strings.ToLower(v)
				}
				bot.HandleCommand(guild, s, g, bot.StorageInterface, m, args)
			}

		}
		//Just deletes messages starting with .au

		if guild.GameStateMsg.SameChannel(m.ChannelID) {
			deleteMessage(s, m.ChannelID, m.Message.ID)
		}
	}

}
